# Auto-Benchmark: conversation_replay Workload

## Overview

`conversation_replay` is an auto-benchmark workload type that simulates real multi-turn Agent conversations (e.g. Coding Agent workflows such as Claude Code, Cursor).

Compared with `fixed` / `normal` / `uniform`, which can only model single-turn requests, `conversation_replay` supports:

| Capability | Description |
|------------|-------------|
| **System Prompt** | Shared system prompt (base behavioral contract) + dynamic system prompt (CLAUDE.md / MCP instructions, etc.) |
| **Multi-turn** | Multi-turn conversations with independent input/output token distributions per turn |
| **Tool Call Latency** | Simulated tool-call latency between turns (file reads, grep, code execution, etc.) |

> **Note**: `conversation_replay` is only supported by the `inference-perf` evaluator, not by `genai-bench`.

## Configuration Layout

In an auto-benchmark config file, set `scenario.workload` to `"conversation_replay"`, and put the detailed parameters under `evaluator.config.conversationReplay`.

```yaml
scenario:
  workload: "conversation_replay"    # workload type keyword

evaluator:
  type: inference-perf               # must use inference-perf
  config:
    warmup:                          # optional: KV Cache warmup
      numRequests: 50                # absolute warmup request count, OR
      # ratio: 0.1                   # warmup ratio (0~1, mutually exclusive with numRequests)
    conversationReplay:              # conversation_replay parameters
      numConversations: ...
      turnsPerConversation: ...
      ...
```

### Warmup Configuration

For workloads with a shared prefix (shared system prompt) such as `conversation_replay`, KV Cache hit rate is unstable: early requests miss the cache and incur high latency, then stabilize as the cache fills up.

The `warmup` configuration sends warmup requests before the measurement phase to populate the KV Cache, ensuring stable and reliable measurement data.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `numRequests` | int | No* | Absolute number of warmup requests |
| `ratio` | float | No* | Warmup ratio (0 < ratio < 1), e.g. 0.1 means 10% of requests are used for warmup |

*Note: `numRequests` and `ratio` are mutually exclusive; if both are set, `numRequests` takes precedence. If neither is set, warmup is disabled.

**Recommended values:**

| Scenario | Recommended Warmup | Notes |
|----------|--------------------|-------|
| Short shared prefix (<1K tokens) | 10–20 or 5% | cache fills quickly |
| Medium shared prefix (1K–5K tokens) | 30–50 or 10% | balanced |
| Long shared prefix (>5K tokens) | 50–100 or 15% | cache fills slowly |

**How it works:** auto-benchmark generates a 2-stage inference-perf config — Stage 0 (warmup) and Stage 1 (measurement). When collecting results, Stage 0 is skipped automatically and only Stage 1 metrics are used.

### Parameters

#### Top-level Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `numConversations` | int | Yes | Total number of conversations to generate; must be > 0 |
| `sharedSystemPromptLen` | int | No | Shared system prompt length in tokens; default 0 |
| `seed` | int | No | Random seed; falls back to `evaluator.config.baseSeed`, then to 0 |

#### Distribution Parameters

The following parameters are all distribution configurations. Each distribution contains:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Distribution type: `normal`, `lognormal`, `uniform`, `fixed` |
| `min` | float | Minimum value (optional) |
| `max` | float | Maximum value (optional) |
| `mean` | float | Mean; used by `normal` / `lognormal` |
| `stdDev` | float | Standard deviation; used by `normal` / `lognormal` |

#### Distribution Parameter List

| Parameter | Required | Description |
|-----------|----------|-------------|
| `turnsPerConversation` | Yes | Distribution of the number of turns per conversation |
| `inputTokensPerTurn` | Yes | Distribution of input tokens per turn |
| `outputTokensPerTurn` | Yes | Distribution of output tokens per turn |
| `dynamicSystemPromptLen` | No | Dynamic system prompt length distribution (sampled per conversation) |
| `toolCallLatencySec` | No | Inter-turn tool-call latency distribution (seconds) |

## Examples

### Example 1: Full Agent Workflow Simulation

Simulates a Claude Code-style Coding Agent with a complete system prompt, multi-turn conversation, and tool-call latency.

```yaml
name: coding-agent-benchmark

templates:
  - name: qwen3-8b-tp4
    template: ./templates/qwen3-8b-tp4.yaml

backend: sglang

searchSpace:
  default:
    maxNumSeqs:
      type: categorical
      values: [64, 128, 256]

scenario:
  name: coding-agent
  workload: "conversation_replay"
  concurrency: 8
  maxRequests: 1000

objectives:
  sla:
    ttftP99MaxMs: 3000
    tpotP99MaxMs: 80
    errorRateMax: 0.01
  optimize: outputThroughput

strategy:
  algorithm: grid
  maxTrialsPerTemplate: 5

evaluator:
  type: inference-perf
  config:
    tokenizerSource: Qwen/Qwen3-8B
    baseSeed: 42
    apiType: completion
    streaming: true
    warmup:
      numRequests: 100                # warm up KV Cache with the first 100 requests (shared 6K system prompt)
    conversationReplay:
      numConversations: 200
      sharedSystemPromptLen: 6000          # base behavioral contract + tool descriptions ~6K tokens
      dynamicSystemPromptLen:              # CLAUDE.md / MCP instructions / environment info
        type: normal
        min: 2000
        max: 10000
        mean: 5000
        stdDev: 1500
      turnsPerConversation:                # 3~10 turns per conversation
        type: normal
        min: 3
        max: 10
        mean: 6
        stdDev: 2
      inputTokensPerTurn:                  # input dominated by tool outputs (file reads, grep, etc.)
        type: lognormal
        min: 256
        max: 6000
        mean: 1500
        stdDev: 1200
      outputTokensPerTurn:                 # output is tool calls + code generation
        type: lognormal
        min: 128
        max: 3000
        mean: 800
        stdDev: 400
      toolCallLatencySec:                  # tool execution latency (compile, test, file I/O, etc.)
        type: lognormal
        min: 1
        max: 30
        mean: 8
        stdDev: 6

results:
  pvc: benchmark-output
  subPath: coding-agent-exp
```

### Example 2: Simple Multi-turn Chat (no tool calls)

Tests multi-turn capability only, without a system prompt or tool-call latency.

```yaml
name: multi-turn-chat

templates:
  - name: qwen3-8b-tp2
    template: ./templates/qwen3-8b-tp2.yaml

backend: sglang

searchSpace:
  default:
    gpuMemoryUtilization:
      type: categorical
      values: [0.85, 0.90, 0.95]

scenario:
  name: multi-turn
  workload: "conversation_replay"
  concurrency: 16
  maxRequests: 500

objectives:
  sla:
    ttftP99MaxMs: 2000
    tpotP99MaxMs: 50
    errorRateMax: 0.01
  optimize: outputThroughput

strategy:
  algorithm: grid
  maxTrialsPerTemplate: 3

evaluator:
  type: inference-perf
  config:
    tokenizerSource: Qwen/Qwen3-8B
    conversationReplay:
      numConversations: 100
      turnsPerConversation:
        type: uniform
        min: 2
        max: 8
      inputTokensPerTurn:
        type: normal
        min: 50
        max: 2000
        mean: 500
        stdDev: 300
      outputTokensPerTurn:
        type: normal
        min: 50
        max: 1000
        mean: 300
        stdDev: 150

results:
  pvc: benchmark-output
  subPath: multi-turn-exp
```

### Example 3: Long System Prompt Stress Test

Specifically tests the impact of a long system prompt on inference performance.

```yaml
name: long-system-prompt

templates:
  - name: qwen3-8b-tp4
    template: ./templates/qwen3-8b-tp4.yaml

backend: sglang

searchSpace:
  default:
    contextLength:
      type: categorical
      values: [16384, 32768]

scenario:
  name: long-prompt
  workload: "conversation_replay"
  concurrency: 4
  maxRequests: 200

objectives:
  sla:
    ttftP99MaxMs: 5000
    errorRateMax: 0.02
  optimize: outputThroughput

strategy:
  algorithm: grid
  maxTrialsPerTemplate: 4

evaluator:
  type: inference-perf
  config:
    tokenizerSource: Qwen/Qwen3-8B
    baseSeed: 123
    conversationReplay:
      seed: 123
      numConversations: 50
      sharedSystemPromptLen: 10000         # extra-long shared system prompt
      dynamicSystemPromptLen:
        type: normal
        min: 5000
        max: 20000
        mean: 12000
        stdDev: 3000
      turnsPerConversation:
        type: fixed
        min: 3
        max: 3
      inputTokensPerTurn:
        type: uniform
        min: 200
        max: 1000
      outputTokensPerTurn:
        type: uniform
        min: 100
        max: 500

results:
  pvc: benchmark-output
  subPath: long-prompt-exp
```

## Workload Type Comparison

| Workload | Syntax | Use Case |
|----------|--------|----------|
| `fixed(in,out)` | `fixed(512,256)` | Single-turn requests with fixed token counts |
| `normal(μ_in,σ_in/μ_out,σ_out)` | `normal(480,240/300,150)` | Single-turn requests with normal distribution |
| `uniform(min,max/min,max)` | `uniform(100,500/200,800)` | Single-turn requests with uniform distribution |
| `dataset` | `dataset` | Uses the ShareGPT dataset |
| `conversation_replay` | `conversation_replay` | Multi-turn Agent conversation simulation |

## Generated inference-perf Config

auto-benchmark automatically converts the above config into native inference-perf YAML format (snake_case). For example, the `data` section generated from Example 1:

```yaml
data:
  type: conversation_replay
  conversation_replay:
    seed: 42
    num_conversations: 200
    shared_system_prompt_len: 6000
    dynamic_system_prompt_len:
      type: normal
      min: 2000
      max: 10000
      mean: 5000
      std_dev: 1500
    turns_per_conversation:
      type: normal
      min: 3
      max: 10
      mean: 6
      std_dev: 2
    input_tokens_per_turn:
      type: lognormal
      min: 256
      max: 6000
      mean: 1500
      std_dev: 1200
    output_tokens_per_turn:
      type: lognormal
      min: 128
      max: 3000
      mean: 800
      std_dev: 400
    tool_call_latency_sec:
      type: lognormal
      min: 1
      max: 30
      mean: 8
      std_dev: 6
```
