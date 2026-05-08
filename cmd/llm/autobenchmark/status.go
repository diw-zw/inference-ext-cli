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
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"sigs.k8s.io/rbgs/cli/pkg/util"
)

func newStatusCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <experiment-name>",
		Short: "Show status of an auto-benchmark experiment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), cf, args[0])
		},
	}

	return cmd
}

func runStatus(ctx context.Context, cf *genericclioptions.ConfigFlags, expName string) error {
	clientset, err := util.GetK8SClientSet(cf)
	if err != nil {
		return err
	}

	namespace := util.GetNamespace(cf)

	jobName := experimentToJobName(expName)
	job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting job %q: %w", jobName, err)
	}

	// Print basic job info
	fmt.Printf("Name:      %s\n", job.Name)
	fmt.Printf("Namespace: %s\n", job.Namespace)
	fmt.Printf("Created:   %s\n", job.CreationTimestamp.Format("2006-01-02 15:04:05"))

	// Print status
	status := "Running"
	for _, c := range job.Status.Conditions {
		if c.Type == "Complete" && c.Status == "True" {
			status = "Completed"
		} else if c.Type == "Failed" && c.Status == "True" {
			status = fmt.Sprintf("Failed: %s", c.Message)
		}
	}
	fmt.Printf("Status:    %s\n", status)

	return nil
}
