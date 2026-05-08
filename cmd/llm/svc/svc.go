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
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"sigs.k8s.io/rbgs/cli/cmd/llm/svc/chat"
)

// NewSVCCmd creates the svc subcommand group for managing LLM inference services.
func NewSVCCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "svc",
		Short: "Manage LLM inference services",
		Long: `Commands for managing LLM inference services (RoleBasedGroups) created by the CLI.

This command group provides operations for the full lifecycle of an inference service:
  - run:           Deploy a model as an inference service
  - list:          List running inference services
  - model-configs: List available model configurations
  - delete:        Remove an inference service
  - chat:          Interact with a running service`,
	}

	cmd.AddCommand(newRunCmd(cf))
	cmd.AddCommand(newListCmd(cf))
	cmd.AddCommand(newModelConfigsCmd())
	cmd.AddCommand(newDeleteCmd(cf))
	cmd.AddCommand(chat.NewChatCmd(cf))

	return cmd
}
