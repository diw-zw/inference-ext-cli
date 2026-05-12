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

package config

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/rbgs/cli/pkg/config"
	engineplugin "sigs.k8s.io/rbgs/cli/pkg/plugin/engine"
)

func newSetEngineCmd() *cobra.Command {
	var configFlags map[string]string

	cmd := &cobra.Command{
		Use:   "set-engine ENGINE_TYPE",
		Short: "Customize engine configuration (optional — engines work with defaults without this)",
		Long: `Customize an inference engine configuration.

This command is optional - engines work with sensible defaults without explicit configuration.
Use this command only when you need to customize engine-specific parameters.

Currently supported engine types:
  - sglang: SGLang inference engine
  - vllm: vLLM inference engine

Example:
  kubectl rbg llm config set-engine sglang --config defaultPort=8000`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'set-engine' requires exactly 1 argument\n\nUsage:\n  kubectl rbg llm config set-engine ENGINE_TYPE [--config key=value]\n\nSee 'kubectl rbg llm config set-engine -h' for examples")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			engineType := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if !engineplugin.IsRegistered(engineType) {
				return fmt.Errorf("unknown engine type '%s'. Supported types: %v", engineType, engineplugin.RegisteredNames())
			}

			configMap := make(map[string]interface{})
			for k, v := range configFlags {
				configMap[k] = v
			}

			if err := engineplugin.ValidateConfig(engineType, configMap); err != nil {
				return err
			}

			cfg.SetEngine(engineType, configMap)

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Engine '%s' configured successfully\n", engineType)
			return nil
		},
	}

	configFlags = make(map[string]string)
	cmd.Flags().StringToStringVar(&configFlags, "config", nil, "Engine configuration key=value pairs")

	return cmd
}

func newGetEnginesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-engines [ENGINE_TYPE]",
		Short: "List customized engine configurations or show details of one",
		Long: `List all engines with custom configurations, or show detailed information for a specific one.

Without ENGINE_TYPE: displays a table showing all customized engines.
With ENGINE_TYPE: displays the detailed configuration for the named engine.
If the engine has no custom configuration, it indicates that defaults are being used.

Examples:
  # List all customized engines
  kubectl rbg llm config get-engines

  # Show details of a specific engine
  kubectl rbg llm config get-engines sglang`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Show details of a single engine
			if len(args) == 1 {
				engineType := args[0]
				if !engineplugin.IsRegistered(engineType) {
					return fmt.Errorf("unknown engine type '%s'. Supported types: %v", engineType, engineplugin.RegisteredNames())
				}
				fmt.Printf("Engine: %s\n", engineType)
				e, err := cfg.GetEngine(engineType)
				if err == nil && e != nil {
					fmt.Println("  Configuration (customized):")
					printConfigItems(e.Config, engineplugin.GetFields(engineType))
				} else {
					fmt.Println("  Configuration: (using defaults)")
					fmt.Println("  No custom configuration found. Run 'kubectl rbg llm config set-engine' to customize.")
				}
				return nil
			}

			// List all customized engines
			if len(cfg.Engines) == 0 {
				fmt.Println("No engine configuration found in rbg config")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TYPE")
			for _, e := range cfg.Engines {
				_, _ = fmt.Fprintf(w, "%s\n", e.Type)
			}
			return w.Flush()
		},
	}
}

func newResetEngineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset-engine ENGINE_TYPE",
		Short: "Remove custom engine configuration, reverting to defaults",
		Long: `Remove custom configuration for an engine, reverting to default settings.

Example:
  kubectl rbg llm config reset-engine sglang`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'reset-engine' requires exactly 1 argument\n\nUsage:\n  kubectl rbg llm config reset-engine ENGINE_TYPE\n\nSee 'kubectl rbg llm config reset-engine -h' for examples")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			engineType := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if err := cfg.DeleteEngine(engineType); err != nil {
				return err
			}

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Engine '%s' reset to defaults\n", engineType)
			return nil
		},
	}
}
