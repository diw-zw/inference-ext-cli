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

package engine

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/plugin/util"
)

const (
	ReadinessProbeInitialDelaySeconds = 10
	ReadinessProbePeriodSeconds       = 5
	ReadinessProbeFailureThreshold    = 360
)

// GenerateOptions contains all information needed to generate a pod template.
type GenerateOptions struct {
	Name             string
	ModelID          string
	ModelPath        string
	Image            string          // Override image (empty to use default)
	Args             []string        // Additional arguments
	Env              []corev1.EnvVar // Additional environment variables
	Resources        corev1.ResourceRequirements
	DistributedSize  int32  // Multi-node deployment size, <=1 means standalone
	ShmSize          string // Shared memory size (e.g., "8Gi", "16Gi"), empty means no shared memory
	Tolerations      []corev1.Toleration
	ImagePullSecrets []corev1.LocalObjectReference
	HostNetwork      bool
	NodeSelector     map[string]string
}

// Plugin defines the interface for inference engines.
type Plugin interface {
	Name() string

	// ConfigFields returns the config fields this plugin accepts.
	ConfigFields() []util.ConfigField

	// Init initializes the engine with credentials/config.
	Init(config map[string]interface{}) error

	// GeneratePattern generates a complete Pattern for the model engine.
	// The returned Pattern can be either:
	// - StandalonePattern for single-node deployment
	// - LeaderWorkerPattern for multi-node deployment (with LeaderTemplatePatch/WorkerTemplatePatch if needed)
	GeneratePattern(opts GenerateOptions) (*workloadsv1alpha2.Pattern, error)
}

// Factory is a constructor for an engine plugin.
type Factory func() Plugin

var registry = make(map[string]Factory)

// Register registers an engine plugin factory under the given type name.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// Get returns an initialized engine plugin instance for the given type and config.
func Get(pluginType string, config map[string]interface{}) (Plugin, error) {
	factory, ok := registry[pluginType]
	if !ok {
		return nil, fmt.Errorf("unknown engine type %q", pluginType)
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
		return fmt.Errorf("unknown engine type %q", pluginType)
	}
	return util.ValidateConfig(factory().ConfigFields(), config)
}

// RegisteredNames returns all registered engine plugin type names in alphabetical order.
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

// IsRegistered checks if a plugin type is registered.
func IsRegistered(pluginType string) bool {
	_, ok := registry[pluginType]
	return ok
}
