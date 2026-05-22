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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
)

func TestInferencePerf_Name(t *testing.T) {
	ip := &InferencePerf{}
	assert.Equal(t, "inference-perf", ip.Name())
}

func TestInferencePerf_Init(t *testing.T) {
	t.Run("valid config with all fields", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{
			"tokenizerSource": "/models/tokenizer",
			"apiKey":          "sk-test",
			"baseSeed":        42,
		})
		require.NoError(t, err)
		assert.Equal(t, "/models/tokenizer", ip.tokenizerSource)
		assert.Equal(t, "sk-test", ip.apiKey)
		require.NotNil(t, ip.baseSeed)
		assert.Equal(t, 42, *ip.baseSeed)
	})

	t.Run("baseSeed as float64 (YAML/JSON decode)", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{
			"baseSeed": float64(12345),
		})
		require.NoError(t, err)
		require.NotNil(t, ip.baseSeed)
		assert.Equal(t, 12345, *ip.baseSeed)
	})

	t.Run("valid config with apiType, streaming, datasetPath", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{
			"apiType":     "chat",
			"streaming":   false,
			"datasetPath": "/data/sharegpt.json",
		})
		require.NoError(t, err)
		assert.Equal(t, "chat", ip.apiType)
		require.NotNil(t, ip.streaming)
		assert.Equal(t, false, *ip.streaming)
		assert.Equal(t, "/data/sharegpt.json", ip.datasetPath)
	})

	t.Run("empty config", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{})
		require.NoError(t, err)
		assert.Empty(t, ip.tokenizerSource)
		assert.Empty(t, ip.apiKey)
		assert.Nil(t, ip.baseSeed)
	})

	t.Run("nil config", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(nil)
		require.NoError(t, err)
	})

	t.Run("wrong type for tokenizerSource", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{"tokenizerSource": 123})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("wrong type for apiKey", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{"apiKey": true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("wrong type for baseSeed", func(t *testing.T) {
		ip := &InferencePerf{}
		err := ip.Init(map[string]interface{}{"baseSeed": "not-a-number"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a number")
	})
}

func TestInferencePerf_BuildConfig(t *testing.T) {
	t.Run("fixed workload", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Backend:   "vllm",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,1000)",
				Concurrency: 64,
				MaxRequests: 200,
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "vllm", cfg.Server.Type)
		assert.Equal(t, "http://svc:8000", cfg.Server.BaseURL)
		assert.Equal(t, "my-model", cfg.Server.ModelName)
		assert.True(t, cfg.Server.IgnoreEOS)
		assert.Equal(t, "completion", cfg.API.Type)
		assert.True(t, cfg.API.Streaming)
		assert.Equal(t, "random", cfg.Data.Type)

		require.NotNil(t, cfg.Data.InputDistribution)
		assert.Equal(t, float64(100), *cfg.Data.InputDistribution.Mean)
		assert.Equal(t, float64(0), *cfg.Data.InputDistribution.StdDev)
		assert.Equal(t, 100, *cfg.Data.InputDistribution.Min)
		assert.Equal(t, 100, *cfg.Data.InputDistribution.Max)

		require.NotNil(t, cfg.Data.OutputDistribution)
		assert.Equal(t, float64(1000), *cfg.Data.OutputDistribution.Mean)
		assert.Equal(t, float64(0), *cfg.Data.OutputDistribution.StdDev)

		require.Len(t, cfg.Load.Stages, 1)
		assert.Equal(t, 200, cfg.Load.Stages[0].NumRequests)
		assert.Equal(t, 64, cfg.Load.Stages[0].ConcurrencyLevel)
	})

	t.Run("normal workload", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "normal(480,240/300,150)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "random", cfg.Data.Type)
		require.NotNil(t, cfg.Data.InputDistribution)
		assert.Equal(t, float64(480), *cfg.Data.InputDistribution.Mean)
		assert.Equal(t, float64(240), *cfg.Data.InputDistribution.StdDev)
		assert.Equal(t, 1, *cfg.Data.InputDistribution.Min)    // max(480-3*240, 1) = max(-240, 1) = 1
		assert.Equal(t, 1200, *cfg.Data.InputDistribution.Max) // 480+3*240 = 1200
		assert.Equal(t, "normal", cfg.Data.InputDistribution.Type)

		require.NotNil(t, cfg.Data.OutputDistribution)
		assert.Equal(t, float64(300), *cfg.Data.OutputDistribution.Mean)
		assert.Equal(t, float64(150), *cfg.Data.OutputDistribution.StdDev)
		assert.Equal(t, 1, *cfg.Data.OutputDistribution.Min)  // max(300-3*150, 1) = max(-150, 1) = 1
		assert.Equal(t, 750, *cfg.Data.OutputDistribution.Max) // 300+3*150 = 750

		// total_count = defaultNumRequests = 500
		assert.Equal(t, 500, cfg.Data.InputDistribution.TotalCount)
	})

	t.Run("uniform workload", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "uniform(100,500/200,800)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "random", cfg.Data.Type)
		require.NotNil(t, cfg.Data.InputDistribution)
		assert.Equal(t, "uniform", cfg.Data.InputDistribution.Type)
		assert.Equal(t, 100, *cfg.Data.InputDistribution.Min)
		assert.Equal(t, 500, *cfg.Data.InputDistribution.Max)
		assert.Nil(t, cfg.Data.InputDistribution.Mean)
		assert.Nil(t, cfg.Data.InputDistribution.StdDev)

		require.NotNil(t, cfg.Data.OutputDistribution)
		assert.Equal(t, "uniform", cfg.Data.OutputDistribution.Type)
		assert.Equal(t, 200, *cfg.Data.OutputDistribution.Min)
		assert.Equal(t, 800, *cfg.Data.OutputDistribution.Max)
	})

	t.Run("dataset workload", func(t *testing.T) {
		ip := &InferencePerf{datasetPath: "/data/sharegpt.json"}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "dataset",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "shareGPT", cfg.Data.Type)
		assert.Equal(t, "/data/sharegpt.json", cfg.Data.Path)
		assert.Nil(t, cfg.Data.InputDistribution)
		assert.Nil(t, cfg.Data.OutputDistribution)
	})

	t.Run("dataset workload without datasetPath returns error", func(t *testing.T) {
		ip := &InferencePerf{}
		_, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "dataset",
				Concurrency: 4,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "datasetPath is required")
	})

	t.Run("configurable api type and streaming", func(t *testing.T) {
		streaming := false
		ip := &InferencePerf{apiType: "chat", streaming: &streaming}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "chat", cfg.API.Type)
		assert.False(t, cfg.API.Streaming)
	})

	t.Run("single concurrency level", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 64,
				MaxRequests: 300,
			},
		})
		require.NoError(t, err)

		require.Len(t, cfg.Load.Stages, 1)
		assert.Equal(t, 64, cfg.Load.Stages[0].ConcurrencyLevel)
		assert.Equal(t, 300, cfg.Load.Stages[0].NumRequests)
	})

	t.Run("default backend and api key", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "sglang", cfg.Server.Type)
		assert.Equal(t, "EMPTY", cfg.Server.APIKey)
	})

	t.Run("with tokenizer and baseSeed", func(t *testing.T) {
		seed := 42
		ip := &InferencePerf{
			tokenizerSource: "Qwen/Qwen3-8B",
			baseSeed:        &seed,
		}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)

		require.NotNil(t, cfg.Tokenizer)
		assert.Equal(t, "Qwen/Qwen3-8B", cfg.Tokenizer.PretrainedModelNameOrPath)
		require.NotNil(t, cfg.Load.BaseSeed)
		assert.Equal(t, 42, *cfg.Load.BaseSeed)
	})

	t.Run("without tokenizer", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)
		assert.Nil(t, cfg.Tokenizer)
		assert.Nil(t, cfg.Load.BaseSeed)
	})

	t.Run("report config", func(t *testing.T) {
		ip := &InferencePerf{}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 4,
			},
		})
		require.NoError(t, err)
		assert.True(t, cfg.Report.RequestLifecycle.Summary)
		assert.True(t, cfg.Report.RequestLifecycle.PerStage)
		assert.Equal(t, []int{50, 90, 95, 99}, cfg.Report.RequestLifecycle.Percentiles)
	})

	t.Run("empty workloads returns error", func(t *testing.T) {
		ip := &InferencePerf{}
		_, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Concurrency: 4,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workload is required")
	})

	t.Run("zero concurrency returns error", func(t *testing.T) {
		ip := &InferencePerf{}
		_, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Scenario: config.ScenarioSpec{
				Workload:    "fixed(100,200)",
				Concurrency: 0,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "concurrency must be positive")
	})

	t.Run("config serializes to valid YAML", func(t *testing.T) {
		ip := &InferencePerf{tokenizerSource: "Qwen/Qwen3-8B"}
		cfg, err := ip.buildConfig(EvalContext{
			Endpoint:  "http://svc:8000",
			ModelName: "my-model",
			Backend:   "vllm",
			Scenario: config.ScenarioSpec{
				Workload:    "normal(512,256/2048,1024)",
				Concurrency: 64,
				MaxRequests: 500,
			},
		})
		require.NoError(t, err)

		data, err := yaml.Marshal(cfg)
		require.NoError(t, err)

		// Verify it roundtrips through YAML
		var roundtrip infPerfConfig
		require.NoError(t, yaml.Unmarshal(data, &roundtrip))
		assert.Equal(t, cfg.Server.Type, roundtrip.Server.Type)
		assert.Equal(t, cfg.Data.Type, roundtrip.Data.Type)
		assert.Len(t, roundtrip.Load.Stages, 1)
	})
}

func TestInferencePerf_CollectResults(t *testing.T) {
	t.Run("single stage file", func(t *testing.T) {
		dir := setupInfPerfResults(t, map[string]infPerfStageResult{
			"stage_0_lifecycle_metrics.json": {
				Successes: makeSuccesses(100, 0.050, 0.200, 0.005, 0.015, 1500, 800, 2300, 25),
				Failures:  makeFailures(2),
			},
		})

		ip := &InferencePerf{}
		m, err := ip.CollectResults(dir)
		require.NoError(t, err)

		// Latency: seconds -> milliseconds
		assert.InDelta(t, 50.0, m.TTFTP50, 0.01)
		assert.InDelta(t, 200.0, m.TTFTP99, 0.01)
		assert.InDelta(t, 5.0, m.TPOTP50, 0.01)
		assert.InDelta(t, 15.0, m.TPOTP99, 0.01)

		assert.InDelta(t, 1500.0, m.OutputThroughput, 0.01)
		assert.InDelta(t, 800.0, m.InputThroughput, 0.01)
		assert.InDelta(t, 2300.0, m.TotalThroughput, 0.01)
		assert.InDelta(t, 25.0, m.RequestsPerSecond, 0.01)

		assert.Equal(t, 100, m.NumCompletedRequests)
		assert.Equal(t, 2, m.NumErrorRequests)
		assert.Equal(t, 102, m.NumRequests)
		assert.InDelta(t, 2.0/102.0, m.ErrorRate, 0.001)
	})

	t.Run("multiple stage files aggregated", func(t *testing.T) {
		dir := setupInfPerfResults(t, map[string]infPerfStageResult{
			"stage_0_lifecycle_metrics.json": {
				Successes: makeSuccesses(200, 0.010, 0.050, 0.003, 0.008, 1000, 500, 1500, 20),
				Failures:  makeFailures(0),
			},
			"stage_1_lifecycle_metrics.json": {
				Successes: makeSuccesses(200, 0.020, 0.100, 0.005, 0.020, 2000, 1000, 3000, 40),
				Failures:  makeFailures(10),
			},
		})

		ip := &InferencePerf{}
		m, err := ip.CollectResults(dir)
		require.NoError(t, err)

		// Latency: worst-case across stages (in ms)
		assert.InDelta(t, 20.0, m.TTFTP50, 0.01)  // max(10, 20)
		assert.InDelta(t, 100.0, m.TTFTP99, 0.01)  // max(50, 100)
		assert.InDelta(t, 5.0, m.TPOTP50, 0.01)    // max(3, 5)
		assert.InDelta(t, 20.0, m.TPOTP99, 0.01)   // max(8, 20)

		// Throughput: averaged
		assert.InDelta(t, 1500.0, m.OutputThroughput, 0.01)  // (1000+2000)/2
		assert.InDelta(t, 750.0, m.InputThroughput, 0.01)    // (500+1000)/2
		assert.InDelta(t, 2250.0, m.TotalThroughput, 0.01)   // (1500+3000)/2
		assert.InDelta(t, 30.0, m.RequestsPerSecond, 0.01)   // (20+40)/2

		// Request counts: summed
		assert.Equal(t, 400, m.NumCompletedRequests)
		assert.Equal(t, 10, m.NumErrorRequests)
		assert.Equal(t, 410, m.NumRequests)

		// Error rate: worst-case
		assert.InDelta(t, 10.0/210.0, m.ErrorRate, 0.001) // max(0/200, 10/210)
	})

	t.Run("fallback to summary file", func(t *testing.T) {
		dir := t.TempDir()
		reportsDir := filepath.Join(dir, "reports-20260514-120000")
		require.NoError(t, os.MkdirAll(reportsDir, 0755))

		summary := infPerfStageResult{
			Successes: makeSuccesses(500, 0.025, 0.080, 0.004, 0.012, 1800, 900, 2700, 30),
			Failures:  makeFailures(5),
		}
		writeJSON(t, filepath.Join(reportsDir, "summary_lifecycle_metrics.json"), summary)

		ip := &InferencePerf{}
		m, err := ip.CollectResults(dir)
		require.NoError(t, err)

		assert.InDelta(t, 25.0, m.TTFTP50, 0.01)
		assert.InDelta(t, 80.0, m.TTFTP99, 0.01)
		assert.Equal(t, 500, m.NumCompletedRequests)
		assert.Equal(t, 5, m.NumErrorRequests)
	})

	t.Run("no reports directory", func(t *testing.T) {
		dir := t.TempDir()
		ip := &InferencePerf{}
		_, err := ip.CollectResults(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no reports-* directory")
	})

	t.Run("multiple reports directories picks latest", func(t *testing.T) {
		dir := t.TempDir()
		oldDir := filepath.Join(dir, "reports-20260101-100000")
		newDir := filepath.Join(dir, "reports-20260514-120000")
		require.NoError(t, os.MkdirAll(oldDir, 0755))
		require.NoError(t, os.MkdirAll(newDir, 0755))

		oldSummary := infPerfStageResult{
			Successes: makeSuccesses(100, 0.050, 0.150, 0.010, 0.030, 500, 250, 750, 10),
			Failures:  makeFailures(50),
		}
		newSummary := infPerfStageResult{
			Successes: makeSuccesses(500, 0.025, 0.080, 0.004, 0.012, 1800, 900, 2700, 30),
			Failures:  makeFailures(5),
		}
		writeJSON(t, filepath.Join(oldDir, "summary_lifecycle_metrics.json"), oldSummary)
		writeJSON(t, filepath.Join(newDir, "summary_lifecycle_metrics.json"), newSummary)

		ip := &InferencePerf{}
		m, err := ip.CollectResults(dir)
		require.NoError(t, err)

		// Should use the latest (newDir) results
		assert.InDelta(t, 25.0, m.TTFTP50, 0.01)
		assert.Equal(t, 500, m.NumCompletedRequests)
		assert.Equal(t, 5, m.NumErrorRequests)
	})

	t.Run("corrupted stage file returns error instead of fallback", func(t *testing.T) {
		dir := t.TempDir()
		reportsDir := filepath.Join(dir, "reports-20260514-120000")
		require.NoError(t, os.MkdirAll(reportsDir, 0755))

		require.NoError(t, os.WriteFile(
			filepath.Join(reportsDir, "stage_0_lifecycle_metrics.json"),
			[]byte("not valid json"),
			0644,
		))
		// Also provide a valid summary to verify we don't silently fall back to it
		summary := infPerfStageResult{
			Successes: makeSuccesses(500, 0.025, 0.080, 0.004, 0.012, 1800, 900, 2700, 30),
			Failures:  makeFailures(5),
		}
		writeJSON(t, filepath.Join(reportsDir, "summary_lifecycle_metrics.json"), summary)

		ip := &InferencePerf{}
		_, err := ip.CollectResults(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading stage files")
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		ip := &InferencePerf{}
		_, err := ip.CollectResults("/nonexistent/path/that/does/not/exist")
		require.Error(t, err)
	})

	t.Run("zero total requests handles division by zero", func(t *testing.T) {
		dir := setupInfPerfResults(t, map[string]infPerfStageResult{
			"stage_0_lifecycle_metrics.json": {
				Successes: makeSuccesses(0, 0, 0, 0, 0, 0, 0, 0, 0),
				Failures:  makeFailures(0),
			},
		})

		ip := &InferencePerf{}
		m, err := ip.CollectResults(dir)
		require.NoError(t, err)
		assert.Equal(t, float64(0), m.ErrorRate)
		assert.Equal(t, 0, m.NumRequests)
	})
}

func TestInferencePerfFactoryRegistration(t *testing.T) {
	t.Run("inference-perf registered", func(t *testing.T) {
		e, err := Get("inference-perf")
		require.NoError(t, err)
		assert.Equal(t, "inference-perf", e.Name())
	})
}

// --- Test helpers ---

func makeSuccesses(
	count int,
	ttftP50, ttftP99, tpotP50, tpotP99 float64,
	outputTP, inputTP, totalTP, rps float64,
) struct {
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
} {
	var s struct {
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
	}
	s.Count = count
	s.Latency.TimeToFirstToken = infPerfPercentiles{P50: ttftP50, P99: ttftP99}
	s.Latency.TimePerOutputToken = infPerfPercentiles{P50: tpotP50, P99: tpotP99}
	s.Throughput.OutputTokensPerSec = outputTP
	s.Throughput.InputTokensPerSec = inputTP
	s.Throughput.TotalTokensPerSec = totalTP
	s.Throughput.RequestsPerSec = rps
	return s
}

func makeFailures(count int) struct {
	Count int `json:"count"`
} {
	return struct {
		Count int `json:"count"`
	}{Count: count}
}

func setupInfPerfResults(t *testing.T, files map[string]infPerfStageResult) string {
	t.Helper()
	dir := t.TempDir()
	reportsDir := filepath.Join(dir, "reports-20260514-120000")
	require.NoError(t, os.MkdirAll(reportsDir, 0755))

	for name, result := range files {
		writeJSON(t, filepath.Join(reportsDir, name), result)
	}
	return dir
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
}
