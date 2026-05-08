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
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"sigs.k8s.io/rbgs/cli/pkg/util"
)

func newLogsCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <experiment-name>",
		Short: "Stream logs from an auto-benchmark controller",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd.Context(), cf, args[0], follow)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")

	return cmd
}

func runLogs(ctx context.Context, cf *genericclioptions.ConfigFlags, expName string, follow bool) error {
	clientset, err := util.GetK8SClientSet(cf)
	if err != nil {
		return err
	}

	namespace := util.GetNamespace(cf)

	jobName := experimentToJobName(expName)

	// Find the controller pod for this job
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return fmt.Errorf("listing pods for job %q: %w", jobName, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for job %q", jobName)
	}

	podName := pods.Items[0].Name

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow: follow,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("streaming logs from pod %q: %w", podName, err)
	}
	defer func() { _ = stream.Close() }()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("reading logs: %w", err)
	}

	return nil
}
