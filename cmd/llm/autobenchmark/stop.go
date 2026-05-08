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
	"sigs.k8s.io/controller-runtime/pkg/client"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/constant"
	"sigs.k8s.io/rbgs/cli/pkg/util"
)

func newStopCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <experiment-name>",
		Short: "Stop an auto-benchmark experiment",
		Long:  `Deletes the controller Job and cleans up any active trial RBGs.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(cmd.Context(), cf, args[0])
		},
	}

	return cmd
}

func runStop(ctx context.Context, cf *genericclioptions.ConfigFlags, expName string) error {
	clientset, err := util.GetK8SClientSet(cf)
	if err != nil {
		return err
	}

	namespace := util.GetNamespace(cf)
	labelSelector := fmt.Sprintf("%s=%s", constant.AutoBenchmarkLabelKey, expName)

	// Delete controller Job(s) by experiment label
	jobList, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("listing jobs: %w", err)
	}
	propagation := metav1.DeletePropagationForeground
	for _, job := range jobList.Items {
		if err := clientset.BatchV1().Jobs(namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); err != nil {
			fmt.Printf("Warning: failed to delete Job %q: %v\n", job.Name, err)
		} else {
			fmt.Printf("Deleted Job %q\n", job.Name)
		}
	}
	if len(jobList.Items) == 0 {
		// Fallback: try the conventional name directly.
		jobName := experimentToJobName(expName)
		if err := clientset.BatchV1().Jobs(namespace).Delete(ctx, jobName, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); err != nil {
			fmt.Printf("Warning: Job %q not found: %v\n", jobName, err)
		} else {
			fmt.Printf("Deleted Job %q\n", jobName)
		}
	}

	// Clean up ConfigMap(s) by experiment label
	cmList, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		fmt.Printf("Warning: could not list ConfigMaps: %v\n", err)
	} else {
		for _, cm := range cmList.Items {
			if err := clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Printf("Warning: failed to delete ConfigMap %q: %v\n", cm.Name, err)
			} else {
				fmt.Printf("Deleted ConfigMap %q\n", cm.Name)
			}
		}
	}

	// Clean up trial RBGs by label
	rbgClient, err := util.GetControllerRuntimeClient(cf)
	if err != nil {
		fmt.Printf("Warning: could not get RBG client for cleanup: %v\n", err)
		return nil
	}

	rbgList := &workloadsv1alpha2.RoleBasedGroupList{}
	if err := rbgClient.List(ctx, rbgList, client.InNamespace(namespace), client.MatchingLabels{constant.AutoBenchmarkLabelKey: expName}); err != nil {
		fmt.Printf("Warning: could not list trial RBGs: %v\n", err)
		return nil
	}

	for i := range rbgList.Items {
		if err := rbgClient.Delete(ctx, &rbgList.Items[i]); err != nil {
			fmt.Printf("Warning: failed to delete trial RBG %q: %v\n", rbgList.Items[i].Name, err)
		} else {
			fmt.Printf("Deleted trial RBG %q\n", rbgList.Items[i].Name)
		}
	}

	return nil
}
