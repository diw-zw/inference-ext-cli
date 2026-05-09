# RBG LLM Model Commands

## Overview

The `kubectl rbg llm model` command group manages LLM model assets in configured storage. It supports pulling models from sources (HuggingFace, ModelScope) and listing downloaded models.

## Prerequisites

1. **Install kubectl-rbg plugin** (refer to [kubectl-rbg](kubectl-rbg.md))

2. **Configure storage and source**

   ```bash
   # Interactive wizard
   kubectl rbg llm config init

   # Or configure manually
   kubectl rbg llm config add-storage my-pvc --type pvc --config pvcName=model-pvc
   kubectl rbg llm config add-source huggingface --type huggingface --config token=hf_xxx
   ```

## Usage

### Pull a Model

```bash
# Pull a model with default settings
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B

# Pull a specific revision
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B --revision v1.0

# Pull using a specific source and storage
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B --source huggingface --storage model-pvc

# Pull without waiting for completion
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B --wait=false
```

By default, the command waits for the pull job to complete and streams logs. Use `--wait=false` to submit the job and return immediately.

### List Downloaded Models

```bash
# List models in the default storage
kubectl rbg llm model list

# List models in a specific storage
kubectl rbg llm model list --storage my-pvc
```

## Command Flags

### pull `MODEL_ID`

| Flag | Default | Description |
|------|---------|-------------|
| `--revision` | `main` | Model revision to download |
| `--source` | `""` | Source to use (overrides default) |
| `--storage` | `""` | Storage to use (overrides default) |
| `--wait` | `true` | Wait for the pull job to complete and stream logs |

### list

| Flag | Default | Description |
|------|---------|-------------|
| `--storage` | `""` | Storage to use (overrides default) |

## Example

```bash
# 1. Configure storage and source
kubectl rbg llm config init

# 2. Pull models
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B

# 3. List downloaded models
kubectl rbg llm model list

> MODEL ID                                 REVISION        DOWNLOADED AT
> ------------------------------------------------------------------------------------------
> google/gemma-4-26B-A4B                   main            2026-04-15T14:57:53Z
> Qwen/Qwen3.5-0.8B                        main            2026-04-10T09:34:58Z
> Qwen/Qwen3.6-35B-A3B                     main            2026-04-21T11:50:53Z

# 4. Deploy a pulled model as an inference service
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B
```
