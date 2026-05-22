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
	"testing"

	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

func makeTrial(feasible bool) abtypes.TrialResult {
	if feasible {
		return abtypes.TrialResult{Constraints: []float64{0, 0}}
	}
	return abtypes.TrialResult{Constraints: []float64{0.5, 0}}
}

func makeErrorTrial() abtypes.TrialResult {
	return abtypes.TrialResult{Error: "RBG not ready: context deadline exceeded"}
}

func TestIsExecutionError(t *testing.T) {
	tests := []struct {
		name  string
		trial abtypes.TrialResult
		want  bool
	}{
		{"no error", makeTrial(true), false},
		{"SLA failure without error", makeTrial(false), false},
		{"execution error without metrics", makeErrorTrial(), true},
		{"error with metrics is not execution error", abtypes.TrialResult{
			Error:   "benchmark failed: some issue",
			Metrics: &abtypes.Metrics{OutputThroughput: 100},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsExecutionError(&tt.trial))
		})
	}
}

func TestIsSLAFeasible(t *testing.T) {
	tests := []struct {
		name  string
		trial abtypes.TrialResult
		want  bool
	}{
		{"feasible - all constraints satisfied", abtypes.TrialResult{Constraints: []float64{0, 0}}, true},
		{"feasible - negative constraints", abtypes.TrialResult{Constraints: []float64{-1, -0.5}}, true},
		{"feasible - empty constraints", abtypes.TrialResult{}, true},
		{"infeasible - constraint violated", abtypes.TrialResult{Constraints: []float64{0.5, 0}}, false},
		{"infeasible - error with passing constraints", abtypes.TrialResult{
			Error:       "benchmark failed: timeout",
			Constraints: []float64{0, 0},
		}, false},
		{"infeasible - error with no constraints", abtypes.TrialResult{
			Error: "RBG not ready",
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.trial.IsSLAFeasible())
		})
	}
}

func TestCheckEarlyTermination(t *testing.T) {
	var numThree = 3
	var numFive = 5

	tests := []struct {
		name           string
		trials         []abtypes.TrialResult
		spec           config.EarlyTerminationSpec
		wantTerminated bool
		wantReason     string
	}{
		{
			name:           "nil spec - no termination",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{},
			wantTerminated: false,
		},
		{
			name:           "empty trials - no termination",
			trials:         nil,
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3},
			wantTerminated: false,
		},
		{
			name:           "consecutive failures below threshold",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3},
			wantTerminated: false,
		},
		{
			name:           "consecutive failures at threshold",
			trials:         []abtypes.TrialResult{makeTrial(true), makeTrial(false), makeTrial(false), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3},
			wantTerminated: true,
			wantReason:     "consecutive SLA failures reached limit: 3/3",
		},
		{
			name:           "consecutive failures broken by pass",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false), makeTrial(true), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3},
			wantTerminated: false,
		},
		{
			name:           "failure rate below threshold",
			trials:         []abtypes.TrialResult{makeTrial(true), makeTrial(true), makeTrial(false), makeTrial(true)},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5},
			wantTerminated: false,
		},
		{
			name:           "failure rate exceeds threshold",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false), makeTrial(false), makeTrial(true)},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5},
			wantTerminated: true,
			wantReason:     "SLA failure rate exceeded limit: 0.75 >= 0.50 (3/4 trials failed)",
		},
		{
			name:           "failure rate at exact threshold - termination",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(true)},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5},
			wantTerminated: true,
			wantReason:     "SLA failure rate exceeded limit: 0.50 >= 0.50 (1/2 trials failed)",
		},
		{
			name:           "minTrials guards all checks - consecutive would trigger but not enough trials",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3, MinTrials: 5},
			wantTerminated: false,
		},
		{
			name:           "minTrials guards all checks - rate would trigger but not enough trials",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5, MinTrials: 5},
			wantTerminated: false,
		},
		{
			name: "minTrials reached - consecutive triggers",
			trials: []abtypes.TrialResult{
				makeTrial(true), makeTrial(true),
				makeTrial(false), makeTrial(false), makeTrial(false),
			},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3, MinTrials: 5},
			wantTerminated: true,
			wantReason:     "consecutive SLA failures reached limit: 3/3",
		},
		{
			name: "minTrials reached - rate triggers",
			trials: []abtypes.TrialResult{
				makeTrial(false), makeTrial(false), makeTrial(false), makeTrial(false), makeTrial(true),
			},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5, MinTrials: 5},
			wantTerminated: true,
			wantReason:     "SLA failure rate exceeded limit: 0.80 >= 0.50 (4/5 trials failed)",
		},
		{
			name:           "no minTrials set - single failure rate triggers immediately",
			trials:         []abtypes.TrialResult{makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5},
			wantTerminated: true,
			wantReason:     "SLA failure rate exceeded limit: 1.00 >= 0.50 (1/1 trials failed)",
		},
		{
			name:           "both conditions - consecutive triggers first",
			trials:         []abtypes.TrialResult{makeTrial(false), makeTrial(false), makeTrial(false)},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3, MaxSLAFailureRate: 0.5},
			wantTerminated: true,
			wantReason:     "consecutive SLA failures reached limit: 3/3",
		},
		{
			name: "both conditions - rate triggers when consecutive does not",
			trials: []abtypes.TrialResult{
				makeTrial(false), makeTrial(false), makeTrial(true),
				makeTrial(false), makeTrial(false),
			},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3, MaxSLAFailureRate: 0.5},
			wantTerminated: true,
			wantReason:     "SLA failure rate exceeded limit: 0.80 >= 0.50 (4/5 trials failed)",
		},
		// --- MaxConsecutiveErrors tests ---
		{
			name:           "consecutive errors below threshold",
			trials:         []abtypes.TrialResult{makeErrorTrial(), makeErrorTrial()},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveErrors: &numThree},
			wantTerminated: false,
		},
		{
			name:           "consecutive errors at threshold",
			trials:         []abtypes.TrialResult{makeTrial(true), makeErrorTrial(), makeErrorTrial(), makeErrorTrial()},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveErrors: &numThree},
			wantTerminated: true,
			wantReason:     "consecutive execution errors reached limit: 3/3",
		},
		{
			name:           "consecutive errors broken by success",
			trials:         []abtypes.TrialResult{makeErrorTrial(), makeErrorTrial(), makeTrial(true), makeErrorTrial()},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveErrors: &numThree},
			wantTerminated: false,
		},
		{
			name:           "consecutive errors broken by SLA failure (not an execution error)",
			trials:         []abtypes.TrialResult{makeErrorTrial(), makeErrorTrial(), makeTrial(false), makeErrorTrial()},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveErrors: &numThree},
			wantTerminated: false,
		},
		{
			name:           "consecutive errors NOT gated by minTrials",
			trials:         []abtypes.TrialResult{makeErrorTrial(), makeErrorTrial(), makeErrorTrial()},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveErrors: &numThree, MinTrials: 10},
			wantTerminated: true,
			wantReason:     "consecutive execution errors reached limit: 3/3",
		},
		{
			name: "error check triggers before SLA checks",
			trials: []abtypes.TrialResult{
				makeErrorTrial(), makeErrorTrial(), makeErrorTrial(),
			},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveErrors: &numThree, MaxConsecutiveSLAFailures: 3},
			wantTerminated: true,
			wantReason:     "consecutive execution errors reached limit: 3/3",
		},
		// --- Execution errors excluded from SLA checks ---
		{
			name: "execution errors do not count toward consecutive SLA failures",
			trials: []abtypes.TrialResult{
				makeTrial(true), makeErrorTrial(), makeErrorTrial(), makeErrorTrial(),
			},
			spec:           config.EarlyTerminationSpec{MaxConsecutiveSLAFailures: 3, MaxConsecutiveErrors: &numFive},
			wantTerminated: false,
		},
		{
			name: "execution errors excluded from SLA failure rate",
			trials: []abtypes.TrialResult{
				makeTrial(false), makeErrorTrial(), makeErrorTrial(), makeTrial(true), makeTrial(true),
			},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5, MaxConsecutiveErrors: &numFive},
			wantTerminated: false,
			wantReason:     "",
		},
		{
			name: "SLA failure rate computed over non-error trials only",
			trials: []abtypes.TrialResult{
				makeTrial(false), makeTrial(false), makeErrorTrial(), makeTrial(true),
			},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5, MaxConsecutiveErrors: &numFive},
			wantTerminated: true,
			wantReason:     "SLA failure rate exceeded limit: 0.67 >= 0.50 (2/3 trials failed)",
		},
		{
			name: "all trials are execution errors - SLA rate check skipped",
			trials: []abtypes.TrialResult{
				makeErrorTrial(), makeErrorTrial(),
			},
			spec:           config.EarlyTerminationSpec{MaxSLAFailureRate: 0.5, MaxConsecutiveErrors: &numFive},
			wantTerminated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckEarlyTermination(tt.trials, tt.spec)
			assert.Equal(t, tt.wantTerminated, result.Terminated)
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, result.Reason)
			}
		})
	}
}
