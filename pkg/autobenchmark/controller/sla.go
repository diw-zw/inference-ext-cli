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
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

// EvaluateSLA checks whether metrics satisfy all SLA constraints.
// Returns (pass, score). Score is the value of the optimize metric when SLA passes,
// or 0 when SLA fails.
func EvaluateSLA(metrics *abtypes.Metrics, objectives config.ObjectivesSpec) (bool, float64) {
	if metrics == nil {
		return false, 0
	}

	sla := objectives.SLA

	// Check TTFT P99 constraint
	if sla.TTFTP99MaxMs != nil && metrics.TTFTP99 > *sla.TTFTP99MaxMs {
		return false, 0
	}

	// Check TPOT P99 constraint
	if sla.TPOTP99MaxMs != nil && metrics.TPOTP99 > *sla.TPOTP99MaxMs {
		return false, 0
	}

	// Check error rate constraint
	if sla.ErrorRateMax != nil && metrics.ErrorRate > *sla.ErrorRateMax {
		return false, 0
	}

	// All SLA checks passed — compute score from optimize metric
	score := getOptimizeMetric(metrics, objectives.Optimize)
	return true, score
}

// getOptimizeMetric extracts the metric value to maximize based on the optimize target.
func getOptimizeMetric(metrics *abtypes.Metrics, optimize string) float64 {
	switch optimize {
	case "outputThroughput":
		return metrics.OutputThroughput
	case "inputThroughput":
		return metrics.InputThroughput
	case "requestsPerSecond":
		return metrics.RequestsPerSecond
	default:
		return metrics.OutputThroughput // fallback
	}
}
