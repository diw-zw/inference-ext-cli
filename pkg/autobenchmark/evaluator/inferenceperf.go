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
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

const defaultNumRequests = 500

func init() {
	Register("inference-perf", func() Evaluator { return &InferencePerf{} })
}

// InferencePerf implements the Evaluator interface using the inference-perf tool
// (kubernetes-sigs/inference-perf).
type InferencePerf struct {
	tokenizerSource string
	apiKey          string
	baseSeed        *int
	apiType         string
	streaming       *bool
	datasetPath     string
}

// Name returns the evaluator name.
func (ip *InferencePerf) Name() string { return "inference-perf" }

// Init reads plugin-specific config.
// Expected keys: tokenizerSource (string), apiKey (string), baseSeed (int/float64),
// apiType (string), streaming (bool), and datasetPath (string).
func (ip *InferencePerf) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["tokenizerSource"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("inference-perf: tokenizerSource must be a string, got %T", v)
		}
		ip.tokenizerSource = s
	}
	if v, ok := cfg["apiKey"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("inference-perf: apiKey must be a string, got %T", v)
		}
		ip.apiKey = s
	}
	if v, ok := cfg["baseSeed"]; ok {
		switch n := v.(type) {
		case int:
			ip.baseSeed = &n
		case float64:
			i := int(n)
			ip.baseSeed = &i
		default:
			return fmt.Errorf("inference-perf: baseSeed must be a number, got %T", v)
		}
	}
	if v, ok := cfg["apiType"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("inference-perf: apiType must be a string, got %T", v)
		}
		ip.apiType = s
	}
	if v, ok := cfg["streaming"]; ok {
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("inference-perf: streaming must be a bool, got %T", v)
		}
		ip.streaming = &b
	}
	if v, ok := cfg["datasetPath"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("inference-perf: datasetPath must be a string, got %T", v)
		}
		ip.datasetPath = s
	}
	return nil
}

// Run generates an inference-perf YAML config and executes it as a subprocess.
func (ip *InferencePerf) Run(ctx context.Context, evalCtx EvalContext) error {
	cfg, err := ip.buildConfig(evalCtx)
	if err != nil {
		return fmt.Errorf("building inference-perf config: %w", err)
	}

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling inference-perf config: %w", err)
	}

	if err := os.MkdirAll(evalCtx.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	configPath := filepath.Join(evalCtx.OutputDir, "inference-perf-config.yaml")
	if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("writing inference-perf config: %w", err)
	}

	logger := log.FromContext(ctx)
	logger.Info("Running inference-perf", "configFile", configPath)

	cmd := exec.CommandContext(ctx, "inference-perf", "--config_file", configPath)
	cmd.Dir = evalCtx.OutputDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("inference-perf failed: %w", err)
	}
	return nil
}

// CollectResults reads inference-perf output files and extracts metrics.
// It looks for per-stage files first (for worst-case latency aggregation),
// falling back to the summary file.
func (ip *InferencePerf) CollectResults(resultDir string) (*abtypes.Metrics, error) {
	reportsDir, err := findReportsDir(resultDir)
	if err != nil {
		return nil, err
	}

	stageResults, stageErr := readStageFiles(reportsDir)
	if stageErr != nil {
		return nil, fmt.Errorf("reading stage files in %q: %w", reportsDir, stageErr)
	}
	if len(stageResults) > 0 {
		return aggregateInfPerfResults(stageResults), nil
	}

	summaryPath := filepath.Join(reportsDir, "summary_lifecycle_metrics.json")
	result, err := readInfPerfResultFile(summaryPath)
	if err != nil {
		return nil, fmt.Errorf("no stage or summary files found in %q: %w", reportsDir, err)
	}
	return mapInfPerfToMetrics(result), nil
}

// --- Config generation ---

// infPerfConfig mirrors the inference-perf YAML configuration structure.
type infPerfConfig struct {
	Server    infPerfServer     `yaml:"server"`
	API       infPerfAPI        `yaml:"api"`
	Tokenizer *infPerfTokenizer `yaml:"tokenizer,omitempty"`
	Data      infPerfData       `yaml:"data"`
	Load      infPerfLoad       `yaml:"load"`
	Report    infPerfReport     `yaml:"report"`
}

type infPerfServer struct {
	Type      string `yaml:"type"`
	ModelName string `yaml:"model_name"`
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key,omitempty"`
	IgnoreEOS bool   `yaml:"ignore_eos"`
}

type infPerfAPI struct {
	Type      string `yaml:"type"`
	Streaming bool   `yaml:"streaming"`
}

type infPerfTokenizer struct {
	PretrainedModelNameOrPath string `yaml:"pretrained_model_name_or_path"`
}

type infPerfData struct {
	Type               string               `yaml:"type"`
	Path               string               `yaml:"path,omitempty"`
	InputDistribution  *infPerfDistribution `yaml:"input_distribution,omitempty"`
	OutputDistribution *infPerfDistribution `yaml:"output_distribution,omitempty"`
}

type infPerfDistribution struct {
	Type       string   `yaml:"type,omitempty"`
	Min        *int     `yaml:"min,omitempty"`
	Max        *int     `yaml:"max,omitempty"`
	Mean       *float64 `yaml:"mean,omitempty"`
	StdDev     *float64 `yaml:"std_dev,omitempty"`
	TotalCount int      `yaml:"total_count"`
}

type infPerfLoad struct {
	Type     string                   `yaml:"type"`
	BaseSeed *int                     `yaml:"base_seed,omitempty"`
	Stages   []infPerfConcurrentStage `yaml:"stages"`
}

type infPerfConcurrentStage struct {
	NumRequests      int `yaml:"num_requests"`
	ConcurrencyLevel int `yaml:"concurrency_level"`
}

type infPerfReport struct {
	RequestLifecycle infPerfRequestLifecycle `yaml:"request_lifecycle"`
}

type infPerfRequestLifecycle struct {
	Summary     bool  `yaml:"summary"`
	PerStage    bool  `yaml:"per_stage"`
	Percentiles []int `yaml:"percentiles"`
}

func (ip *InferencePerf) buildConfig(evalCtx EvalContext) (*infPerfConfig, error) {
	scenario := evalCtx.Scenario
	backend := evalCtx.Backend
	if backend == "" {
		backend = "sglang"
	}
	apiKey := ip.apiKey
	if apiKey == "" {
		apiKey = "EMPTY"
	}

	numRequests := defaultNumRequests
	if scenario.MaxRequests > 0 {
		numRequests = scenario.MaxRequests
	}

	apiType := ip.apiType
	if apiType == "" {
		apiType = "completion"
	}
	streaming := true
	if ip.streaming != nil {
		streaming = *ip.streaming
	}

	cfg := &infPerfConfig{
		Server: infPerfServer{
			Type:      backend,
			ModelName: evalCtx.ModelName,
			BaseURL:   evalCtx.Endpoint,
			APIKey:    apiKey,
			IgnoreEOS: true,
		},
		API: infPerfAPI{
			Type:      apiType,
			Streaming: streaming,
		},
		Load: infPerfLoad{
			Type:     "concurrent",
			BaseSeed: ip.baseSeed,
		},
		Report: infPerfReport{
			RequestLifecycle: infPerfRequestLifecycle{
				Summary:     true,
				PerStage:    true,
				Percentiles: []int{50, 90, 95, 99},
			},
		},
	}

	if ip.tokenizerSource != "" {
		cfg.Tokenizer = &infPerfTokenizer{
			PretrainedModelNameOrPath: ip.tokenizerSource,
		}
	}

	if scenario.Concurrency <= 0 {
		return nil, fmt.Errorf("scenario.concurrency must be positive, got %d", scenario.Concurrency)
	}
	cfg.Load.Stages = append(cfg.Load.Stages, infPerfConcurrentStage{
		NumRequests:      numRequests,
		ConcurrencyLevel: scenario.Concurrency,
	})

	// Translate workload to data config
	if err := ip.buildDataConfig(cfg, scenario, numRequests); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (ip *InferencePerf) buildDataConfig(cfg *infPerfConfig, scenario config.ScenarioSpec, numRequests int) error {
	if scenario.Workload == "" {
		return fmt.Errorf("workload is required")
	}

	wl, err := config.ParseWorkload(scenario.Workload)
	if err != nil {
		return fmt.Errorf("parsing workload %q: %w", scenario.Workload, err)
	}

	totalCount := numRequests

	switch wl.Type {
	case config.WorkloadFixed:
		cfg.Data = infPerfData{
			Type:               "random",
			InputDistribution:  fixedDistribution(wl.InputTokens, totalCount),
			OutputDistribution: fixedDistribution(wl.OutputTokens, totalCount),
		}
	case config.WorkloadNormal:
		cfg.Data = infPerfData{
			Type:               "random",
			InputDistribution:  normalDistribution(wl.InputMean, wl.InputStdDev, totalCount),
			OutputDistribution: normalDistribution(wl.OutputMean, wl.OutputStdDev, totalCount),
		}
	case config.WorkloadUniform:
		cfg.Data = infPerfData{
			Type:               "random",
			InputDistribution:  uniformDistribution(wl.InputMin, wl.InputMax, totalCount),
			OutputDistribution: uniformDistribution(wl.OutputMin, wl.OutputMax, totalCount),
		}
	case config.WorkloadDataset:
		if ip.datasetPath == "" {
			return fmt.Errorf("datasetPath is required in evaluator config when workload type is %q", wl.Type)
		}
		cfg.Data = infPerfData{Type: "shareGPT", Path: ip.datasetPath}
	default:
		return fmt.Errorf("unsupported workload type %q", wl.Type)
	}

	return nil
}

func fixedDistribution(value, totalCount int) *infPerfDistribution {
	mean := float64(value)
	zero := 0.0
	return &infPerfDistribution{
		Min:        &value,
		Max:        &value,
		Mean:       &mean,
		StdDev:     &zero,
		TotalCount: totalCount,
		Type:       "fixed",
	}
}

func normalDistribution(mean, stdDev, totalCount int) *infPerfDistribution {
	minVal := mean - 3*stdDev
	if minVal < 1 {
		minVal = 1
	}
	maxVal := mean + 3*stdDev
	meanF := float64(mean)
	stdDevF := float64(stdDev)
	return &infPerfDistribution{
		Type:       "normal",
		Min:        &minVal,
		Max:        &maxVal,
		Mean:       &meanF,
		StdDev:     &stdDevF,
		TotalCount: totalCount,
	}
}

func uniformDistribution(min, max, totalCount int) *infPerfDistribution {
	return &infPerfDistribution{
		Type:       "uniform",
		Min:        &min,
		Max:        &max,
		TotalCount: totalCount,
	}
}

// --- Result parsing ---

// infPerfStageResult mirrors the inference-perf stage/summary JSON output.
type infPerfStageResult struct {
	Successes struct {
		Count   int `json:"count"`
		Latency struct {
			TimeToFirstToken   infPerfPercentiles `json:"time_to_first_token"`
			TimePerOutputToken infPerfPercentiles `json:"time_per_output_token"`
		} `json:"latency"`
		Throughput struct {
			OutputTokensPerSec float64 `json:"output_tokens_per_sec"`
			InputTokensPerSec  float64 `json:"input_tokens_per_sec"`
			TotalTokensPerSec  float64 `json:"total_tokens_per_sec"`
			RequestsPerSec     float64 `json:"requests_per_sec"`
		} `json:"throughput"`
	} `json:"successes"`
	Failures struct {
		Count int `json:"count"`
	} `json:"failures"`
}

type infPerfPercentiles struct {
	P50 float64 `json:"p50"`
	P99 float64 `json:"p99"`
}

// findReportsDir locates the latest reports-* directory inside resultDir.
// inference-perf names directories with a timestamp suffix (e.g. reports-20260101-120000),
// so lexicographic sorting yields the most recent run.
func findReportsDir(resultDir string) (string, error) {
	entries, err := os.ReadDir(resultDir)
	if err != nil {
		return "", fmt.Errorf("reading result directory %q: %w", resultDir, err)
	}

	var candidates []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "reports-") {
			candidates = append(candidates, e.Name())
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no reports-* directory found in %q", resultDir)
	}

	sort.Strings(candidates)
	return filepath.Join(resultDir, candidates[len(candidates)-1]), nil
}

// readStageFiles reads all stage_N_lifecycle_metrics.json files, sorted by stage number.
func readStageFiles(reportsDir string) ([]infPerfStageResult, error) {
	pattern := filepath.Join(reportsDir, "stage_*_lifecycle_metrics.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return parseStageNumber(matches[i]) < parseStageNumber(matches[j])
	})

	results := make([]infPerfStageResult, 0, len(matches))
	for _, path := range matches {
		r, err := readInfPerfResultFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading stage file %q: %w", filepath.Base(path), err)
		}
		results = append(results, *r)
	}
	return results, nil
}

// parseStageNumber extracts the numeric index from a filename like "stage_2_lifecycle_metrics.json".
// Returns math.MaxInt on parse failure so unparseable names sort last.
func parseStageNumber(path string) int {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "stage_")
	idx := strings.Index(base, "_")
	if idx < 0 {
		return math.MaxInt
	}
	n, err := strconv.Atoi(base[:idx])
	if err != nil {
		return math.MaxInt
	}
	return n
}

func readInfPerfResultFile(path string) (*infPerfStageResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r infPerfStageResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", filepath.Base(path), err)
	}
	return &r, nil
}

// aggregateInfPerfResults merges metrics across multiple stage results.
// Same semantics as genai-bench: worst-case for latency/error, average for throughput.
func aggregateInfPerfResults(results []infPerfStageResult) *abtypes.Metrics {
	m := &abtypes.Metrics{}
	n := float64(len(results))

	var sumOutputTP, sumInputTP, sumTotalTP, sumRPS float64
	var maxErrorRate float64
	var maxTTFTP50, maxTTFTP99, maxTPOTP50, maxTPOTP99 float64
	var totalCompleted, totalErrors int

	for _, r := range results {
		total := r.Successes.Count + r.Failures.Count
		var errRate float64
		if total > 0 {
			errRate = float64(r.Failures.Count) / float64(total)
		}

		sumOutputTP += r.Successes.Throughput.OutputTokensPerSec
		sumInputTP += r.Successes.Throughput.InputTokensPerSec
		sumTotalTP += r.Successes.Throughput.TotalTokensPerSec
		sumRPS += r.Successes.Throughput.RequestsPerSec
		maxErrorRate = math.Max(maxErrorRate, errRate)
		totalCompleted += r.Successes.Count
		totalErrors += r.Failures.Count

		// Latency: seconds -> milliseconds, then take worst-case
		ttftP50 := r.Successes.Latency.TimeToFirstToken.P50 * 1000
		ttftP99 := r.Successes.Latency.TimeToFirstToken.P99 * 1000
		tpotP50 := r.Successes.Latency.TimePerOutputToken.P50 * 1000
		tpotP99 := r.Successes.Latency.TimePerOutputToken.P99 * 1000
		maxTTFTP50 = math.Max(maxTTFTP50, ttftP50)
		maxTTFTP99 = math.Max(maxTTFTP99, ttftP99)
		maxTPOTP50 = math.Max(maxTPOTP50, tpotP50)
		maxTPOTP99 = math.Max(maxTPOTP99, tpotP99)
	}

	m.OutputThroughput = sumOutputTP / n
	m.InputThroughput = sumInputTP / n
	m.TotalThroughput = sumTotalTP / n
	m.RequestsPerSecond = sumRPS / n
	m.ErrorRate = maxErrorRate
	m.NumCompletedRequests = totalCompleted
	m.NumErrorRequests = totalErrors
	m.NumRequests = totalCompleted + totalErrors
	m.TTFTP50 = maxTTFTP50
	m.TTFTP99 = maxTTFTP99
	m.TPOTP50 = maxTPOTP50
	m.TPOTP99 = maxTPOTP99

	return m
}

// mapInfPerfToMetrics converts a single inference-perf result to Metrics.
func mapInfPerfToMetrics(r *infPerfStageResult) *abtypes.Metrics {
	total := r.Successes.Count + r.Failures.Count
	var errRate float64
	if total > 0 {
		errRate = float64(r.Failures.Count) / float64(total)
	}

	return &abtypes.Metrics{
		TTFTP50:              r.Successes.Latency.TimeToFirstToken.P50 * 1000,
		TTFTP99:              r.Successes.Latency.TimeToFirstToken.P99 * 1000,
		TPOTP50:              r.Successes.Latency.TimePerOutputToken.P50 * 1000,
		TPOTP99:              r.Successes.Latency.TimePerOutputToken.P99 * 1000,
		OutputThroughput:     r.Successes.Throughput.OutputTokensPerSec,
		InputThroughput:      r.Successes.Throughput.InputTokensPerSec,
		TotalThroughput:      r.Successes.Throughput.TotalTokensPerSec,
		RequestsPerSecond:    r.Successes.Throughput.RequestsPerSec,
		ErrorRate:            errRate,
		NumCompletedRequests: r.Successes.Count,
		NumErrorRequests:     r.Failures.Count,
		NumRequests:          total,
	}
}
