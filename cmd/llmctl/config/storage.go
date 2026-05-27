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
	"bufio"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/rbgs/cli/pkg/config"
	storageplugin "sigs.k8s.io/rbgs/cli/pkg/plugin/storage"
	"sigs.k8s.io/rbgs/cli/pkg/util"
)

func newAddStorageCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	var storageType string
	var configFlags map[string]string
	var interactive bool

	cmd := &cobra.Command{
		Use:   "add-storage NAME",
		Short: "Add a storage configuration",
		Long: `Add a new storage configuration for model storage.

Storage defines where models are stored and accessed by inference engines.
Currently supported storage types:
  - pvc: Kubernetes PersistentVolumeClaim
  - oss: Alibaba Cloud Object Storage Service

PVC configuration fields:
  - pvcName: name of the pre-existing PersistentVolumeClaim to bind to (required)

OSS configuration fields:
  - url: OSS endpoint URL (e.g., oss-cn-hangzhou.aliyuncs.com) (required)
  - bucket: OSS bucket name (required)
  - subpath: subpath within the bucket (optional)
  - akId: Alibaba Cloud AccessKey ID (required)
  - akSecret: Alibaba Cloud AccessKey Secret (required)

Examples:
  # Add a PVC storage with command-line flags
  llmctl config add-storage my-pvc --type pvc --config pvcName=model-pvc

  # Add an OSS storage with command-line flags
  llmctl config add-storage my-oss --type oss --config url=oss-cn-hangzhou.aliyuncs.com --config bucket=my-bucket --config akId=MY_ACCESS_KEY_ID --config akSecret=MY_ACCESS_KEY_SECRET

  # Add storage interactively
  llmctl config add-storage my-pvc -i`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'add-storage' requires exactly 1 argument\n\nUsage:\n  llmctl config add-storage NAME [-i]\n\nSee 'llmctl config add-storage -h' for examples")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			var configMap map[string]interface{}

			if interactive {
				// Interactive mode
				reader := bufio.NewReader(os.Stdin)
				storageType, configMap, err = configureStorage(reader)
				if err != nil {
					return err
				}
			} else {
				// Command-line mode
				configMap = make(map[string]interface{})
				for k, v := range configFlags {
					configMap[k] = v
				}
			}

			if err := storageplugin.ValidateConfig(storageType, configMap); err != nil {
				return err
			}

			// Call PreAdd to perform any preparatory work (e.g., creating Kubernetes Secrets)
			// This also returns a modified config with secretRef instead of raw credentials
			ns := util.GetNamespace(cf)
			ctrlClient, err := util.GetControllerRuntimeClient(cf)
			if err != nil {
				return fmt.Errorf("failed to create controller client: %w", err)
			}

			preAddOpts := storageplugin.PreAddOptions{
				Client:      ctrlClient,
				StorageName: name,
				Namespace:   ns,
				Config:      configMap,
			}

			modifiedConfig, err := storageplugin.PreAdd(storageType, preAddOpts)
			if err != nil {
				return fmt.Errorf("failed to prepare storage: %w", err)
			}

			if err := cfg.AddStorage(name, storageType, modifiedConfig); err != nil {
				return err
			}

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Storage '%s' added successfully\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&storageType, "type", "pvc", "Storage type (pvc)")
	configFlags = make(map[string]string)
	cmd.Flags().StringToStringVar(&configFlags, "config", nil, "Storage configuration key=value pairs")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Interactive configuration mode")

	return cmd
}

func newGetStoragesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-storages [NAME]",
		Short: "List all storage configurations or show details of one",
		Long: `List all configured storage backends, or show detailed information for a specific one.

Without NAME: displays a table showing all storages.
With NAME: displays the detailed configuration for the named storage.

Examples:
  # List all storages
  llmctl config get-storages

  # Show details of a specific storage
  llmctl config get-storages my-pvc`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Show details of a single storage
			if len(args) == 1 {
				name := args[0]
				s, err := cfg.GetStorage(name)
				if err != nil {
					return err
				}
				if s.Name == cfg.CurrentStorage {
					fmt.Printf("Storage: %s (active)\n", s.Name)
				} else {
					fmt.Printf("Storage: %s\n", s.Name)
				}
				fmt.Printf("  Type: %s\n", s.Type)
				printConfigItems(s.Config, storageplugin.GetFields(s.Type))
				return nil
			}

			// List all storages
			if len(cfg.Storages) == 0 {
				fmt.Println("No storage found in rbg config")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tTYPE\tCURRENT")
			for _, s := range cfg.Storages {
				current := ""
				if s.Name == cfg.CurrentStorage {
					current = "*"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Type, current)
			}
			return w.Flush()
		},
	}
}

func newUseStorageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use-storage NAME",
		Short: "Set the current storage",
		Long: `Set the specified storage as the current active storage.

The active storage is used by default when deploying models.

Example:
  llmctl config use-storage my-pvc`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'use-storage' requires exactly 1 argument\n\nUsage:\n  llmctl config use-storage NAME\n\nSee 'llmctl config use-storage -h' for examples")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if err := cfg.UseStorage(name); err != nil {
				return err
			}

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Now using storage '%s'\n", name)
			return nil
		},
	}
}

func newSetStorageCmd() *cobra.Command {
	var configFlags map[string]string

	cmd := &cobra.Command{
		Use:   "set-storage NAME",
		Short: "Update a storage configuration",
		Long: `Update an existing storage configuration.

Modify the configuration parameters of a previously added storage.

Example:
  llmctl config set-storage my-pvc --config pvcName=new-model-pvc`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'set-storage' requires exactly 1 argument\n\nUsage:\n  llmctl config set-storage NAME [--config key=value]\n\nSee 'llmctl config set-storage -h' for examples")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			configMap := make(map[string]interface{})
			for k, v := range configFlags {
				configMap[k] = v
			}

			if err := cfg.UpdateStorage(name, configMap); err != nil {
				return err
			}

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Storage '%s' updated successfully\n", name)
			return nil
		},
	}

	configFlags = make(map[string]string)
	cmd.Flags().StringToStringVar(&configFlags, "config", nil, "Storage configuration key=value pairs")

	return cmd
}

func newDeleteStorageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete-storage NAME",
		Short: "Delete a storage configuration",
		Long: `Delete a storage configuration from the config.

Note: Cannot delete the currently active storage. Switch to another storage first.

Example:
  llmctl config delete-storage my-pvc`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'delete-storage' requires exactly 1 argument\n\nUsage:\n  llmctl config delete-storage NAME\n\nSee 'llmctl config delete-storage -h' for examples")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if err := cfg.DeleteStorage(name); err != nil {
				return err
			}

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Storage '%s' deleted successfully\n", name)
			return nil
		},
	}
}
