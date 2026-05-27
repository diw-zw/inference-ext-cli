# RBG LLM Config Commands

## Overview

The `llmctl config` command group manages the CLI configuration for LLM deployments, including storage backends, model download sources, and inference engine settings.

Configuration is stored locally at `~/.rbg/config`.

## Usage

### Initialize Configuration

The interactive wizard guides you through setting up storage and source in one step:

```bash
llmctl config init
```

### View Configuration

```bash
llmctl config view
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
llmctl config add-storage my-pvc --type pvc --config pvcName=model-pvc
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
llmctl config add-storage my-oss --type oss \
  --config url=oss-cn-hangzhou.aliyuncs.com \
  --config bucket=my-bucket \
  --config akId=MY_ACCESS_KEY_ID \
  --config akSecret=MY_ACCESS_KEY_SECRET
```

The credentials are stored as a Kubernetes Secret; only the secret reference is saved in the config file.

### Storage Subcommands

```bash
# Add a storage configuration
llmctl config add-storage NAME --type TYPE --config key=value

# Add interactively
llmctl config add-storage NAME -i

# List all storage configurations
llmctl config get-storages

# Show details of a specific storage
llmctl config get-storages my-pvc

# Set the active storage
llmctl config use-storage NAME

# Update a storage configuration
llmctl config set-storage NAME --config key=value

# Delete a storage configuration
llmctl config delete-storage NAME
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
llmctl config add-source hf --type huggingface --config token=hf_xxx
```

#### ModelScope

| Config Key | Required | Description |
|------------|----------|-------------|
| `token` | No | ModelScope API token (required for private models) |
| `tokenSecret` | No | Kubernetes Secret name containing `MODELSCOPE_TOKEN` (takes precedence over `token`) |

```bash
llmctl config add-source ms --type modelscope --config token=xxx
```

### Source Subcommands

```bash
# Add a source configuration
llmctl config add-source NAME --type TYPE --config key=value

# Add interactively
llmctl config add-source NAME -i

# List all source configurations
llmctl config get-sources

# Show details of a specific source
llmctl config get-sources huggingface

# Set the active source
llmctl config use-source NAME

# Update a source configuration
llmctl config set-source NAME --config key=value

# Delete a source configuration
llmctl config delete-source NAME
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
llmctl config set-engine ENGINE_TYPE --config key=value

# List customized engine configurations
llmctl config get-engines

# Show details of a specific engine
llmctl config get-engines sglang

# Remove custom configuration, revert to defaults
llmctl config reset-engine ENGINE_TYPE
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
llmctl config init

# 2. Or set up manually
llmctl config add-storage my-pvc --type pvc --config pvcName=model-pvc
llmctl config add-source hf --type huggingface --config token=hf_xxx

# 3. View current configuration
llmctl config view
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
llmctl config use-storage my-pvc
llmctl config use-source hf

# 5. Customize engine (optional)
llmctl config set-engine vllm --config image=my-registry/vllm:custom

# 6. List all configurations
llmctl config get-storages
> NAME       TYPE  CURRENT
> oss-pvc    pvc   
> oss-name   oss   *

llmctl config get-storages oss-name
> Storage: oss-name (active)
>   Type: oss
>   Config:
>     bucket: demo
>     secretName: oss-name-oss-secret
>     secretNamespace: default
>     subpath: /test-cli/
>     url: oss-cn-hongkong-internal.aliyuncs.com

llmctl config get-sources
> NAME         TYPE         CURRENT
> huggingface  huggingface  *

llmctl config get-sources huggingface
> Source: huggingface (active)
>   Type: huggingface

# 7. Update a configuration
llmctl config set-storage my-pvc --config pvcName=new-model-pvc

# 8. Clean up
llmctl config reset-engine vllm
llmctl config delete-source hf
llmctl config delete-storage my-pvc
```
