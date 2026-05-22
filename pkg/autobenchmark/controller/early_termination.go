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
	"fmt"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

// EarlyTerminationResult holds the outcome of an early termination check.
type EarlyTerminationResult struct {
	Terminated bool
	Reason     string
}

// IsExecutionError returns true when the trial failed due to an execution error
// (build, create, startup, or runtime failure) rather than an SLA violation.
func IsExecutionError(t *abtypes.TrialResult) bool {
	return t.Error != "" && t.Metrics == nil
}

// CheckEarlyTermination evaluates whether the trial loop should be stopped
// based on the configured early termination conditions.
func CheckEarlyTermination(trials []abtypes.TrialResult, spec config.EarlyTerminationSpec) EarlyTerminationResult {
	if len(trials) == 0 {
		return EarlyTerminationResult{}
	}

	// Check consecutive execution errors first — NOT gated by MinTrials because
	// consecutive errors indicate broken templates or cluster issues, not
	// suboptimal parameter choices.
	if spec.MaxConsecutiveErrors != nil && *spec.MaxConsecutiveErrors > 0 {
		consecutive := 0
		for i := len(trials) - 1; i >= 0; i-- {
			if IsExecutionError(&trials[i]) {
				consecutive++
			} else {
				break
			}
		}
		if consecutive >= *spec.MaxConsecutiveErrors {
			return EarlyTerminationResult{
				Terminated: true,
				Reason: fmt.Sprintf("consecutive execution errors reached limit: %d/%d",
					consecutive, *spec.MaxConsecutiveErrors),
			}
		}
	}

	// SLA-based checks are gated by MinTrials.
	if spec.MinTrials > 0 && len(trials) < spec.MinTrials {
		return EarlyTerminationResult{}
	}

	// Check consecutive SLA failures (from the tail of the trial list).
	// Execution errors are excluded — they are handled by MaxConsecutiveErrors.
	if spec.MaxConsecutiveSLAFailures > 0 {
		consecutive := 0
		for i := len(trials) - 1; i >= 0; i-- {
			if IsExecutionError(&trials[i]) {
				break
			}
			if !trials[i].IsSLAFeasible() {
				consecutive++
			} else {
				break
			}
		}
		if consecutive >= spec.MaxConsecutiveSLAFailures {
			return EarlyTerminationResult{
				Terminated: true,
				Reason: fmt.Sprintf("consecutive SLA failures reached limit: %d/%d",
					consecutive, spec.MaxConsecutiveSLAFailures),
			}
		}
	}

	// Check overall SLA failure rate (execution errors excluded).
	if spec.MaxSLAFailureRate > 0 {
		failures := 0
		evaluated := 0
		for i := range trials {
			if IsExecutionError(&trials[i]) {
				continue
			}
			evaluated++
			if !trials[i].IsSLAFeasible() {
				failures++
			}
		}
		if evaluated == 0 {
			return EarlyTerminationResult{}
		}
		rate := float64(failures) / float64(evaluated)
		if rate > spec.MaxSLAFailureRate {
			return EarlyTerminationResult{
				Terminated: true,
				Reason: fmt.Sprintf("SLA failure rate exceeded limit: %.2f > %.2f (%d/%d trials failed)",
					rate, spec.MaxSLAFailureRate, failures, evaluated),
			}
		}
	}

	return EarlyTerminationResult{}
}
