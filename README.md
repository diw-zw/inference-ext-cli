# Inference extension cli

RBG (RoleBasedGroup) CLI extension for LLM inference workload management on Kubernetes.

## Features

- **Service Management** — Deploy, list, delete LLM inference services, interactive chat
- **Model Management** — List and pull models from various sources (HuggingFace, ModelScope, etc.)
- **Benchmark** — Run LLM inference benchmarks, view logs and results via dashboard
- **Auto-Benchmark** — Automated benchmark orchestration with SLA evaluation, parameter search (Optuna), convergence analysis
- **Configuration** — Manage engine, storage, and data source plugin configurations
- **Generate** — Generate RBG deployment manifests from templates
- **Visualization** — Web dashboards for experiment overview, parameter comparison, and convergence analysis

## Project Structure

```
cmd/
├── llmctl/             # CLI entry point (llmctl binary)
└── autobenchmark/      # Auto-benchmark controller binary
pkg/
├── autobenchmark/      # Auto-benchmark core logic (controller, config, search, lifecycle)
├── config/             # Shared configuration
├── plugin/             # Plugin system (engine, source, storage)
└── util/               # Utilities
ui/
├── auto-benchmark/     # Auto-benchmark dashboard (React)
└── benchmark/          # Benchmark viewer (React)
tools/
├── genai/              # genai-bench Docker image build
└── optuna/             # Optuna bridge for parameter search
doc/
└── usage/              # CLI usage documentation
```

## Prerequisites

- Go 1.26+
- Docker (for image builds)
- Access to a Kubernetes cluster with RBG installed

## Build

```bash
# Build CLI binary (current platform)
make build-cli

# Build for all platforms (linux/darwin, amd64/arm64)
make build-cli-all

# Build all binaries (CLI + autobenchmark controller + dashboard)
make build-all

# Install to GOPATH/bin
make install
```

## Docker Images

```bash
# Build all images
make docker-build

# Build individual images
make docker-build-autobenchmark-ctl
make docker-build-benchmark-dashboard
make docker-build-autobenchmark-dashboard

# Multi-arch build and push (linux/amd64 + linux/arm64)
make docker-buildx

# Push images
make docker-push
```

Override registry:

```bash
IMG_REPO=your-registry.com/namespace make docker-build
```

## Development

```bash
# Run tests
make test

# Format code
make fmt

# Lint
make lint

# Tidy modules
make tidy
```

## Usage

See [doc/usage/](doc/usage/) for detailed CLI documentation.

```bash
# Basic usage
llmctl --help

# Service management
llmctl svc run <name> <model-id> [--engine vllm]
llmctl svc list
llmctl svc delete <name>

# Benchmark
llmctl benchmark run <rbg-name> [--config <config.yaml>]
llmctl benchmark list <rbg-name>
llmctl benchmark dashboard

# Model operations
llmctl model list
llmctl model pull <model-id>

# Configuration
llmctl config view
llmctl config get-engines
llmctl config get-sources
llmctl config get-storages
```

## License

Apache License 2.0
