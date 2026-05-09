# RBG LLM Service Commands

## Overview

The `kubectl rbg llm svc` command group manages the full lifecycle of LLM inference services on Kubernetes using RoleBasedGroup. It supports deploying models with various inference engines (vLLM, SGLang), listing available model configurations, listing and deleting services, and interactively chatting with deployed models.

## Prerequisites

1. **Install kubectl-rbg plugin** (refer to [kubectl-rbg](kubectl-rbg.md))

2. **Configure storage and source**

   ```bash
   # Interactive wizard — sets up storage and source in one step
   kubectl rbg llm config init

   # Or configure manually
   kubectl rbg llm config add-storage my-pvc --type pvc --config pvcName=model-pvc
   kubectl rbg llm config add-source huggingface --type huggingface --config token=hf_xxx
   ```

3. **Pull a model to storage**

   ```bash
   kubectl rbg llm model pull Qwen/Qwen3.5-0.8B
   ```

## Usage

### Run an Inference Service

```bash
# Quick start with built-in model config
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B

# Specify a deployment mode
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --mode throughput

# Override inference engine
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --engine sglang

# Use a custom image (e.g., mirror registry)
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --image registry.cn-hangzhou.aliyuncs.com/my/vllm:latest

# Deploy a custom model without any pre-built model config
kubectl rbg llm svc run my-model org/new-model --engine vllm --resource nvidia.com/gpu=1

# Override resources on an existing model config
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --resource nvidia.com/gpu=2 --resource memory=16Gi

# Multi-node distributed deployment
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --distributed-size 4 --shm-size 16Gi

# Run with multiple replicas
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --replicas 3

# Pass additional engine arguments and environment variables
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B \
  --arg --max-model-len=4096 --arg --dtype=half \
  --env CUDA_VISIBLE_DEVICES=0,1

# Specify a custom model path (when models are placed manually in storage)
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --model-path /models/my-custom-model

# Dry run to preview generated configuration
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --dry-run

# Skip API readiness test
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --test-api=false
```

By default, the command waits for the service to be ready and tests the API endpoint. Use `--wait=false` to return immediately after creating the RoleBasedGroup.

### List Model Configurations

Before deploying a model with `kubectl rbg llm svc run`, you can list available model configurations to discover supported model IDs, run modes, inference engines, and resource presets. Model configs can be built-in or user-defined (placed in `~/.rbg/models/`).

```bash
# List all available model configurations
kubectl rbg llm svc model-configs

# Show full details including engine and source
kubectl rbg llm svc model-configs -o wide
```

### List Services

```bash
# List services in current namespace
kubectl rbg llm svc list

# List services across all namespaces
kubectl rbg llm svc list -A
```

### Delete Services

```bash
# Delete a single service
kubectl rbg llm svc delete my-qwen

# Delete multiple services
kubectl rbg llm svc delete my-qwen my-llama
```

### Chat with a Service

```bash
# Non-interactive: send a single prompt
kubectl rbg llm svc chat my-qwen --prompt "What is Kubernetes?"

# Interactive session
kubectl rbg llm svc chat my-qwen

# With a system prompt
kubectl rbg llm svc chat my-qwen --system "You are a helpful assistant."
```

## Command Flags

### run `<name>` `<model-id>`

#### Model & Engine

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `""` | Run mode (default: first mode in model config) |
| `--engine` | `""` | Inference engine: `vllm`, `sglang` (default: from mode config). Required when no model config exists for `<model-id>`. |
| `--image` | `""` | Container image override (default: from engine plugin) |
| `--revision` | `main` | Model revision |
| `--storage` | `""` | Storage to use (overrides default) |
| `--model-path` | `""` | Absolute model path inside the container. Default is `<storage-mount>/<model>/<revision>` |

#### Resource & Deployment

| Flag | Default | Description |
|------|---------|-------------|
| `--replicas` | `1` | Number of replicas |
| `--resource` | - | Resource requirements (`key=value`, e.g. `nvidia.com/gpu=1`), can be specified multiple times |
| `--distributed-size` | `0` | Multi-node deployment size (`<=1` means standalone) |
| `--shm-size` | `""` | Shared memory size (e.g. `8Gi`, `16Gi`) |

#### Engine Arguments

| Flag | Default | Description |
|------|---------|-------------|
| `--arg` | - | Additional arguments for the engine, can be specified multiple times |
| `--env` | - | Environment variables (`KEY=VALUE`), can be specified multiple times |

#### Execution Control

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run` | `false` | Print the generated template without creating the workload |
| `--wait` | `true` | Wait for the RoleBasedGroup to be ready |
| `--wait-timeout` | `20m` | Timeout for waiting |
| `--test-api` | `true` | Test the `/v1/chat/completions` endpoint after service is ready |
| `--test-api-timeout` | `5m` | Timeout for API testing |
| `--local-port` | `32432` | Local port for port-forward when testing API |

#### Flag-Only Deployment

When no built-in or user-defined model config is found for the given `<model-id>`, the command falls back to flag-only mode if `--engine` is specified. In this mode, all configuration comes from flags:

```bash
kubectl rbg llm svc run my-model org/new-model \
  --engine vllm \
  --image my-registry/vllm:custom \
  --resource nvidia.com/gpu=1 \
  --resource memory=16Gi \
  --arg "--max-model-len=4096" \
  --env "CUDA_VISIBLE_DEVICES=0"
```

When a model config exists, these flags act as overrides — `--resource` merges by key (flag wins on conflict), `--arg` and `--env` append to the config values.

### model-configs

| Flag | Default | Description |
|------|---------|-------------|
| `-o, --output` | `""` | Output format: `wide` for full details (includes engine, source) |

### list

| Flag | Default | Description |
|------|---------|-------------|
| `-A, --all-namespaces` | `false` | List services across all namespaces |

### delete `[name...]`

No additional flags. Accepts one or more service names as positional arguments.

### chat `<name>`

| Flag | Default | Description |
|------|---------|-------------|
| `-p, --prompt` | `""` | Single prompt (non-interactive mode) |
| `-i, --interactive` | `false` | Start an interactive chat session (REPL) |
| `--system` | `""` | System prompt prepended to every conversation |
| `--no-stream` | `false` | Disable streaming; wait for the full response |
| `--local-port` | `0` | Local port for the tunnel (default: random) |
| `--request-timeout` | `5m` | HTTP request timeout for model inference |
| `--port-forward-timeout` | `30s` | Timeout waiting for the port-forward tunnel |

## Example

```bash
# 1. Configure storage and source
kubectl rbg llm config init

# 2. Pull a model
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B

# 3. Deploy with default config
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B

# 4. Check service status
kubectl rbg llm svc list

> NAME              MODEL                  ENGINE   MODE       REVISION   REPLICAS   STATUS
> qwen3-6-35b-a3b   Qwen/Qwen3.6-35B-A3B   sglang   standard   main       1          Ready

# 5. Chat with the model
kubectl rbg llm svc chat my-qwen --prompt "解释 MoE 架构的优势"

> Connecting to pod qwen3-6-35b-a3b-inference-0...
> Connected.
> 
> Here's a thinking process:
> ...
> </think>

> Mixture of Experts（MoE，混合专家）是一种“稀疏激活”的大模型架构。其核心思想是：**为每个输入动态路由到少数几个“专家”网络进行处理，而非让全部参数参与计算**。这种设计在保持甚至提升模型能力的同时，显著优化了资源使用效率。以下是 MoE 架构的主要优势：
> ...
> MoE 架构通过**“稀疏激活 + 动态路由 + 隐性分工”**，重新定义了大模型规模与效率的平衡关系。它不是单纯“增加参数”，而是让参数更智能地被调用，是当前突破算力瓶颈、实现万亿参数实用化的核心架构之一。随着路由优化、通信压缩和硬件协同设计的进步，MoE 正从“研究方案”快速走向工业级标配。

# 6. Interactive chat session
kubectl rbg llm svc chat my-qwen -i

> Connecting to pod qwen3-6-35b-a3b-inference-0...
> Connected.
> 
> Interactive chat session started. Type '/exit' or press Ctrl+C to quit.
> ──────────────────────────────────────────────────
> You: hello, introduce yourself
> Assistant: Here's a thinking process:
> 
> 1.  **Analyze User Input:**
> ...
> 6.  **Final Output Generation:** (Matches the refined draft)✅
> </think>
> 
> Hello! I'm Qwen, a large language model developed by Alibaba Group's Tongyi Lab. I'm designed to be a clear, honest, and versatile AI assistant. I can help with tasks like answering questions, writing and debugging code, analyzing documents or images, brainstorming ideas, translating across 100+ languages, and breaking down complex topics into understandable steps. I'll always aim to be accurate, respectful, and tailored to what you need. What can I help you with today?
> You: /exit

# 7. Deploy a custom model with flag-only configuration
kubectl rbg llm svc run my-custom custom-org/custom-model \
  --engine vllm \
  --resource nvidia.com/gpu=2 \
  --resource memory=32Gi \
  --arg "--tensor-parallel-size=2"

# 8. Dry run to inspect generated configuration
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --dry-run

# 9. Clean up
kubectl rbg llm svc delete my-qwen my-custom
```
