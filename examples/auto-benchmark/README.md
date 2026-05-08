# Auto Benchmark Example

This example demonstrates how to use RBG's auto-benchmark feature to automatically tune LLM serving parameters for optimal performance.

## Overview

Auto-benchmark uses Bayesian optimization (TPE algorithm) to search through parameter combinations, benchmark each configuration, and find the optimal settings that maximize throughput while meeting SLA constraints (TTFT, TPOT, error rate).

## Files in This Directory

| File | Description |
|------|-------------|
| `README.md` | This documentation |
| `config.yaml` | Auto-benchmark configuration (search space, workloads, objectives) |
| `template.yaml` | RBG template defining the model serving deployment |

## Prerequisites

- RBG CLI installed (`kubectl rbg`)
- Kubernetes cluster with GPU nodes
- PVC for storing benchmark results
- Docker/Buildah for building images (or use pre-built images)

## Build Images

Two images need to be built: the auto-benchmark controller and the dashboard UI.

### Auto-Benchmark Controller Image

```bash
docker build -f cmd/autobenchmark/Dockerfile \
  -t <your-registry>/rbgs-auto-benchmark:<tag> \
  .
```

This image includes:
- Auto-benchmark Go controller binary
- Python runtime with genai-bench and optuna
- Optuna bridge script for Bayesian optimization

Push to your registry:

```bash
docker push <your-registry>/rbgs-auto-benchmark:<tag>
```

### Dashboard UI Image

```bash
cd ui/benchmark-viewer
docker build -t <your-registry>/rbgs-ab-dashboard:<tag> .
docker push <your-registry>/rbgs-ab-dashboard:<tag>
```

This is a multi-stage build (Node.js → Nginx) serving the React dashboard.

## Quick Start

### Step 1: Prepare RBAC

```bash
kubectl create -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: auto-benchmark-role
rules:
- apiGroups:
  - workloads.x-k8s.io
  resources:
  - rolebasedgroups
  verbs:
  - get
  - create
  - delete
EOF

kubectl create -f - << EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: auto-benchmark-controller
EOF

kubectl create -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: auto-benchmark-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: auto-benchmark-role
subjects:
- kind: ServiceAccount
  name: auto-benchmark-controller
EOF
```

### Step 2: Customize Configuration

Edit `config.yaml` to match your environment:

- **templates**: Reference your RBG template file
- **backend**: Set to `sglang` or `vllm`
- **searchSpace**: Define parameter ranges to explore
- **scenarios**: Configure workload patterns and concurrency levels
- **objectives**: Set SLA constraints and optimization target
- **evaluator**: Configure tokenizer path
- **results**: Specify PVC for storing results

### Step 3: Run Auto-Benchmark

```bash
kubectl rbg llm auto-benchmark run \
  -f config.yaml \
  --name qwen3-8b \
  --service-account auto-benchmark-controller \
  --image <your-auto-benchmark-image>
```

The command will:
1. Parse config.yaml and generate trial combinations
2. Create RBG deployments for each trial
3. Run benchmarks and collect metrics
4. Use TPE algorithm to guide next trials toward better parameters
5. Store all results in the configured PVC

### Step 4: View Dashboard

While the experiment is running (or after completion), view results:

```bash
kubectl rbg llm auto-benchmark dashboard <experiment-name> \
  -f config.yaml \
  --image <your-dashboard-image>
```

This will:
- Create a temporary dashboard deployment
- Port-forward to your local machine
- Auto-open browser with results
- Clean up on exit (Ctrl+C)

### Step 5: Stop Experiment (Optional)

```bash
kubectl rbg llm auto-benchmark stop <experiment-name>
```

## Configuration Highlights

### Search Space

```yaml
searchSpace:
  default:
    gpuMemoryUtilization:
      type: categorical
      values: [0.70, 0.80, 0.90, 0.95]
    maxNumSeqs:
      type: categorical
      values: [256, 512, 1024, 2048]
    chunkedPrefillSize:
      type: categorical
      values: [512, 2048, 8192]
```

Parameters are mapped to the serving engine's CLI flags automatically (see `mapper.go`).

### Workload Scenario

```yaml
scenario:
  name: chat
  workloads:
  - "normal(512,256/2048,1024)"
  concurrency: [64, 128, 256]
  duration: 3m
  maxRequests: 500
```

- **workloads**: `normal(mean, stddev/min,max/output)` - 输入长度正态分布 (均值 512, 标准差 256, 范围 256-2048)，输出固定 1024
- **concurrency**: 在每个并发级别 (64, 128, 256) 分别运行 benchmark
- **duration**: 每个 trial 持续 3 分钟
- **maxRequests**: 最多发送 500 个请求

### SLA Constraints

```yaml
objectives:
  sla:
    ttftP99MaxMs: 2000       # TTFT P99 ≤ 2000ms
    tpotP99MaxMs: 15         # TPOT P99 ≤ 15ms
    errorRateMax: 0.01       # Error rate ≤ 1%
  optimize: outputThroughput # Maximize throughput within SLA
```

Trials violating SLA constraints are marked as failed regardless of throughput.

## Dashboard Features

- **Overview**: Experiment summary, best trial, parameter rankings
- **Parallel Coordinates**: Visualize parameter-performance relationships
- **Parameter Impact**: See which parameters affect scores most
- **Raw Data**: Full trial details in JSON format
- **Score Distribution**: Histogram of trial scores

Color coding: 🟢 Green = good (low ratio), 🔴 Red = poor (high ratio), based on optimization direction.

## Tips

- Use **mixed-length workloads** (`normal` or `uniform`) instead of `fixed` to see parameter differences
- Higher concurrency levels (128, 256) are needed to saturate GPU and expose bottlenecks
- Expand parameter ranges for more pronounced performance differences
- Longer durations (5m+) provide more stable measurements
- The dashboard auto-opens in browser and cleans up on exit
