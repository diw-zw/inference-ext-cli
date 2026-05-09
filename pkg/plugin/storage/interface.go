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

package storage

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/rbgs/cli/pkg/plugin/util"
)

// DefaultMountPath is the default path where model storage is mounted in containers.
const DefaultMountPath = "/models"

// MountOptions contains options passed to MountStorage, including both
// mount configuration and pre-mount resource provisioning parameters.
type MountOptions struct {
	// Client is the controller-runtime client used to create/verify Kubernetes resources
	// (e.g., Secret, PV, PVC). May be nil for storage backends that don't need it (e.g., PVC).
	Client client.Client
	// StorageName is the name from the storage configuration, used for naming resources.
	StorageName string
	// Namespace is the target namespace for the resources.
	Namespace string
	// DryRun skips Kubernetes resource provisioning (Secret, PV, PVC creation) while
	// still adding volumes and mounts to the pod template for preview purposes.
	DryRun bool
	// MountPath is the path where storage is mounted in the container.
	// Typically set to DefaultMountPath ("/models"). The caller is responsible for
	// specifying this value to ensure consistent mount behavior across plugins.
	MountPath string
}

// PreAddOptions contains options passed to PreAdd, used for preparing
// resources before adding a storage configuration.
type PreAddOptions struct {
	// Client is the controller-runtime client used to create Kubernetes resources
	// (e.g., Secret). Required for storage backends that need to store sensitive data.
	Client client.Client
	// StorageName is the name from the storage configuration, used for naming resources.
	StorageName string
	// Namespace is the target namespace for the resources.
	Namespace string
	// Config is the original storage configuration map.
	Config map[string]interface{}
}

// ModelInfo contains information about a downloaded model.
type ModelInfo struct {
	// ModelID is the original model identifier (e.g., "organization/model-name")
	ModelID string `json:"modelID"`
	// Revision is the model revision (e.g., "main", "v1.0")
	Revision string `json:"revision"`
	// DownloadedAt is the timestamp when the model was downloaded (optional)
	DownloadedAt string `json:"downloadedAt,omitempty"`
}

// Plugin defines the interface for storage backends.
type Plugin interface {
	Name() string

	// ConfigFields returns the config fields this plugin accepts.
	ConfigFields() []util.ConfigField

	// Init initializes storage with config.
	Init(config map[string]interface{}) error

	// MountStorage provisions any required Kubernetes resources (e.g., Secret, PV, PVC)
	// and modifies the PodTemplateSpec to add volumes and mounts.
	// The volume is mounted at the path specified by opts.MountPath.
	MountStorage(podTemplate *corev1.PodTemplateSpec, opts MountOptions) error

	// PreAdd is called before adding a new storage configuration.
	// It allows plugins to perform preparatory work such as creating Kubernetes Secrets
	// for sensitive data. The plugin should create necessary resources and return a
	// modified config that replaces sensitive values with references (e.g., secretName/secretNamespace).
	//
	// For example, an OSS plugin might:
	// 1. Create a Secret containing akId and akSecret
	// 2. Return a config with secretName and secretNamespace instead of the raw credentials
	//
	// If no preparation is needed, the plugin should return the original config.
	PreAdd(opts PreAddOptions) (config map[string]interface{}, err error)
}

// Factory is a constructor for a storage plugin.
type Factory func() Plugin

var registry = make(map[string]Factory)

// Register registers a storage plugin factory under the given type name.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// Get returns an initialized storage plugin instance for the given type and config.
func Get(pluginType string, config map[string]interface{}) (Plugin, error) {
	factory, ok := registry[pluginType]
	if !ok {
		return nil, fmt.Errorf("unknown storage type %q", pluginType)
	}
	p := factory()
	if err := p.Init(config); err != nil {
		return nil, err
	}
	return p, nil
}

// ValidateConfig validates the provided config against the declared fields of the named plugin.
func ValidateConfig(pluginType string, config map[string]interface{}) error {
	factory, ok := registry[pluginType]
	if !ok {
		return fmt.Errorf("unknown storage type %q. . Supported types: %v", pluginType, RegisteredNames())
	}
	return util.ValidateConfig(factory().ConfigFields(), config)
}

// RegisteredNames returns all registered storage plugin type names in alphabetical order.
func RegisteredNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetFields returns the config fields for a plugin type without initializing it.
func GetFields(pluginType string) []util.ConfigField {
	factory, ok := registry[pluginType]
	if !ok {
		return nil
	}
	return factory().ConfigFields()
}

// PreAdd calls the PreAdd method for the specified plugin type with the given options.
// This should be called before adding a new storage configuration to perform any
// necessary preparatory work (e.g., creating Kubernetes Secrets for sensitive data).
// Returns the modified config (with secretName/secretNamespace instead of raw credentials) and any error.
func PreAdd(pluginType string, opts PreAddOptions) (map[string]interface{}, error) {
	factory, ok := registry[pluginType]
	if !ok {
		return nil, fmt.Errorf("unknown storage type %q", pluginType)
	}
	p := factory()
	return p.PreAdd(opts)
}
