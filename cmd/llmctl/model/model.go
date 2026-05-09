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

package model

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// NewModelCmd creates the model subcommand group for managing LLM models in storage.
func NewModelCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage LLM models in storage",
		Long: `Commands for managing downloaded LLM models in configured storage.

This command group provides operations for model assets:
  - list: List models that have been downloaded to storage
  - pull: Download a model from a configured source to storage`,
	}

	cmd.AddCommand(newListCmd(cf))
	cmd.AddCommand(newPullCmd(cf))

	return cmd
}
