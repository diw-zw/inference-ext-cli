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

package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/evaluator"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

type mockEvaluator struct {
	metricsByLevel map[int]*abtypes.Metrics
	runErr         map[int]error
	collectErr     map[int]error
	runCalls       []int
}

func (m *mockEvaluator) Name() string { return "mock" }

func (m *mockEvaluator) Init(_ map[string]interface{}) error { return nil }

func (m *mockEvaluator) Run(_ context.Context, evalCtx evaluator.EvalContext) error {
	level := evalCtx.Scenario.Concurrency
	m.runCalls = append(m.runCalls, level)
	if err, ok := m.runErr[level]; ok {
		return err
	}
	return nil
}

func (m *mockEvaluator) CollectResults(_ string) (*abtypes.Metrics, error) {
	if len(m.runCalls) == 0 {
		return nil, fmt.Errorf("no run calls recorded")
	}
	level := m.runCalls[len(m.runCalls)-1]
	if err, ok := m.collectErr[level]; ok {
		return nil, err
	}
	if metrics, ok := m.metricsByLevel[level]; ok {
		cp := *metrics
		return &cp, nil
	}
	return &abtypes.Metrics{}, nil
}

func testCtx() context.Context {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
	return log.IntoContext(context.Background(), log.Log)
}

func TestRunConcurrencySweep_AllLevelsPass(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2:  {TTFTP99: 100, OutputThroughput: 500},
			4:  {TTFTP99: 150, OutputThroughput: 900},
			8:  {TTFTP99: 200, OutputThroughput: 1500},
			16: {TTFTP99: 300, OutputThroughput: 2000},
		},
	}

	ttftLimit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA:      config.SLASpec{TTFTP99MaxMs: &ttftLimit},
				Optimize: "outputThroughput",
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8, 16},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 16, concurrency)
	assert.InDelta(t, 2000, metrics.OutputThroughput, 0.01)
	assert.Equal(t, []int{2, 4, 8, 16}, mock.runCalls)
}

func TestRunConcurrencySweep_EarlyStop(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2:  {TTFTP99: 100, OutputThroughput: 500},
			4:  {TTFTP99: 200, OutputThroughput: 900},
			8:  {TTFTP99: 600, OutputThroughput: 1200},
			16: {TTFTP99: 1000, OutputThroughput: 1500},
		},
	}

	ttftLimit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TTFTP99MaxMs: &ttftLimit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8, 16},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 4, concurrency)
	assert.InDelta(t, 900, metrics.OutputThroughput, 0.01)
	assert.Equal(t, []int{2, 4, 8}, mock.runCalls)
}

func TestRunConcurrencySweep_FirstLevelFails(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			1: {TTFTP99: 800, OutputThroughput: 100},
		},
	}

	ttftLimit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TTFTP99MaxMs: &ttftLimit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{1, 2, 4},
			MaxRequests:      100,
		},
	)

	assert.Contains(t, errMsg, "no concurrency level passed")
	assert.Empty(t, warning)
	assert.Nil(t, metrics)
	assert.Equal(t, 0, concurrency)
	assert.Equal(t, []int{1}, mock.runCalls)
}

func TestRunConcurrencySweep_RunError(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2: {TTFTP99: 100, OutputThroughput: 500},
		},
		runErr: map[int]error{
			4: fmt.Errorf("connection refused"),
		},
	}

	ttftLimit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TTFTP99MaxMs: &ttftLimit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8},
			MaxRequests:      100,
		},
	)

	// When a lower level already passed, a run error at a higher level is treated
	// as a capacity limit — no errMsg, but a warning is set to distinguish from SLA violation.
	assert.Empty(t, errMsg)
	assert.Contains(t, warning, "benchmark failed at concurrency 4")
	assert.Contains(t, warning, "connection refused")
	require.NotNil(t, metrics)
	assert.Equal(t, 2, concurrency)
	assert.InDelta(t, 500, metrics.OutputThroughput, 0.01)
}

func TestRunConcurrencySweep_UnsortedInput(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2:  {TTFTP99: 100, OutputThroughput: 500},
			8:  {TTFTP99: 200, OutputThroughput: 1500},
			16: {TTFTP99: 300, OutputThroughput: 2000},
		},
	}

	ttftLimit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TTFTP99MaxMs: &ttftLimit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{16, 2, 8},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 16, concurrency)
	assert.Equal(t, []int{2, 8, 16}, mock.runCalls)
}

func TestRunConcurrencySweep_ContextCanceled(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2: {TTFTP99: 100, OutputThroughput: 500},
		},
	}

	ttftLimit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TTFTP99MaxMs: &ttftLimit},
			},
		},
		eval: mock,
	}

	ctx, cancel := context.WithCancel(testCtx())
	cancel()

	metrics, _, errMsg, warning := ctrl.runConcurrencySweep(
		ctx, "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8},
			MaxRequests:      100,
		},
	)

	assert.Contains(t, errMsg, "sweep cancelled before any level completed")
	assert.Empty(t, warning)
	assert.Nil(t, metrics)
	assert.Empty(t, mock.runCalls)
}

func TestRunConcurrencySweep_P90EarlyStop(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2: {TTFTP90: 50, TTFTP99: 100, OutputThroughput: 500},
			4: {TTFTP90: 200, TTFTP99: 300, OutputThroughput: 900},
			8: {TTFTP90: 600, TTFTP99: 800, OutputThroughput: 1200},
		},
	}

	ttftP90Limit := 500.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TTFTP90MaxMs: &ttftP90Limit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 4, concurrency) // stopped at 8 because P90 exceeded
	assert.Equal(t, []int{2, 4, 8}, mock.runCalls)
}

func TestRunConcurrencySweep_TPOTEarlyStop(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2: {TTFTP99: 100, TPOTP99: 5, OutputThroughput: 500},
			4: {TTFTP99: 200, TPOTP99: 12, OutputThroughput: 900},
			8: {TTFTP99: 300, TPOTP99: 20, OutputThroughput: 1200},
		},
	}

	tpotLimit := 15.0
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{TPOTP99MaxMs: &tpotLimit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 4, concurrency) // stopped at 8 because TPOT exceeded
	assert.Equal(t, []int{2, 4, 8}, mock.runCalls)
}

func TestRunConcurrencySweep_ErrorRateEarlyStop(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2: {TTFTP99: 100, ErrorRate: 0.001, OutputThroughput: 500},
			4: {TTFTP99: 200, ErrorRate: 0.005, OutputThroughput: 900},
			8: {TTFTP99: 300, ErrorRate: 0.02, OutputThroughput: 1200},
		},
	}

	errLimit := 0.01
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{ErrorRateMax: &errLimit},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 4, concurrency) // stopped at 8 because error rate exceeded
	assert.Equal(t, []int{2, 4, 8}, mock.runCalls)
}

func TestRunConcurrencySweep_MultiSLAConstraints(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2:  {TTFTP90: 50, TTFTP99: 100, TPOTP90: 3, TPOTP99: 5, ErrorRate: 0.001, OutputThroughput: 500},
			4:  {TTFTP90: 100, TTFTP99: 200, TPOTP90: 6, TPOTP99: 10, ErrorRate: 0.005, OutputThroughput: 900},
			8:  {TTFTP90: 200, TTFTP99: 400, TPOTP90: 8, TPOTP99: 18, ErrorRate: 0.008, OutputThroughput: 1200},
			16: {TTFTP90: 400, TTFTP99: 800, TPOTP90: 12, TPOTP99: 25, ErrorRate: 0.02, OutputThroughput: 1500},
		},
	}

	ttftP90Limit := 300.0
	ttftP99Limit := 500.0
	tpotP90Limit := 10.0
	tpotP99Limit := 20.0
	errLimit := 0.01
	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{
					TTFTP90MaxMs: &ttftP90Limit,
					TTFTP99MaxMs: &ttftP99Limit,
					TPOTP90MaxMs: &tpotP90Limit,
					TPOTP99MaxMs: &tpotP99Limit,
					ErrorRateMax: &errLimit,
				},
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8, 16},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 8, concurrency) // stopped at 16 because multiple SLA violated
	assert.Equal(t, []int{2, 4, 8, 16}, mock.runCalls)
}

func TestRunConcurrencySweep_NoSLAConstraints(t *testing.T) {
	mock := &mockEvaluator{
		metricsByLevel: map[int]*abtypes.Metrics{
			2:  {TTFTP99: 1000, TPOTP99: 50, ErrorRate: 0.1, OutputThroughput: 500},
			4:  {TTFTP99: 2000, TPOTP99: 100, ErrorRate: 0.2, OutputThroughput: 900},
			8:  {TTFTP99: 5000, TPOTP99: 200, ErrorRate: 0.3, OutputThroughput: 1500},
			16: {TTFTP99: 10000, TPOTP99: 500, ErrorRate: 0.5, OutputThroughput: 2000},
		},
	}

	ctrl := &Controller{
		cfg: &config.AutoBenchmarkConfig{
			Objectives: config.ObjectivesSpec{
				SLA: config.SLASpec{}, // no SLA constraints
			},
		},
		eval: mock,
	}

	metrics, concurrency, errMsg, warning := ctrl.runConcurrencySweep(
		testCtx(), "http://svc:8000", "model", t.TempDir(),
		config.ScenarioSpec{
			Workload:         "fixed(100,200)",
			ConcurrencySweep: []int{2, 4, 8, 16},
			MaxRequests:      100,
		},
	)

	assert.Empty(t, errMsg)
	assert.Empty(t, warning)
	require.NotNil(t, metrics)
	assert.Equal(t, 16, concurrency) // all levels pass because no SLA constraints
	assert.Equal(t, []int{2, 4, 8, 16}, mock.runCalls)
}
