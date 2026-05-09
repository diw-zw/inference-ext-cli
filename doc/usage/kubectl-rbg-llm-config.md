# RBG LLM Config Commands

## Overview

The `kubectl rbg llm config` command group manages the CLI configuration for LLM deployments, including storage backends, model download sources, and inference engine settings.

Configuration is stored locally at `~/.rbg/config`.

## Usage

### Initialize Configuration

The interactive wizard guides you through setting up storage and source in one step:

```bash
kubectl rbg llm config init
```

### View Configuration

```bash
kubectl rbg llm config view
```

Sensitive fields (e.g. API tokens, AccessKey secrets) are masked in the output.

---

## Storage Management

Storage defines where models are stored and accessed by inference engines.

### Supported Storage Types

#### PVC

| Config Key | Required | Description |
|------------|----------|-------------|
| `pvcName` | Yes | Name of the pre-existing PersistentVolumeClaim |

```bash
kubectl rbg llm config add-storage my-pvc --type pvc --config pvcName=model-pvc
```

#### OSS (Alibaba Cloud Object Storage Service)

| Config Key | Required | Description |
|------------|----------|-------------|
| `url` | Yes | OSS endpoint URL (e.g. `oss-cn-hangzhou.aliyuncs.com`) |
| `bucket` | Yes | OSS bucket name |
| `subpath` | No | Subpath within the bucket |
| `akId` | Yes | Alibaba Cloud AccessKey ID |
| `akSecret` | Yes | Alibaba Cloud AccessKey Secret |

```bash
kubectl rbg llm config add-storage my-oss --type oss \
  --config url=oss-cn-hangzhou.aliyuncs.com \
  --config bucket=my-bucket \
  --config akId=MY_ACCESS_KEY_ID \
  --config akSecret=MY_ACCESS_KEY_SECRET
```

The credentials are stored as a Kubernetes Secret; only the secret reference is saved in the config file.

### Storage Subcommands

```bash
# Add a storage configuration
kubectl rbg llm config add-storage NAME --type TYPE --config key=value

# Add interactively
kubectl rbg llm config add-storage NAME -i

# List all storage configurations
kubectl rbg llm config get-storages

# Show details of a specific storage
kubectl rbg llm config get-storages my-pvc

# Set the active storage
kubectl rbg llm config use-storage NAME

# Update a storage configuration
kubectl rbg llm config set-storage NAME --config key=value

# Delete a storage configuration
kubectl rbg llm config delete-storage NAME
```

> Note: Cannot delete the currently active storage. Switch to another storage first.

---

## Source Management

Sources define where models are downloaded from.

### Supported Source Types

#### HuggingFace

| Config Key | Required | Description |
|------------|----------|-------------|
| `token` | No | HuggingFace API token (required for private models) |
| `tokenSecret` | No | Kubernetes Secret name containing `HF_TOKEN` (takes precedence over `token`) |
| `mirror` | No | Mirror URL (e.g. `https://hf-mirror.com`) |

```bash
kubectl rbg llm config add-source hf --type huggingface --config token=hf_xxx
```

#### ModelScope

| Config Key | Required | Description |
|------------|----------|-------------|
| `token` | No | ModelScope API token (required for private models) |
| `tokenSecret` | No | Kubernetes Secret name containing `MODELSCOPE_TOKEN` (takes precedence over `token`) |

```bash
kubectl rbg llm config add-source ms --type modelscope --config token=xxx
```

### Source Subcommands

```bash
# Add a source configuration
kubectl rbg llm config add-source NAME --type TYPE --config key=value

# Add interactively
kubectl rbg llm config add-source NAME -i

# List all source configurations
kubectl rbg llm config get-sources

# Show details of a specific source
kubectl rbg llm config get-sources huggingface

# Set the active source
kubectl rbg llm config use-source NAME

# Update a source configuration
kubectl rbg llm config set-source NAME --config key=value

# Delete a source configuration
kubectl rbg llm config delete-source NAME
```

> Note: Cannot delete the currently active source. Switch to another source first.

---

## Engine Management

Engine configuration is optional — engines work with sensible defaults. Use these commands only when you need to customize engine-specific parameters.

### Supported Engines

#### vLLM

| Config Key | Default | Description |
|------------|---------|-------------|
| `image` | `vllm/vllm-openai:latest` | Container image |
| `port` | `8000` | Server listen port |

#### SGLang

| Config Key | Default | Description |
|------------|---------|-------------|
| `image` | `lmsysorg/sglang:latest` | Container image |
| `port` | `30000` | Server listen port |

### Engine Subcommands

```bash
# Customize engine configuration
kubectl rbg llm config set-engine ENGINE_TYPE --config key=value

# List customized engine configurations
kubectl rbg llm config get-engines

# Show details of a specific engine
kubectl rbg llm config get-engines sglang

# Remove custom configuration, revert to defaults
kubectl rbg llm config reset-engine ENGINE_TYPE
```

---

## Command Flags

### add-storage `NAME`

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | `pvc` | Storage type (`pvc`, `oss`) |
| `--config` | - | Configuration key=value pairs, can be specified multiple times |
| `-i, --interactive` | `false` | Interactive configuration mode |

### add-source `NAME`

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | `huggingface` | Source type (`huggingface`, `modelscope`) |
| `--config` | - | Configuration key=value pairs, can be specified multiple times |
| `-i, --interactive` | `false` | Interactive configuration mode |

### set-storage `NAME` / set-source `NAME` / set-engine `ENGINE_TYPE`

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | - | Configuration key=value pairs to update |

### Other Subcommands

`init`, `view`, `get-storages`, `get-sources`, `get-engines`, `use-storage`, `use-source`, `delete-storage`, `delete-source`, `reset-engine` — no additional flags.

## Example

```bash
# 1. Initialize configuration interactively
kubectl rbg llm config init

# 2. Or set up manually
kubectl rbg llm config add-storage my-pvc --type pvc --config pvcName=model-pvc
kubectl rbg llm config add-source hf --type huggingface --config token=hf_xxx

# 3. View current configuration
kubectl rbg llm config view
> Current Configuration:
> 
> Storage: oss-name (active)
>   Type: oss
>   Config:
>     bucket: demo
>     secretName: oss-name-oss-secret
>     secretNamespace: default
>     subpath: /test-cli/
>     url: oss-cn-hongkong-internal.aliyuncs.com
> 
> Source: huggingface (active)
>   Type: huggingface

# 4. Switch active storage/source
kubectl rbg llm config use-storage my-pvc
kubectl rbg llm config use-source hf

# 5. Customize engine (optional)
kubectl rbg llm config set-engine vllm --config image=my-registry/vllm:custom

# 6. List all configurations
kubectl rbg llm config get-storages
> NAME       TYPE  CURRENT
> oss-pvc    pvc   
> oss-name   oss   *

kubectl rbg llm config get-storages oss-name
> Storage: oss-name (active)
>   Type: oss
>   Config:
>     bucket: demo
>     secretName: oss-name-oss-secret
>     secretNamespace: default
>     subpath: /test-cli/
>     url: oss-cn-hongkong-internal.aliyuncs.com

kubectl rbg llm config get-sources
> NAME         TYPE         CURRENT
> huggingface  huggingface  *

kubectl rbg llm config get-sources huggingface
> Source: huggingface (active)
>   Type: huggingface

# 7. Update a configuration
kubectl rbg llm config set-storage my-pvc --config pvcName=new-model-pvc

# 8. Clean up
kubectl rbg llm config reset-engine vllm
kubectl rbg llm config delete-source hf
kubectl rbg llm config delete-storage my-pvc
```
