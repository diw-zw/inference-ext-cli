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

package autobenchmark

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

const (
	controllerImage = "rolebasedgroup/rbgs-auto-benchmark:latest"
)

// NewAutoBenchmarkCmd creates the "llm auto-benchmark" command.
func NewAutoBenchmarkCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auto-benchmark",
		Aliases: []string{"ab"},
		Short:   "Automated LLM inference parameter tuning",
		Long: `Automated parameter tuning for LLM inference engines.

Iterates through RBG template configurations and engine parameter search spaces
to find the optimal configuration that maximizes throughput while meeting SLA constraints.`,
	}

	cmd.AddCommand(newRunCmd(cf))
	cmd.AddCommand(newDashboardCmd(cf))
	cmd.AddCommand(newStatusCmd(cf))
	cmd.AddCommand(newListCmd(cf))
	cmd.AddCommand(newStopCmd(cf))
	cmd.AddCommand(newLogsCmd(cf))

	return cmd
}

// experimentToJobName converts an experiment name to the conventional Job name.
// Uses sanitizeResourceName to ensure DNS-1123 compliance.
func experimentToJobName(expName string) string {
	safeName := sanitizeResourceName(expName)
	jobName := fmt.Sprintf("ab-%s", safeName)
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	return strings.TrimRight(jobName, "-")
}
