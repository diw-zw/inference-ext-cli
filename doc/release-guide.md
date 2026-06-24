# Release Guide

This document describes the complete release process for the inference-ext-cli project.

## Pre-Release Checklist

### Step 1: Update VERSION File

Edit the `VERSION` file in the repository root to the new version number (bare semver, no `v` prefix):

```bash
echo "<NEW_VERSION>" > VERSION
```

### Step 2: Generate Changelog

The changelog script uses GitHub CLI (`gh`) to collect merged PRs since the last tag and generates entries in the following format:

```markdown
## [v0.8.0](https://github.com/rolebasedgroup/inference-ext-cli/tree/v0.8.0) (2026-05-15)

- feat: add auto-benchmark SLA evaluation ([#42](https://github.com/rolebasedgroup/inference-ext-cli/pull/42) by [@author](https://github.com/author))
- fix: resolve dashboard loading issue ([#43](https://github.com/rolebasedgroup/inference-ext-cli/pull/43) by [@author](https://github.com/author))

[Full Changelog](https://github.com/rolebasedgroup/inference-ext-cli/compare/v0.7.0...v0.8.0)
```

Run the script:

```bash
./tools/changelog.sh
```

If working from a fork, use `--repo` to specify the upstream repository:

```bash
./tools/changelog.sh --repo rolebasedgroup/inference-ext-cli
```

Review and edit the generated content:

```bash
vim CHANGELOG.md
```

After generation, group PRs into categories (Features, Bug fixes, Misc, etc.) and remove irrelevant entries before committing.

### Step 3: Commit and Submit PR

```bash
VERSION=$(cat VERSION)
git checkout -b release/v${VERSION}
git add VERSION CHANGELOG.md cmd/ doc/ pkg/    # adjust based on actual changes
git commit -m "Prepare release v${VERSION}"
git push origin release/v${VERSION}
```

Open a PR to `main`. CI will run tests, lint, build check, copyright check, and version-format check.
Wait for all checks to pass, then merge.

## Release Process

### Step 4: Create and Push Tag

```bash
VERSION=$(cat VERSION)
git checkout main && git pull
git tag v${VERSION}
git push origin v${VERSION}
```

### Step 5: Wait for CI

The tag push triggers the `release.yml` workflow, which automatically:

1. **Validates** that the git tag matches the VERSION file
2. **Builds CLI binaries** for 4 platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
3. **Builds and pushes Docker images** (3 images, multi-arch amd64+arm64) to Docker Hub
4. **Creates a Draft Release** on GitHub with CLI binaries and release notes from CHANGELOG.md

### Step 6: Review and Publish Release

1. Go to the GitHub Releases page
2. Find the draft release created by CI
3. Review the release description (sourced from CHANGELOG.md)
4. Adjust the description if needed
5. Click **Publish release**

## Post-Release Verification

- Check the GitHub Release page: 4 platform binaries + checksums
- Download and verify CLI binary version (both forms work):
  ```bash
  ./llmctl-darwin-arm64 version
  ./llmctl-darwin-arm64 --version
  # Expected: RBG CLI Version: v<VERSION>, git commit: <SHA>, build date: <DATE>
  ```

## Hotfix Process

For urgent fixes after a release, follow the same Pre-Release Checklist + Release Process
steps, but branched from the prior release tag rather than `main`:

1. Branch from the release tag: `git checkout -b hotfix/v<NEW_VERSION> v<BASE_VERSION>`
2. Apply the fix.
3. Update `VERSION`: `echo "<NEW_VERSION>" > VERSION`.
4. Run **Step 2** (sed image-tag updates) so default CLI image references match the new version.
5. Run **Step 3** (`./tools/changelog.sh`) to regenerate the changelog entry.
6. Open a PR to `main` (Step 4) and wait for CI.
7. After merge, tag and push as in **Step 5** to trigger the release workflow.
