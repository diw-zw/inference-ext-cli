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

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
)

func TestGenAIBench_Name(t *testing.T) {
	g := &GenAIBench{}
	assert.Equal(t, "genai-bench", g.Name())
}

func TestBuildGenAIBenchArgs(t *testing.T) {
	tests := []struct {
		name            string
		tokenizerSource string // for Init
		evalCtx         EvalContext
		expected        []string // args that must all be present
		absent          []string // args that must NOT be present
	}{
		{
			name: "basic args with workload translation",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "test-scenario",
					Workload:    "fixed(100,1000)",
					Concurrency: 4,
				},
			},
			expected: []string{
				"benchmark",
				"--api-backend", "sglang",
				"--api-base", "http://svc:8000",
				"--api-model-name", "my-model",
				"--task", "text-to-text",
				"--traffic-scenario", "D(100,1000)",
				"--num-concurrency", "4",
			},
			absent: []string{
				"--experiment-base-dir",
				"--experiment-folder-name",
				"--model-tokenizer",
			},
		},
		{
			name: "with output dir",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Concurrency: 1,
				},
				OutputDir: "/data/results/scenario/trial-0",
			},
			expected: []string{
				"--experiment-base-dir", "/data/results/scenario",
				"--experiment-folder-name", "trial-0",
			},
		},
		{
			name:            "with tokenizer from Init",
			tokenizerSource: "/models/tokenizer",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Concurrency: 1,
				},
			},
			expected: []string{
				"--model-tokenizer", "/models/tokenizer",
			},
		},
		{
			name: "duration and maxRequests",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Workload:    "fixed(100,1000)",
					Concurrency: 4,
					Duration:    "2m",
					MaxRequests: 1000,
				},
			},
			expected: []string{
				"--traffic-scenario", "D(100,1000)",
				"--num-concurrency", "4",
				"--max-time-per-run", "2",
				"--max-requests-per-run", "1000",
			},
		},
		{
			name: "uniform workload translation",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Workload:    "uniform(100,500/200,800)",
					Concurrency: 4,
				},
			},
			expected: []string{
				"--traffic-scenario", "U(100,500)/(200,800)",
			},
		},
		{
			name: "dataset workload translation",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Workload:    "dataset",
					Concurrency: 4,
				},
			},
			expected: []string{
				"--traffic-scenario", "dataset",
			},
		},
		{
			name: "duration 30s rounds up to 1 minute",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Concurrency: 4,
					Duration:    "30s",
				},
			},
			expected: []string{
				"--max-time-per-run", "1",
			},
		},
		{
			name: "backend from config",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Backend:   "vllm",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Concurrency: 4,
				},
			},
			expected: []string{
				"--api-backend", "vllm",
			},
			absent: []string{
				"sglang",
			},
		},
		{
			name: "default backend when empty",
			evalCtx: EvalContext{
				Endpoint:  "http://svc:8000",
				ModelName: "my-model",
				Scenario: config.ScenarioSpec{
					Name:        "s1",
					Concurrency: 4,
				},
			},
			expected: []string{
				"--api-backend", "sglang",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GenAIBench{}
			if tt.tokenizerSource != "" {
				require.NoError(t, g.Init(map[string]interface{}{
					"tokenizerSource": tt.tokenizerSource,
				}))
			}
			args := g.buildGenAIBenchArgs(tt.evalCtx)

			// Check all expected args are present
			for i := 0; i < len(tt.expected); i++ {
				assert.Contains(t, args, tt.expected[i],
					"expected arg %q not found", tt.expected[i])
			}

			// Check absent args are not present
			for _, a := range tt.absent {
				assert.NotContains(t, args, a,
					"arg %q should not be present", a)
			}
		})
	}
}

func TestGenAIBench_CollectResults(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]interface{} // filename -> content to marshal as JSON
		wantErr string
		check   func(t *testing.T, dir string)
	}{
		{
			name: "single result file",
			files: map[string]interface{}{
				"D100_100_text-to-text_num_concurrency_4_time_16s.json": benchmarkResult{
					AggregatedMetrics: aggregatedMetrics{
						MeanOutputThroughput:      1500.5,
						MeanInputThroughput:       800.3,
						MeanTotalTokensThroughput: 2300.8,
						RequestsPerSecond:         25.7,
						ErrorRate:                 0.01,
						NumCompletedRequests:      99,
						NumErrorRequests:          1,
						NumRequests:               100,
						Stats: map[string]stats{
							"ttft": {P50: 12.5, P90: 30.0, P99: 45.0},
							"tpot": {P50: 3.2, P90: 6.0, P99: 8.7},
						},
					},
				},
			},
			check: func(t *testing.T, dir string) {
				g := &GenAIBench{}
				m, err := g.CollectResults(dir)
				require.NoError(t, err)
				assert.InDelta(t, 1500.5, m.OutputThroughput, 0.01)
				assert.InDelta(t, 800.3, m.InputThroughput, 0.01)
				assert.InDelta(t, 2300.8, m.TotalThroughput, 0.01)
				assert.InDelta(t, 25.7, m.RequestsPerSecond, 0.01)
				assert.InDelta(t, 0.01, m.ErrorRate, 0.001)
				assert.Equal(t, 99, m.NumCompletedRequests)
				assert.Equal(t, 1, m.NumErrorRequests)
				assert.Equal(t, 100, m.NumRequests)
				assert.InDelta(t, 12.5, m.TTFTP50, 0.01)
				assert.InDelta(t, 30.0, m.TTFTP90, 0.01)
				assert.InDelta(t, 45.0, m.TTFTP99, 0.01)
				assert.InDelta(t, 3.2, m.TPOTP50, 0.01)
				assert.InDelta(t, 6.0, m.TPOTP90, 0.01)
				assert.InDelta(t, 8.7, m.TPOTP99, 0.01)
			},
		},
		{
			name: "multiple concurrency files aggregated",
			files: map[string]interface{}{
				"D100_100_text-to-text_num_concurrency_4_time_16s.json": benchmarkResult{
					AggregatedMetrics: aggregatedMetrics{
						MeanOutputThroughput: 1000.0,
						RequestsPerSecond:    20.0,
						ErrorRate:            0.01,
						NumCompletedRequests: 99,
						NumErrorRequests:     1,
						NumRequests:          100,
						Stats: map[string]stats{
							"ttft": {P50: 10.0, P90: 25.0, P99: 40.0},
							"tpot": {P50: 3.0, P90: 5.0, P99: 8.0},
						},
					},
				},
				"N480_240_300_150_text-to-text_num_concurrency_16_time_26s.json": benchmarkResult{
					AggregatedMetrics: aggregatedMetrics{
						MeanOutputThroughput: 2000.0,
						RequestsPerSecond:    30.0,
						ErrorRate:            0.05,
						NumCompletedRequests: 95,
						NumErrorRequests:     5,
						NumRequests:          100,
						Stats: map[string]stats{
							"ttft": {P50: 20.0, P90: 50.0, P99: 80.0},
							"tpot": {P50: 5.0, P90: 9.0, P99: 12.0},
						},
					},
				},
			},
			check: func(t *testing.T, dir string) {
				g := &GenAIBench{}
				m, err := g.CollectResults(dir)
				require.NoError(t, err)
				// Throughput averaged: (1000+2000)/2 = 1500
				assert.InDelta(t, 1500.0, m.OutputThroughput, 0.01)
				// RPS averaged: (20+30)/2 = 25
				assert.InDelta(t, 25.0, m.RequestsPerSecond, 0.01)
				// Error rate worst-case: max(0.01, 0.05) = 0.05
				assert.InDelta(t, 0.05, m.ErrorRate, 0.001)
				// Request counts summed
				assert.Equal(t, 194, m.NumCompletedRequests)
				assert.Equal(t, 6, m.NumErrorRequests)
				assert.Equal(t, 200, m.NumRequests)
				// Latency worst-case: max across files
				assert.InDelta(t, 20.0, m.TTFTP50, 0.01)
				assert.InDelta(t, 50.0, m.TTFTP90, 0.01)
				assert.InDelta(t, 80.0, m.TTFTP99, 0.01)
				assert.InDelta(t, 5.0, m.TPOTP50, 0.01)
				assert.InDelta(t, 9.0, m.TPOTP90, 0.01)
				assert.InDelta(t, 12.0, m.TPOTP99, 0.01)
			},
		},
		{
			name: "skips experiment_metadata.json",
			files: map[string]interface{}{
				"experiment_metadata.json": map[string]string{"version": "1.0"},
				"result_concurrency_4.json": benchmarkResult{
					AggregatedMetrics: aggregatedMetrics{
						MeanOutputThroughput: 500.0,
						RequestsPerSecond:    10.0,
						NumCompletedRequests: 50,
						NumRequests:          50,
					},
				},
			},
			check: func(t *testing.T, dir string) {
				g := &GenAIBench{}
				m, err := g.CollectResults(dir)
				require.NoError(t, err)
				assert.InDelta(t, 500.0, m.OutputThroughput, 0.01)
				assert.Equal(t, 50, m.NumRequests)
			},
		},
		{
			name: "missing stats map defaults to zero",
			files: map[string]interface{}{
				"result.json": benchmarkResult{
					AggregatedMetrics: aggregatedMetrics{
						MeanOutputThroughput: 100.0,
						RequestsPerSecond:    10.0,
						NumCompletedRequests: 10,
						NumRequests:          10,
						Stats:                nil,
					},
				},
			},
			check: func(t *testing.T, dir string) {
				g := &GenAIBench{}
				m, err := g.CollectResults(dir)
				require.NoError(t, err)
				assert.InDelta(t, 100.0, m.OutputThroughput, 0.01)
				assert.Equal(t, float64(0), m.TTFTP50)
				assert.Equal(t, float64(0), m.TPOTP99)
			},
		},
		{
			name:    "empty directory",
			files:   map[string]interface{}{},
			wantErr: "no result JSON files found",
		},
		{
			name:    "nonexistent directory",
			wantErr: "reading result directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nonexistent directory" {
				g := &GenAIBench{}
				_, err := g.CollectResults("/nonexistent/path/that/does/not/exist")
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			dir := t.TempDir()
			for name, content := range tt.files {
				data, err := json.Marshal(content)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0644))
			}

			if tt.wantErr != "" {
				g := &GenAIBench{}
				_, err := g.CollectResults(dir)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			tt.check(t, dir)
		})
	}
}

func TestFactoryRegistration(t *testing.T) {
	t.Run("genai-bench registered", func(t *testing.T) {
		e, err := Get("genai-bench")
		require.NoError(t, err)
		assert.Equal(t, "genai-bench", e.Name())
	})

	t.Run("unknown evaluator", func(t *testing.T) {
		_, err := Get("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown evaluator")
	})
}

func TestGenAIBench_Init(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		g := &GenAIBench{}
		err := g.Init(map[string]interface{}{
			"tokenizerSource": "/models/tokenizer",
		})
		require.NoError(t, err)
		assert.Equal(t, "/models/tokenizer", g.tokenizerSource)
	})

	t.Run("empty config", func(t *testing.T) {
		g := &GenAIBench{}
		err := g.Init(map[string]interface{}{})
		require.NoError(t, err)
		assert.Empty(t, g.tokenizerSource)
	})

	t.Run("nil config", func(t *testing.T) {
		g := &GenAIBench{}
		err := g.Init(nil)
		require.NoError(t, err)
	})

	t.Run("wrong type for tokenizerSource", func(t *testing.T) {
		g := &GenAIBench{}
		err := g.Init(map[string]interface{}{
			"tokenizerSource": 123,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})
}

func TestTranslateWorkload(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"fixed(100,1000)", "D(100,1000)"},
		{"normal(480,240/300,150)", "N(480,240)/(300,150)"},
		{"uniform(100,500/200,800)", "U(100,500)/(200,800)"},
		{"dataset", "dataset"},
		{"unknown-format", "unknown-format"}, // passthrough on parse error
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := translateWorkload(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
