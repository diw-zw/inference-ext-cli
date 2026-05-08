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
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	runpkg "sigs.k8s.io/rbgs/cli/cmd/llm/svc/run"
)

func newModelConfigsCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "model-configs",
		Short: "List available model configurations",
		Long: `List built-in and user-defined model configurations with their available run modes.

Use this to discover which model IDs and modes are supported before running
'kubectl rbg llm svc run'.

Use -o wide to show additional columns (source, engine).

Examples:
  # List all available model configurations
  kubectl rbg llm svc model-configs

  # Show full details
  kubectl rbg llm svc model-configs -o wide
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "" && output != "wide" {
				return fmt.Errorf("unsupported output format %q, supported values: wide", output)
			}

			models, err := runpkg.LoadAllModels()
			if err != nil {
				return fmt.Errorf("failed to load model configurations: %w", err)
			}

			if len(models) == 0 {
				fmt.Println("No model configurations found.")
				return nil
			}

			for i := range models {
				sort.Slice(models[i].Modes, func(a, b int) bool {
					return models[i].Modes[a].Name < models[i].Modes[b].Name
				})
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			write := func(format string, a ...any) error {
				_, err := fmt.Fprintf(w, format, a...)
				return err
			}

			if output == "wide" {
				if err := write("MODEL ID\tMODE\tENGINE\tSOURCE\tDESCRIPTION\n"); err != nil {
					return err
				}
				for _, m := range models {
					for _, mode := range m.Modes {
						if err := write("%s\t%s\t%s\t%s\t%s\n",
							m.ID, mode.Name, mode.Engine, mode.Source, mode.Description); err != nil {
							return err
						}
					}
				}
			} else {
				if err := write("MODEL ID\tMODE\tDESCRIPTION\n"); err != nil {
					return err
				}
				for _, m := range models {
					for _, mode := range m.Modes {
						if err := write("%s\t%s\t%s\n",
							m.ID, mode.Name, mode.Description); err != nil {
							return err
						}
					}
				}
			}

			return w.Flush()
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format. Supported: wide")

	return cmd
}
