# Image URL to use all building/pushing image targets
IMG_REPO ?= rolebasedgroup
AUTOBENCHMARK_IMG  ?= ${IMG_REPO}/rbgs-autobenchmark
DASHBOARD_IMG      ?= ${IMG_REPO}/rbgs-benchmark-dashboard
AUTOBENCHMARK_UI_IMG   ?= ${IMG_REPO}/rbgs-autobenchmark-dashboard

AUTOBENCHMARK_DOCKERFILE  ?= docker/autobenchmark-ctl.Dockerfile
DASHBOARD_DOCKERFILE      ?= docker/benchmark-dashboard.Dockerfile 
BENCHMARK_UI_DOCKERFILE   ?= docker/autobenchmark-dashboard.Dockerfile

VERSION ?= v0.7.0
GIT_SHA ?= $(shell git rev-parse --short HEAD || echo "HEAD")
TAG ?= ${VERSION}-${GIT_SHA}

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GO_CMD ?= go
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

# CONTAINER_TOOL defines the container tool to be used for building images.
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

export GO111MODULE=on

GOPROXY   ?=
GOPRIVATE ?=
GOSUMDB   ?=

# ldflags
VERSION_PKG=sigs.k8s.io/rbgs/cli/version
GIT_COMMIT=$(shell git rev-parse HEAD)
BUILD_DATE=$(shell date +%Y-%m-%dT%H:%M:%S%z)
ldflags="-s -w -X $(VERSION_PKG).Version=$(TAG) -X $(VERSION_PKG).GitCommit=${GIT_COMMIT} -X ${VERSION_PKG}.BuildDate=${BUILD_DATE}"

DOCKER_BUILD_ARGS := \
	--build-arg GOPROXY=$(GOPROXY) \
	--build-arg GOPRIVATE=$(GOPRIVATE) \
	--build-arg GOSUMDB=$(GOSUMDB) \
	$(if $(TARGETARCH),--build-arg TARGETARCH=$(TARGETARCH)) \
	$(if $(TARGETOS),--build-arg TARGETOS=$(TARGETOS))

.PHONY: all
all: build-cli

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

.PHONY: build-cli
build-cli: ## Build CLI binary.
	GOARCH=${TARGETARCH} \
	GOOS=${TARGETOS} \
	CGO_ENABLED=0 \
	GO111MODULE=on \
	GOPROXY=${GOPROXY} \
	$(GO_CMD) build -v -o bin/llmctl -ldflags $(ldflags) ./cmd/llmctl/

CLI_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build-cli-all
build-cli-all: ## Build CLI binaries for all platforms.
	@for platform in $(CLI_PLATFORMS); do \
		GOOS=$${platform%%/*} GOARCH=$${platform##*/} \
		CGO_ENABLED=0 GO111MODULE=on GOPROXY=${GOPROXY} \
		$(GO_CMD) build -v -o bin/llmctl-$${platform%%/*}-$${platform##*/} -ldflags $(ldflags) ./cmd/llmctl/; \
		echo "Built bin/llmctl-$${platform%%/*}-$${platform##*/}"; \
	done

.PHONY: build-autobenchmark-ctl
build-autobenchmark-ctl: ## Build autobenchmark controller binary.
	GOARCH=${TARGETARCH} \
	GOOS=${TARGETOS} \
	CGO_ENABLED=0 \
	GO111MODULE=on \
	GOPROXY=${GOPROXY} \
	$(GO_CMD) build -v -o bin/autobenchmark -ldflags $(ldflags) ./cmd/autobenchmark/

.PHONY: build-benchmark-dashboard
build-benchmark-dashboard: ## Build benchmark dashboard binary.
	GOARCH=${TARGETARCH} \
	GOOS=${TARGETOS} \
	CGO_ENABLED=0 \
	GO111MODULE=on \
	GOPROXY=${GOPROXY} \
	$(GO_CMD) build -v -o bin/dashboard -ldflags $(ldflags) ./ui/benchmark/

.PHONY: build-all
build-all: build-cli build-autobenchmark-ctl build-benchmark-dashboard ## Build all binaries.

.PHONY: install
install: build-cli ## Install llmctl to GOPATH/bin.
	cp bin/llmctl $(GOBIN)/llmctl

##@ Test

.PHONY: test
test: ## Run unit tests.
	$(GO_CMD) test ./... -count=1

.PHONY: test-verbose
test-verbose: ## Run unit tests with verbose output.
	$(GO_CMD) test -v ./... -count=1

.PHONY: cover
cover: ## Run tests with coverage report.
	$(GO_CMD) test ./... -count=1 -coverprofile bin/coverage.out
	$(GO_CMD) tool cover -html=bin/coverage.out -o bin/coverage.html
	@echo "Coverage report: bin/coverage.html"

##@ Code Quality

.PHONY: fmt
fmt: ## Run go fmt against code.
	$(GO_CMD) fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	$(GO_CMD) vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: tidy
tidy: ## Tidy and verify go modules.
	$(GO_CMD) mod tidy
	$(GO_CMD) mod verify

##@ Docker

DOCKER_BUILD_ARGS := \
	--build-arg GOPROXY=$(GOPROXY) \
	--build-arg GOPRIVATE=$(GOPRIVATE) \
	--build-arg GOSUMDB=$(GOSUMDB) \
	$(if $(TARGETARCH),--build-arg TARGETARCH=$(TARGETARCH)) \
	$(if $(TARGETOS),--build-arg TARGETOS=$(TARGETOS))

.PHONY: docker-build-autobenchmark-ctl
docker-build-autobenchmark-ctl: ## Build autobenchmark Docker image.
	$(CONTAINER_TOOL) build -f ${AUTOBENCHMARK_DOCKERFILE} --platform linux/amd64 -t ${AUTOBENCHMARK_IMG}:${TAG} $(DOCKER_BUILD_ARGS) .

.PHONY: docker-build-benchmark-dashboard
docker-build-benchmark-dashboard: ## Build dashboard Docker image.
	$(CONTAINER_TOOL) build -f ${DASHBOARD_DOCKERFILE} --platform linux/amd64 -t ${DASHBOARD_IMG}:${TAG} $(DOCKER_BUILD_ARGS) .

.PHONY: docker-build-autobenchmark-dashboard
docker-build-autobenchmark-dashboard: ## Build benchmark-viewer UI Docker image.
	$(CONTAINER_TOOL) build -f ${BENCHMARK_UI_DOCKERFILE} --platform linux/amd64 -t ${AUTOBENCHMARK_UI_IMG}:${TAG} .

.PHONY: docker-build
docker-build: docker-build-autobenchmark-ctl docker-build-benchmark-dashboard docker-build-autobenchmark-dashboard ## Build all Docker images.

.PHONY: docker-push-autobenchmark
docker-push-autobenchmark: ## Push autobenchmark Docker image.
	$(CONTAINER_TOOL) push ${AUTOBENCHMARK_IMG}:${TAG}

.PHONY: docker-push-benchmark-dashboard
docker-push-benchmark-dashboard: ## Push dashboard Docker image.
	$(CONTAINER_TOOL) push ${DASHBOARD_IMG}:${TAG}

.PHONY: docker-push-autobenchmark-dashboard
docker-push-autobenchmark-dashboard: ## Push benchmark-viewer UI Docker image.
	$(CONTAINER_TOOL) push ${AUTOBENCHMARK_UI_IMG}:${TAG}

.PHONY: docker-push
docker-push: docker-push-autobenchmark docker-push-benchmark-dashboard docker-push-autobenchmark-dashboard ## Push all Docker images.

# Multi-platform Docker builds using buildx
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: docker-buildx-autobenchmark
docker-buildx-autobenchmark: ## Build and push multi-arch autobenchmark image.
	- $(CONTAINER_TOOL) buildx create --name rbgs-cli-builder
	$(CONTAINER_TOOL) buildx use rbgs-cli-builder
	$(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${AUTOBENCHMARK_IMG}:${TAG} $(DOCKER_BUILD_ARGS) -f ${AUTOBENCHMARK_DOCKERFILE} .
	- $(CONTAINER_TOOL) buildx rm rbgs-cli-builder

.PHONY: docker-buildx-benchmark-dashboard
docker-buildx-benchmark-dashboard: ## Build and push multi-arch benchmark-dashboard image.
	- $(CONTAINER_TOOL) buildx create --name rbgs-cli-builder
	$(CONTAINER_TOOL) buildx use rbgs-cli-builder
	$(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${DASHBOARD_IMG}:${TAG} $(DOCKER_BUILD_ARGS) -f ${DASHBOARD_DOCKERFILE} .
	- $(CONTAINER_TOOL) buildx rm rbgs-cli-builder

.PHONY: docker-buildx-autobenchmark-dashboard
docker-buildx-autobenchmark-dashboard: ## Build and push multi-arch benchmark-viewer UI image.
	- $(CONTAINER_TOOL) buildx create --name rbgs-cli-builder
	$(CONTAINER_TOOL) buildx use rbgs-cli-builder
	$(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${AUTOBENCHMARK_UI_IMG}:${TAG} -f ${BENCHMARK_UI_DOCKERFILE} .
	- $(CONTAINER_TOOL) buildx rm rbgs-cli-builder

.PHONY: docker-buildx
docker-buildx: docker-buildx-autobenchmark docker-buildx-benchmark-dashboard docker-buildx-autobenchmark-dashboard ## Build and push all multi-arch Docker images.

##@ Cleanup

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf bin/

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(PROJECT_DIR)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
GOLANGCI_LINT_VERSION ?= v1.63.4

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) $(GO_CMD) install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
