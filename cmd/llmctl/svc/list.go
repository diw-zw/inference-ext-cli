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

package svc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	llmmeta "sigs.k8s.io/rbgs/cli/cmd/llmctl/svc/metadata"
	"sigs.k8s.io/rbgs/cli/pkg/util"
)

const unknownValue = "unknown"

// serviceInfo represents a single LLM service entry for display
type serviceInfo struct {
	Name      string
	Namespace string
	Model     string
	Engine    string
	Mode      string
	Revision  string
	Replicas  int32
	Status    string
}

func newListCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		allNamespaces bool
	)

	cmd := &cobra.Command{
		Use:   "list [flags]",
		Short: "List LLM inference services created by the CLI",
		Long: `List RoleBasedGroup resources created by 'llmctl svc run'.

This command displays all LLM inference services that were created using the CLI.
It shows information such as the service name, model, engine, mode, replicas, and status.

The command filters RoleBasedGroups by the CLI source label to only show resources
managed by the kubectl-rbg CLI tool.`,
		Example: `  # List services in current namespace
  llmctl svc list

  # List services in all namespaces
  llmctl svc list -A

  # List services in a specific namespace
  llmctl svc list -n kubeai

  # Filter by label selector
  llmctl svc list -l app.kubernetes.io/name=my-qwen`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rbgClient, err := util.GetControllerRuntimeClient(cf)
			if err != nil {
				return fmt.Errorf("unable to connect to Kubernetes cluster: %w", err)
			}

			var namespace string
			if allNamespaces {
				namespace = ""
			} else {
				namespace = util.GetNamespace(cf)
			}

			ctx := context.Background()
			rbgList := &workloadsv1alpha2.RoleBasedGroupList{}
			listOpts := []client.ListOption{
				client.MatchingLabels{llmmeta.RunCommandSourceLabelKey: llmmeta.RunCommandSourceLabelValue},
			}
			if namespace != "" {
				listOpts = append(listOpts, client.InNamespace(namespace))
			}
			if err := rbgClient.List(ctx, rbgList, listOpts...); err != nil {
				return fmt.Errorf("failed to list RoleBasedGroups: %w", err)
			}

			services := extractServiceInfos(rbgList)
			listPrintTable(cmd, services, allNamespaces)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List services across all namespaces")

	return cmd
}

// extractServiceInfos extracts serviceInfo from a list of RoleBasedGroups
func extractServiceInfos(rbgList *workloadsv1alpha2.RoleBasedGroupList) []serviceInfo {
	services := make([]serviceInfo, 0, len(rbgList.Items))
	for i := range rbgList.Items {
		rbg := &rbgList.Items[i]
		info := serviceInfo{
			Name:      rbg.Name,
			Namespace: rbg.Namespace,
			Status:    extractRBGStatus(rbg),
		}

		if annotationVal, ok := rbg.Annotations[llmmeta.RunCommandMetadataAnnotationKey]; ok {
			var meta llmmeta.RunMetadata
			if err := json.Unmarshal([]byte(annotationVal), &meta); err == nil {
				info.Model = meta.ModelID
				info.Engine = meta.Engine
				info.Mode = meta.Mode
				info.Revision = meta.Revision
			} else {
				info.Model = unknownValue
				info.Engine = unknownValue
				info.Mode = unknownValue
				info.Revision = unknownValue
			}
		} else {
			info.Model = unknownValue
			info.Engine = unknownValue
			info.Mode = unknownValue
			info.Revision = unknownValue
		}

		if len(rbg.Spec.Roles) > 0 && rbg.Spec.Roles[0].Replicas != nil {
			info.Replicas = *rbg.Spec.Roles[0].Replicas
		}

		services = append(services, info)
	}
	return services
}

// extractRBGStatus returns the human-readable status from an RBG's conditions
func extractRBGStatus(rbg *workloadsv1alpha2.RoleBasedGroup) string {
	if len(rbg.Status.Conditions) == 0 {
		return "Pending"
	}
	for _, c := range rbg.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			return "Ready"
		}
	}
	return "NotReady"
}
