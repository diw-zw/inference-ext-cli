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

package run

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	cliconfig "sigs.k8s.io/rbgs/cli/pkg/config"
	"sigs.k8s.io/yaml"
)

// ModelConfig describes a model and its available run modes.
type ModelConfig struct {
	ID    string       `yaml:"id"`
	Name  string       `yaml:"name"`
	Modes []ModeConfig `yaml:"modes"`
}

// ModeConfig describes a single run mode for a model.
type ModeConfig struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Engine      string              `yaml:"engine"`
	Image       string              `yaml:"image"`
	Resources   corev1.ResourceList `yaml:"resources"`
	Args        []string            `yaml:"args"`
	Env         []corev1.EnvVar     `yaml:"env"`
	Distributed *DistributedConfig  `yaml:"distributed,omitempty"` // Multi-node deployment config
	ShmSize     string              `yaml:"shmSize,omitempty"`     // Shared memory size (e.g., "8Gi", "16Gi")

	// Source indicates where this mode was defined ("builtin" or config filename).
	// Populated by LoadAllModels; excluded from YAML/JSON serialization.
	Source string `yaml:"-" json:"-"`
}

// DistributedConfig describes multi-node deployment configuration.
// Size > 1 enables leader-worker pattern for distributed inference.
type DistributedConfig struct {
	Size int32 `yaml:"size"` // Total nodes (1 leader + N workers)
}

// modelWithSource tracks a model config and its source file
type modelWithSource struct {
	model  ModelConfig
	source string
}

// LoadAllModels loads all available model configurations and merges them at the
// mode level: user modes override builtin modes with the same name, while
// builtin modes with no user counterpart are preserved.
//
// Sources:
//  1. User-defined models (from GetModelConfigDir(), default: ~/.rbg/models,
//     overridable via RBG_MODEL_CONFIG env var; accepts .yaml and .yml files)
//  2. Built-in models (embedded models.yaml)
func LoadAllModels() ([]ModelConfig, error) {
	userModels, err := loadUserModels()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load user models: %v\n", err)
	}

	builtinModels, err := loadBuiltinModels()
	if err != nil {
		return nil, fmt.Errorf("failed to load builtin models: %w", err)
	}

	return mergeModelConfigs(userModels, builtinModels), nil
}

// mergeModelConfigs merges user and builtin model configs at the mode level.
// Builtin models form the base; user modes override same-named modes,
// and user-only modes are appended. User-only models are added as-is.
// Within a single user model definition, duplicate modes are deduplicated
// with last-one-wins semantics.
func mergeModelConfigs(userModels []modelWithSource, builtinModels []ModelConfig) []ModelConfig {
	modelMap := make(map[string]*ModelConfig, len(builtinModels))
	for i := range builtinModels {
		bm := &builtinModels[i]
		modelMap[bm.ID] = &ModelConfig{ID: bm.ID, Name: bm.Name, Modes: copyModes(bm.Modes, "builtin")}
	}

	for _, um := range userModels {
		existing, exists := modelMap[um.model.ID]
		if !exists {
			existing = &ModelConfig{ID: um.model.ID, Name: um.model.Name}
			modelMap[um.model.ID] = existing
		}
		if um.model.Name != "" {
			existing.Name = um.model.Name
		}
		for _, umode := range um.model.Modes {
			umodeCopy := copyMode(umode, um.source)
			found := false
			for i := range existing.Modes {
				if existing.Modes[i].Name == umodeCopy.Name {
					fmt.Fprintf(os.Stderr, "Warning: mode %q for model %q redefined by %s, overriding %s\n",
						umodeCopy.Name, um.model.ID, um.source, existing.Modes[i].Source)
					existing.Modes[i] = umodeCopy
					found = true
					break
				}
			}
			if !found {
				existing.Modes = append(existing.Modes, umodeCopy)
			}
		}
	}

	result := make([]ModelConfig, 0, len(modelMap))
	for _, mc := range modelMap {
		result = append(result, *mc)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// copyMode returns a deep copy of a single ModeConfig with Source set.
func copyMode(mode ModeConfig, source string) ModeConfig {
	mode.Source = source
	if mode.Args != nil {
		mode.Args = append([]string(nil), mode.Args...)
	}
	if mode.Env != nil {
		mode.Env = append([]corev1.EnvVar(nil), mode.Env...)
	}
	if mode.Resources != nil {
		mode.Resources = mode.Resources.DeepCopy()
	}
	if mode.Distributed != nil {
		d := *mode.Distributed
		mode.Distributed = &d
	}
	return mode
}

// copyModes returns a deep copy of the given modes, setting each mode's Source.
func copyModes(modes []ModeConfig, source string) []ModeConfig {
	out := make([]ModeConfig, len(modes))
	for i, m := range modes {
		out[i] = copyMode(m, source)
	}
	return out
}

// loadBuiltinModels loads the embedded model configurations
func loadBuiltinModels() ([]ModelConfig, error) {
	var configs []ModelConfig
	if err := yaml.Unmarshal(embeddedModelsYAML, &configs); err != nil {
		return nil, fmt.Errorf("failed to parse builtin model configs: %w", err)
	}
	return configs, nil
}

// loadUserModels loads user-defined models from the models/ directory
// Returns models with their source file information for conflict detection
func loadUserModels() ([]modelWithSource, error) {
	modelsDir := cliconfig.GetModelConfigDir()
	if modelsDir == "" {
		return nil, nil
	}

	envModelsDir, envModelsDirSet := os.LookupEnv("RBG_MODEL_CONFIG")
	// Check if directory exists and is actually a directory
	info, err := os.Stat(modelsDir)
	if os.IsNotExist(err) {
		if envModelsDirSet && envModelsDir != "" && envModelsDir == modelsDir {
			fmt.Fprintf(os.Stderr, "Warning: model config directory %q from RBG_MODEL_CONFIG does not exist, skipping\n", modelsDir)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat models directory: %w", err)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Warning: models path %q is not a directory, skipping\n", modelsDir)
		return nil, nil
	}

	// Read all YAML files in the directory
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read models directory: %w", err)
	}

	// Sort entries by filename for deterministic order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var allModels []modelWithSource
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process .yaml and .yml files (case-insensitive)
		name := entry.Name()
		lowerName := strings.ToLower(name)
		if !strings.HasSuffix(lowerName, ".yaml") && !strings.HasSuffix(lowerName, ".yml") {
			continue
		}

		filePath := filepath.Join(modelsDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read model file %s: %v\n", filePath, err)
			continue
		}

		var models []ModelConfig
		if err := yaml.Unmarshal(data, &models); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse model file %s: %v\n", filePath, err)
			continue
		}

		// Track source file for each model
		for _, m := range models {
			allModels = append(allModels, modelWithSource{
				model:  m,
				source: name, // Use filename for user-friendly messages
			})
		}
	}

	return allModels, nil
}

// FindModelConfig finds the best matching ModelConfig for modelID using:
//  1. Exact match
//  2. Wildcard match (e.g. "Qwen/*")
//  3. Default config ("*")
func FindModelConfig(models []ModelConfig, modelID string) (*ModelConfig, error) {
	var wildcardMatch *ModelConfig
	var defaultMatch *ModelConfig

	for i := range models {
		mc := &models[i]
		if mc.ID == modelID {
			return mc, nil
		}
		if mc.ID == "*" {
			defaultMatch = mc
			continue
		}
		if matched, _ := path.Match(mc.ID, modelID); matched {
			if wildcardMatch == nil {
				wildcardMatch = mc
			}
		}
	}

	if wildcardMatch != nil {
		return wildcardMatch, nil
	}
	if defaultMatch != nil {
		return defaultMatch, nil
	}

	return nil, fmt.Errorf("no configuration found for model %q", modelID)
}

// FindModeConfig finds a named mode within a ModelConfig.
// If mode is empty, the first mode in the list is used.
func FindModeConfig(mc *ModelConfig, mode string) (*ModeConfig, error) {
	if len(mc.Modes) == 0 {
		return nil, fmt.Errorf("no modes defined for model %q", mc.ID)
	}
	if mode == "" {
		return &mc.Modes[0], nil
	}

	modeNames := make([]string, 0, len(mc.Modes))
	for i := range mc.Modes {
		m := &mc.Modes[i]
		if m.Name == mode {
			return m, nil
		}
		modeNames = append(modeNames, m.Name)
	}

	return nil, fmt.Errorf("mode %q not found for model %q, available modes: %v", mode, mc.ID, modeNames)
}
