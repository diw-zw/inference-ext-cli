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
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewAddStorageCmd(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)
	cmd := newAddStorageCmd(cf)

	assert.NotNil(t, cmd)
	assert.Equal(t, "add-storage NAME", cmd.Use)
	assert.Equal(t, "Add a storage configuration", cmd.Short)
	assert.NotNil(t, cmd.Args)

	// Check flags
	flag := cmd.Flags().Lookup("type")
	assert.NotNil(t, flag)
	assert.Equal(t, "pvc", flag.DefValue)
	assert.Equal(t, "Storage type (pvc)", flag.Usage)

	configFlag := cmd.Flags().Lookup("config")
	assert.NotNil(t, configFlag)
	assert.Equal(t, "Storage configuration key=value pairs", configFlag.Usage)
}

func TestNewGetStoragesCmd(t *testing.T) {
	cmd := newGetStoragesCmd()

	assert.NotNil(t, cmd)
	assert.Equal(t, "get-storages [NAME]", cmd.Use)
	assert.Equal(t, "List all storage configurations or show details of one", cmd.Short)
}

func TestNewUseStorageCmd(t *testing.T) {
	cmd := newUseStorageCmd()

	assert.NotNil(t, cmd)
	assert.Equal(t, "use-storage NAME", cmd.Use)
	assert.Equal(t, "Set the current storage", cmd.Short)
	assert.NotNil(t, cmd.Args)
}

func TestNewSetStorageCmd(t *testing.T) {
	cmd := newSetStorageCmd()

	assert.NotNil(t, cmd)
	assert.Equal(t, "set-storage NAME", cmd.Use)
	assert.Equal(t, "Update a storage configuration", cmd.Short)
	assert.NotNil(t, cmd.Args)

	// Check config flag
	configFlag := cmd.Flags().Lookup("config")
	assert.NotNil(t, configFlag)
	assert.Equal(t, "Storage configuration key=value pairs", configFlag.Usage)
}

func TestNewDeleteStorageCmd(t *testing.T) {
	cmd := newDeleteStorageCmd()

	assert.NotNil(t, cmd)
	assert.Equal(t, "delete-storage NAME", cmd.Use)
	assert.Equal(t, "Delete a storage configuration", cmd.Short)
	assert.NotNil(t, cmd.Args)
}

func TestStorageCommands_ReturnCobraCommand(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)

	commands := []func() *cobra.Command{
		newGetStoragesCmd,
		newUseStorageCmd,
		newSetStorageCmd,
		newDeleteStorageCmd,
	}

	for _, fn := range commands {
		cmd := fn()
		assert.IsType(t, &cobra.Command{}, cmd)
	}

	// Test newAddStorageCmd separately since it requires ConfigFlags
	cmd := newAddStorageCmd(cf)
	assert.IsType(t, &cobra.Command{}, cmd)
}
