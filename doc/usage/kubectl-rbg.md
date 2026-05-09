# kubectl-rbg

kubectl-rbg is a local executable command-line tool for managing RBG (RoleBasedGroup) and related resources such as ControllerRevision. Currently, it provides features such as viewing the RBG status, viewing RBG historical revisions, and rolling back the RBG. kubectl-rbg can be used both as a standalone tool or as a kubectl plugin.

## 📜 Deploying kubectl-rbg

### Prerequisites

1. Go 1.24 development environment
2. Access to a Kubernetes cluster

### Installation Steps

```shell
# Download source code
$ git clone https://github.com/sgl-project/rbg.git
# Build locally
$ make build-cli
# Install
$ chmod +x bin/kubectl-rbg
$ sudo mv bin/kubectl-rbg /usr/local/bin/
```

### Verify Installation

```shell
$ kubectl plugin list | grep rbg
# Expected output
/usr/local/bin/kubectl-rbg

$ kubectl rbg -h
# The above command works the same as "kubectl-rbg -h"
Kubectl plugin for RoleBasedGroup

Usage:
  kubectl rbg [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  llm         LLM deployment management commands
  rollout     Manage the rollout of a rbg object
  status      Display rbg status information

Flags:
      --as string                      Username to impersonate for the operation. User could be a regular user or a service account in a namespace.
      --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --as-uid string                  UID to impersonate for the operation.
      --cache-dir string               Default cache directory (default "~/.kube/cache")
      --certificate-authority string   Path to a cert file for the certificate authority
      --client-certificate string      Path to a client certificate file for TLS
      --client-key string              Path to a client key file for TLS
      --cluster string                 The name of the kubeconfig cluster to use
      --context string                 The name of the kubeconfig context to use
      --disable-compression            If true, opt-out of response compression for all requests to the server
  -h, --help                           help for rbg
      --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
      --kubeconfig string              Path to the kubeconfig file to use for CLI requests.
  -n, --namespace string               If present, the namespace scope for this CLI request
      --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
  -s, --server string                  The address and port of the Kubernetes API server
      --tls-server-name string         Server name to use for server certificate validation. If it is not provided, the hostname used to contact the server is used
      --token string                   Bearer token for authentication to the API server
      --user string                    The name of the kubeconfig user to use
  -v, --v Level                        number for the log level verbosity
      --version                        version for rbg
```

## 📖 Feature Overview

### Command Reference

| Command | Description | Documentation |
|---------|-------------|---------------|
| `status` | Display RBG status with role readiness and replica counts | [kubectl-rbg-status](kubectl-rbg-status.md) |
| `rollout` | Manage rollout history, diff, and undo for RBG resources | [kubectl-rbg-rollout](kubectl-rbg-rollout.md) |
| `llm svc` | Manage LLM inference services — deploy, list, delete, and chat | [kubectl-rbg-llm-svc](kubectl-rbg-llm-svc.md) |
| `llm model` | Manage LLM models in storage — pull and list models | [kubectl-rbg-llm-model](kubectl-rbg-llm-model.md) |
| `llm config` | Manage LLM configuration — storage, source, and engine settings | [kubectl-rbg-llm-config](kubectl-rbg-llm-config.md) |
| `llm benchmark` | Run performance benchmarks against deployed services | [kubectl-rbg-benchmark](kubectl-rbg-benchmark.md) |
| `llm generate` | Generate optimized RBG deployment configurations using AI Configurator | [kubectl-rbg-llm-generate](kubectl-rbg-llm-generate.md) |

### RBG Resource Management

```bash
# View RBG status
kubectl rbg status nginx-cluster

# View rollout history
kubectl rbg rollout history nginx-cluster

# Compare current RBG with a specific revision
kubectl rbg rollout diff nginx-cluster --revision=1

# Rollback to a specific revision
kubectl rbg rollout undo nginx-cluster --revision=1
```

For detailed usage and output examples, see [kubectl-rbg-status](kubectl-rbg-status.md) and [kubectl-rbg-rollout](kubectl-rbg-rollout.md).

### LLM Deployment Quick Start

```bash
# 1. Initialize configuration (storage and source)
kubectl rbg llm config init

# 2. Pull a model
kubectl rbg llm model pull Qwen/Qwen3.5-0.8B

# 3. Deploy as an inference service
kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B

# 4. Chat with the service
kubectl rbg llm svc chat my-qwen

# 5. List running services
kubectl rbg llm svc list

# 6. Delete the service
kubectl rbg llm svc delete my-qwen
```
