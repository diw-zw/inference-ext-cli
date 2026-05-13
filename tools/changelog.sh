#!/usr/bin/env bash
#
# Generate changelog entries for the current version.
#
# Usage:
#   ./tools/changelog.sh [--repo <owner/repo>]
#
# Options:
#   --repo    Override the GitHub repository to query PRs from.
#             Useful when working from a fork. e.g., --repo rolebasedgroup/inference-ext-cli
#
# This script reads the version from the VERSION file, collects merged PRs
# since the last git tag via GitHub API, and prepends a new section to CHANGELOG.md.
#
# Output format:
#   ## [v0.8.0](repo_url/tree/v0.8.0) (2026-05-11)
#
#   - feat: add new feature ([#42](repo_url/pull/42) by [@author](https://github.com/author))
#
#   [Full Changelog](repo_url/compare/v0.7.0...v0.8.0)
#
# Requirements:
#   - gh CLI (GitHub CLI) with authentication
#
# After running, review and edit CHANGELOG.md before committing.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

VERSION_FILE="${ROOT_DIR}/VERSION"
CHANGELOG_FILE="${ROOT_DIR}/CHANGELOG.md"

# Parse arguments
OVERRIDE_REPO=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --repo)
            OVERRIDE_REPO="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Usage: $0 [--repo <owner/repo>]" >&2
            exit 1
            ;;
    esac
done

if [ ! -f "${VERSION_FILE}" ]; then
    echo "Error: VERSION file not found at ${VERSION_FILE}" >&2
    exit 1
fi

# Check gh CLI
if ! command -v gh &>/dev/null; then
    echo "Error: gh CLI is required. Install from https://cli.github.com/" >&2
    exit 1
fi

if ! gh auth status &>/dev/null 2>&1; then
    echo "Error: gh CLI is not authenticated. Run 'gh auth login' first." >&2
    exit 1
fi

VERSION="v$(tr -d '[:space:]' < "${VERSION_FILE}")"
DATE=$(date +%Y-%m-%d)

# Determine repository URL and slug
get_repo_info() {
    local remote_url
    remote_url=$(git -C "${ROOT_DIR}" remote get-url origin 2>/dev/null || echo "")
    # Convert SSH URL to HTTPS
    if [[ "${remote_url}" == git@github.com:* ]]; then
        remote_url="https://github.com/${remote_url#git@github.com:}"
    fi
    # Remove .git suffix
    remote_url="${remote_url%.git}"
    echo "${remote_url}"
}

if [ -n "${OVERRIDE_REPO}" ]; then
    REPO_SLUG="${OVERRIDE_REPO}"
    REPO_URL="https://github.com/${REPO_SLUG}"
else
    REPO_URL=$(get_repo_info)
    REPO_SLUG="${REPO_URL#https://github.com/}"
fi

# Find previous tag
PREV_TAG=$(git -C "${ROOT_DIR}" describe --tags --abbrev=0 2>/dev/null || echo "")

echo "Generating changelog for ${VERSION}..."
echo "  Repository: ${REPO_SLUG}"
echo "  Previous tag: ${PREV_TAG:-<none>}"

# Get merged PRs since previous tag using gh API
get_merged_prs() {
    local since_date=""
    if [ -n "${PREV_TAG}" ]; then
        # Use commit date in YYYY-MM-DD form — GitHub search accepts that format
        # cleanly and avoids timezone-suffix edge cases.
        since_date=$(git -C "${ROOT_DIR}" log -1 --format=%cs "${PREV_TAG}" 2>/dev/null || echo "")
    fi

    # gh pr list with --search overrides the --state filter, so build a single
    # search query that covers both is:merged and merged:>... when applicable.
    local args=(
        --repo "${REPO_SLUG}"
        --limit 200
        --json number,title,author,url,mergedAt
        --jq 'sort_by(.mergedAt) | reverse | .[] | "- \(.title) ([#\(.number)](\(.url)) by [@\(.author.login)](https://github.com/\(.author.login)))"'
    )
    if [ -n "${since_date}" ]; then
        args+=(--search "is:pr is:merged merged:>${since_date}")
    else
        args+=(--state merged)
    fi
    gh pr list "${args[@]}"
}

# Generate PR list. Capture stderr so a real failure (auth, rate limit, network)
# is surfaced instead of silently falling through to git log.
GH_ERR=$(mktemp)
trap 'rm -f "${GH_ERR}" "${CHANGELOG_FILE}.tmp"' EXIT
if PR_LIST=$(get_merged_prs 2>"${GH_ERR}"); then
    :
else
    echo "Warning: gh pr list failed:" >&2
    cat "${GH_ERR}" >&2
    PR_LIST=""
fi

if [ -z "${PR_LIST}" ]; then
    echo ""
    echo "No merged PRs found. Falling back to git log..."

    # Fallback: use git log with commit-based entries
    log_range=""
    if [ -n "${PREV_TAG}" ]; then
        log_range="${PREV_TAG}..HEAD"
    fi

    PR_LIST=$(git -C "${ROOT_DIR}" log ${log_range} --no-merges \
        --pretty=format:"- %s ([%h](${REPO_URL}/commit/%H) by @%an)" 2>/dev/null || echo "")

    if [ -z "${PR_LIST}" ]; then
        echo "No new changes found. Nothing to add."
        exit 0
    fi
fi

# Build release URL and comparison URL
RELEASE_URL="${REPO_URL}/tree/${VERSION}"
if [ -n "${PREV_TAG}" ]; then
    COMPARE_URL="${REPO_URL}/compare/${PREV_TAG}...${VERSION}"
else
    COMPARE_URL=""
fi

# Build the new section
NEW_SECTION="## [${VERSION}](${RELEASE_URL}) (${DATE})

${PR_LIST}"

if [ -n "${COMPARE_URL}" ]; then
    NEW_SECTION="${NEW_SECTION}

[Full Changelog](${COMPARE_URL})"
fi

# Insert the new section into CHANGELOG.md
if [ -f "${CHANGELOG_FILE}" ]; then
    # Check if this version already exists. -F (fixed-string) avoids the dots in
    # vX.Y.Z being interpreted as regex metacharacters.
    EXISTING=$(grep -nF "## [${VERSION}]" "${CHANGELOG_FILE}" || true)
    if [ -n "${EXISTING}" ]; then
        echo ""
        echo "Warning: ${VERSION} already exists in CHANGELOG.md. Skipping."
        echo "Remove the existing section first if you want to regenerate."
        exit 1
    fi

    # Find the line number of the first "## " heading (version section)
    FIRST_SECTION_LINE=$(grep -n "^## " "${CHANGELOG_FILE}" | head -1 | cut -d: -f1 || true)

    if [ -n "${FIRST_SECTION_LINE}" ]; then
        # Insert new section before the first existing version section
        {
            head -n $((FIRST_SECTION_LINE - 1)) "${CHANGELOG_FILE}"
            echo "${NEW_SECTION}"
            echo ""
            tail -n +"${FIRST_SECTION_LINE}" "${CHANGELOG_FILE}"
        } > "${CHANGELOG_FILE}.tmp"
        mv "${CHANGELOG_FILE}.tmp" "${CHANGELOG_FILE}"
    else
        # No existing version sections, append after the header
        {
            cat "${CHANGELOG_FILE}"
            echo ""
            echo "${NEW_SECTION}"
        } > "${CHANGELOG_FILE}.tmp"
        mv "${CHANGELOG_FILE}.tmp" "${CHANGELOG_FILE}"
    fi
else
    cat > "${CHANGELOG_FILE}" <<EOF
# Changelog

${NEW_SECTION}
EOF
fi

echo ""
echo "Changelog updated for ${VERSION} (${DATE})"
echo ""
echo "Next steps:"
echo "  1. Review and edit ${CHANGELOG_FILE}"
echo "  2. Group PRs into: Features, Bug fixes, Misc, etc."
echo "  3. Commit the updated Changelog"
