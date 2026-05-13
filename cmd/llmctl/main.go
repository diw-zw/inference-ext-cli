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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/autobenchmark"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/benchmark"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/config"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/generate"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/model"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/svc"

	// Import plugins to register them
	_ "sigs.k8s.io/rbgs/cli/pkg/plugin/engine"
	_ "sigs.k8s.io/rbgs/cli/pkg/plugin/source"
	_ "sigs.k8s.io/rbgs/cli/pkg/plugin/storage"
)

var (
	cf *genericclioptions.ConfigFlags

	// Version information, set via ldflags at build time.
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:               "llmctl [command]",
	Short:             "Reference extension for RoleBasedGroup",
	Long:              `Commands for managing LLM model deployments on Kubernetes using RoleBasedGroup`,
	SilenceUsage:      true,
	DisableAutoGenTag: true,
	Args:              cobra.MaximumNArgs(1),
	Version:           getVersion(),
}

func getVersion() string {
	return fmt.Sprintf(
		"RBG CLI Version: %s, git commit: %s, build date: %s",
		Version, GitCommit, BuildDate,
	)
}

func main() {
	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		if f.Name != "v" {
			pflag.Lookup(f.Name).Hidden = true
		}
	})

	cf = genericclioptions.NewConfigFlags(true)
	cf.AddFlags(rootCmd.PersistentFlags())

	// Add subcommands
	rootCmd.AddCommand(svc.NewSVCCmd(cf))
	rootCmd.AddCommand(model.NewModelCmd(cf))
	rootCmd.AddCommand(config.NewConfigCmd(cf))
	rootCmd.AddCommand(generate.NewGenerateCmd())
	rootCmd.AddCommand(benchmark.NewBenchmarkCmd(cf))
	rootCmd.AddCommand(autobenchmark.NewAutoBenchmarkCmd(cf))
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(getVersion())
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
