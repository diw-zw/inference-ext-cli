/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

func init() {
	Register("genai-bench", func() Evaluator { return &GenAIBench{} })
}

// GenAIBench implements the Evaluator interface using the genai-bench tool.
type GenAIBench struct {
	tokenizerSource string
	apiKey          string
}

// Name returns the evaluator name.
func (g *GenAIBench) Name() string { return "genai-bench" }

// Init reads plugin-specific config. Expected keys: tokenizerSource (string), apiKey (string).
func (g *GenAIBench) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["tokenizerSource"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("genai-bench: tokenizerSource must be a string, got %T", v)
		}
		g.tokenizerSource = s
	}
	if v, ok := cfg["apiKey"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("genai-bench: apiKey must be a string, got %T", v)
		}
		g.apiKey = s
	}
	return nil
}

// Run executes genai-bench as a subprocess.
func (g *GenAIBench) Run(ctx context.Context, evalCtx EvalContext) error {
	args := g.buildGenAIBenchArgs(evalCtx)

	logger := log.FromContext(ctx)
	logger.Info("Running genai-bench", "command", strings.Join(append([]string{"genai-bench"}, args...), " "))

	cmd := exec.CommandContext(ctx, "genai-bench", args...)
	cmd.Env = append(os.Environ(), "ENABLE_UI=false")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// genai-bench does not expose a "benchmark-only" mode; after all benchmark
		// runs finish it unconditionally generates Excel/plots, which can fail in
		// container environments (e.g. PVC without random-write support). Since
		// genai-bench writes one result JSON per (scenario × concurrency) immediately
		// after each run completes, we use the file count as an indirect indicator:
		// all expected JSONs present ⇒ benchmarks finished, failure is post-benchmark.
		expected := expectedResultCount(evalCtx.Scenario)
		actual := countResultJSON(evalCtx.OutputDir)
		if expected > 0 && actual >= expected {
			logger.Info("genai-bench exited with error but all result files exist, treating as non-fatal",
				"error", err, "expectedFiles", expected, "actualFiles", actual)
		} else {
			return fmt.Errorf("genai-bench failed (expected %d result files, found %d): %w", expected, actual, err)
		}
	}
	return nil
}

// expectedResultCount returns the number of result JSON files genai-bench
// should produce: one per (workload × concurrency) combination.
func expectedResultCount(scenario config.ScenarioSpec) int {
	w := len(scenario.Workloads)
	c := len(scenario.Concurrency)
	if w == 0 {
		w = 1
	}
	if c == 0 {
		c = 1
	}
	return w * c
}

// countResultJSON counts benchmark result JSON files in dir,
// excluding metadata files like experiment_metadata.json.
func countResultJSON(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && e.Name() != "experiment_metadata.json" {
			count++
		}
	}
	return count
}

// CollectResults reads result JSON files from the given directory and aggregates metrics.
// genai-bench writes one JSON file per concurrency level (e.g., D100_100_text-to-text_num_concurrency_4_time_16s.json).
// Metrics are aggregated across all result files using worst-case semantics for SLA checking.
func (g *GenAIBench) CollectResults(resultDir string) (*abtypes.Metrics, error) {
	entries, err := os.ReadDir(resultDir)
	if err != nil {
		return nil, fmt.Errorf("reading result directory %q: %w", resultDir, err)
	}

	results := make([]benchmarkResult, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || name == "experiment_metadata.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(resultDir, name))
		if err != nil {
			return nil, fmt.Errorf("reading result file %q: %w", name, err)
		}
		var r benchmarkResult
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parsing result file %q: %w", name, err)
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no result JSON files found in %q", resultDir)
	}

	return aggregateResults(results), nil
}

// aggregateResults merges metrics across multiple concurrency-level results.
// Uses worst-case for latency/error metrics (SLA-oriented) and average for throughput.
func aggregateResults(results []benchmarkResult) *abtypes.Metrics {
	m := &abtypes.Metrics{}
	n := float64(len(results))

	var sumOutputTP, sumInputTP, sumTotalTP, sumRPS float64
	var maxErrorRate float64
	var maxTTFTP50, maxTTFTP99, maxTPOTP50, maxTPOTP99 float64
	var totalCompleted, totalErrors, totalRequests int

	for _, r := range results {
		am := r.AggregatedMetrics
		sumOutputTP += am.MeanOutputThroughput
		sumInputTP += am.MeanInputThroughput
		sumTotalTP += am.MeanTotalTokensThroughput
		sumRPS += am.RequestsPerSecond
		maxErrorRate = math.Max(maxErrorRate, am.ErrorRate)
		totalCompleted += am.NumCompletedRequests
		totalErrors += am.NumErrorRequests
		totalRequests += am.NumRequests

		if ttft, ok := am.Stats["ttft"]; ok {
			maxTTFTP50 = math.Max(maxTTFTP50, ttft.P50)
			maxTTFTP99 = math.Max(maxTTFTP99, ttft.P99)
		}
		if tpot, ok := am.Stats["tpot"]; ok {
			maxTPOTP50 = math.Max(maxTPOTP50, tpot.P50)
			maxTPOTP99 = math.Max(maxTPOTP99, tpot.P99)
		}
	}

	m.OutputThroughput = sumOutputTP / n
	m.InputThroughput = sumInputTP / n
	m.TotalThroughput = sumTotalTP / n
	m.RequestsPerSecond = sumRPS / n
	m.ErrorRate = maxErrorRate
	m.NumCompletedRequests = totalCompleted
	m.NumErrorRequests = totalErrors
	m.NumRequests = totalRequests
	m.TTFTP50 = maxTTFTP50
	m.TTFTP99 = maxTTFTP99
	m.TPOTP50 = maxTPOTP50
	m.TPOTP99 = maxTPOTP99

	return m
}

// benchmarkResult mirrors the genai-bench output structure.
type benchmarkResult struct {
	AggregatedMetrics aggregatedMetrics `json:"aggregated_metrics"`
}

type aggregatedMetrics struct {
	MeanOutputThroughput      float64          `json:"mean_output_throughput_tokens_per_s"`
	MeanInputThroughput       float64          `json:"mean_input_throughput_tokens_per_s"`
	MeanTotalTokensThroughput float64          `json:"mean_total_tokens_throughput_tokens_per_s"`
	RequestsPerSecond         float64          `json:"requests_per_second"`
	ErrorRate                 float64          `json:"error_rate"`
	NumCompletedRequests      int              `json:"num_completed_requests"`
	NumErrorRequests          int              `json:"num_error_requests"`
	NumRequests               int              `json:"num_requests"`
	Stats                     map[string]stats `json:"stats"`
}

type stats struct {
	P50 float64 `json:"p50"`
	P99 float64 `json:"p99"`
}

// buildGenAIBenchArgs constructs the command-line arguments for genai-bench.
// It translates the generic ScenarioSpec into genai-bench specific CLI flags.
func (g *GenAIBench) buildGenAIBenchArgs(evalCtx EvalContext) []string {
	scenario := evalCtx.Scenario
	backend := evalCtx.Backend
	if backend == "" {
		backend = "sglang"
	}
	apiKey := g.apiKey
	if apiKey == "" {
		apiKey = "EMPTY"
	}
	args := []string{
		"benchmark",
		"--api-backend", backend,
		"--api-base", evalCtx.Endpoint,
		"--api-model-name", evalCtx.ModelName,
		"--api-key", apiKey,
		"--task", "text-to-text",
	}

	// Translate workloads to genai-bench traffic scenarios
	for _, w := range scenario.Workloads {
		ts := translateWorkload(w)
		args = append(args, "--traffic-scenario", ts)
	}

	for _, c := range scenario.Concurrency {
		args = append(args, "--num-concurrency", strconv.Itoa(c))
	}

	// Duration -> --max-time-per-run (in minutes)
	if scenario.Duration != "" {
		if d, err := time.ParseDuration(scenario.Duration); err == nil {
			minutes := int(d.Minutes())
			if minutes < 1 {
				minutes = 1
			}
			args = append(args, "--max-time-per-run", strconv.Itoa(minutes))
		}
	}

	if scenario.MaxRequests > 0 {
		args = append(args, "--max-requests-per-run", strconv.Itoa(scenario.MaxRequests))
	}

	// Output directory: split into base dir + folder name for genai-bench convention.
	if evalCtx.OutputDir != "" {
		baseDir := filepath.Dir(evalCtx.OutputDir)
		folderName := filepath.Base(evalCtx.OutputDir)
		args = append(args, "--experiment-base-dir", baseDir, "--experiment-folder-name", folderName)
	}

	if g.tokenizerSource != "" {
		args = append(args, "--model-tokenizer", g.tokenizerSource)
	}

	return args
}

// translateWorkload converts a project-own workload string to genai-bench traffic scenario format.
func translateWorkload(w string) string {
	wl, err := config.ParseWorkload(w)
	if err != nil {
		return w
	}
	switch wl.Type {
	case config.WorkloadFixed:
		return fmt.Sprintf("D(%d,%d)", wl.InputTokens, wl.OutputTokens)
	case config.WorkloadNormal:
		return fmt.Sprintf("N(%d,%d)/(%d,%d)", wl.InputMean, wl.InputStdDev, wl.OutputMean, wl.OutputStdDev)
	case config.WorkloadUniform:
		return fmt.Sprintf("U(%d,%d)/(%d,%d)", wl.InputMin, wl.InputMax, wl.OutputMin, wl.OutputMax)
	case config.WorkloadDataset:
		return "dataset"
	default:
		return w
	}
}
