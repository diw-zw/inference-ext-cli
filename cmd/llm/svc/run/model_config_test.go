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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserModelsFromDirectory(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create models directory: %v", err)
	}

	// Create a model YAML file
	modelFile := filepath.Join(modelsDir, "custom-models.yaml")
	modelContent := `- id: "test-org/test-model"
  name: "Test Model"
  modes:
    - name: standard
      description: "Test standard mode"
      engine: vllm
      image: vllm/vllm-openai:latest
      resources:
        nvidia.com/gpu: "1"
        cpu: "2"
        memory: 8Gi
      args:
        - "--tensor-parallel-size=1"
    - name: throughput
      description: "Test throughput mode"
      engine: sglang
      image: lmsys/sglang:latest
      resources:
        nvidia.com/gpu: "2"
        cpu: "4"
        memory: 16Gi
      args:
        - "--mem-fraction-static=0.9"
`

	if err := os.WriteFile(modelFile, []byte(modelContent), 0600); err != nil {
		t.Fatalf("Failed to write model file: %v", err)
	}

	// Set the model config path to our temp directory
	_ = os.Setenv("RBG_MODEL_CONFIG", modelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Load models
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Verify that user model is loaded
	var foundUserModel bool
	for _, model := range models {
		if model.ID == "test-org/test-model" {
			foundUserModel = true

			// Verify model properties
			if model.Name != "Test Model" {
				t.Errorf("Expected model name 'Test Model', got %q", model.Name)
			}

			if len(model.Modes) != 2 {
				t.Errorf("Expected 2 modes, got %d", len(model.Modes))
			}

			// Verify first mode
			if model.Modes[0].Name != "standard" {
				t.Errorf("Expected first mode name 'standard', got %q", model.Modes[0].Name)
			}
			if model.Modes[0].Engine != "vllm" {
				t.Errorf("Expected first mode engine 'vllm', got %q", model.Modes[0].Engine)
			}
			// Verify first mode resources (nvidia.com/gpu)
			if gpu, ok := model.Modes[0].Resources["nvidia.com/gpu"]; !ok || gpu.String() != "1" {
				t.Errorf("Expected first mode GPU 1, got %v", model.Modes[0].Resources["nvidia.com/gpu"])
			}

			// Verify second mode
			if model.Modes[1].Name != "throughput" {
				t.Errorf("Expected second mode name 'throughput', got %q", model.Modes[1].Name)
			}
			if model.Modes[1].Engine != "sglang" {
				t.Errorf("Expected second mode engine 'sglang', got %q", model.Modes[1].Engine)
			}

			break
		}
	}

	if !foundUserModel {
		t.Error("User-defined model not found in loaded models")
	}
}

func TestLoadUserModelsMultipleFiles(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create models directory: %v", err)
	}

	// Create first model file
	modelFile1 := filepath.Join(modelsDir, "model-a.yaml")
	modelContent1 := `- id: "org/model-a"
  name: "Model A"
  modes:
    - name: standard
      engine: vllm
      image: vllm/vllm-openai:latest
      resources:
        nvidia.com/gpu: "1"
`
	if err := os.WriteFile(modelFile1, []byte(modelContent1), 0600); err != nil {
		t.Fatalf("Failed to write model file 1: %v", err)
	}

	// Create second model file
	modelFile2 := filepath.Join(modelsDir, "model-b.yml")
	modelContent2 := `- id: "org/model-b"
  name: "Model B"
  modes:
    - name: standard
      engine: sglang
      image: lmsys/sglang:latest
      resources:
        nvidia.com/gpu: "2"
`
	if err := os.WriteFile(modelFile2, []byte(modelContent2), 0600); err != nil {
		t.Fatalf("Failed to write model file 2: %v", err)
	}

	// Set the model config path to our temp directory
	_ = os.Setenv("RBG_MODEL_CONFIG", modelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Load models
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Verify both models are loaded
	var foundModelA, foundModelB bool
	for _, model := range models {
		if model.ID == "org/model-a" {
			foundModelA = true
		}
		if model.ID == "org/model-b" {
			foundModelB = true
		}
	}

	if !foundModelA {
		t.Error("Model A not found in loaded models")
	}
	if !foundModelB {
		t.Error("Model B not found in loaded models")
	}
}

func TestLoadUserModelsInvalidFile(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create models directory: %v", err)
	}

	// Create an invalid YAML file
	invalidFile := filepath.Join(modelsDir, "invalid.yaml")
	invalidContent := `this is not valid yaml: [`
	if err := os.WriteFile(invalidFile, []byte(invalidContent), 0600); err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	// Create a valid model file
	validFile := filepath.Join(modelsDir, "valid.yaml")
	validContent := `- id: "org/valid-model"
  name: "Valid Model"
  modes:
    - name: standard
      engine: vllm
      image: vllm/vllm-openai:latest
      resources:
        nvidia.com/gpu: "1"
`
	if err := os.WriteFile(validFile, []byte(validContent), 0600); err != nil {
		t.Fatalf("Failed to write valid file: %v", err)
	}

	// Set the model config path to our temp directory
	_ = os.Setenv("RBG_MODEL_CONFIG", modelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Load models - should not fail, but should warn about invalid file
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("LoadAllModels should not fail on invalid files: %v", err)
	}

	// Verify valid model is still loaded
	var foundValidModel bool
	for _, model := range models {
		if model.ID == "org/valid-model" {
			foundValidModel = true
			break
		}
	}

	if !foundValidModel {
		t.Error("Valid model not found after loading with invalid file present")
	}
}

func TestLoadAllModelsUserOverridesBuiltin(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create models directory: %v", err)
	}

	// Create a model file that overrides a builtin model
	// Qwen/Qwen3.5-0.8B is defined in builtin models.yaml
	overrideFile := filepath.Join(modelsDir, "override.yaml")
	overrideContent := `- id: "Qwen/Qwen3.5-0.8B"
  name: "My Override"
  modes:
    - name: standard
      engine: vllm
      image: my-custom-image:latest
      resources:
        nvidia.com/gpu: "1"
`
	if err := os.WriteFile(overrideFile, []byte(overrideContent), 0600); err != nil {
		t.Fatalf("Failed to write override file: %v", err)
	}

	// Set the model config path
	_ = os.Setenv("RBG_MODEL_CONFIG", modelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Load models
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Find the model - should get user override (first match)
	var foundModel *ModelConfig
	for i := range models {
		if models[i].ID == "Qwen/Qwen3.5-0.8B" {
			foundModel = &models[i]
			break
		}
	}

	if foundModel == nil {
		t.Fatal("Model Qwen/Qwen3.5-0.8B not found")
	}

	// Should be the user override, not builtin
	if foundModel.Name != "My Override" {
		t.Errorf("Expected user override 'My Override', got %q", foundModel.Name)
	}

	// Locate the "standard" mode (modes are sorted by name)
	var standardMode *ModeConfig
	for i := range foundModel.Modes {
		if foundModel.Modes[i].Name == "standard" {
			standardMode = &foundModel.Modes[i]
			break
		}
	}
	if standardMode == nil {
		t.Fatal("Mode 'standard' not found in merged model")
	}
	if standardMode.Image != "my-custom-image:latest" {
		t.Errorf("Expected user image 'my-custom-image:latest', got %q", standardMode.Image)
	}

	// User's "standard" mode should be sourced from override.yaml
	if standardMode.Source != "override.yaml" {
		t.Errorf("Expected 'standard' mode source 'override.yaml', got %q", standardMode.Source)
	}

	// Builtin modes not overridden should still be present
	modeNames := make(map[string]bool)
	for _, m := range foundModel.Modes {
		modeNames[m.Name] = true
	}
	for _, expected := range []string{"throughput", "latency", "distributed-sglang", "distributed-vllm"} {
		if !modeNames[expected] {
			t.Errorf("Expected builtin mode %q to be preserved after mode-level merge", expected)
		}
	}
}

func TestLoadAllModelsUserDuplicate(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create models directory: %v", err)
	}

	// Create two files with the same model ID
	file1 := filepath.Join(modelsDir, "file-a.yaml")
	content1 := `- id: "my-org/duplicate-model"
  name: "Definition A"
  modes:
    - name: standard
      engine: vllm
      image: image-a:latest
      resources:
        nvidia.com/gpu: "1"
`
	if err := os.WriteFile(file1, []byte(content1), 0600); err != nil {
		t.Fatalf("Failed to write file1: %v", err)
	}

	file2 := filepath.Join(modelsDir, "file-b.yaml")
	content2 := `- id: "my-org/duplicate-model"
  name: "Definition B"
  modes:
    - name: standard
      engine: vllm
      image: image-b:latest
      resources:
        nvidia.com/gpu: "2"
`
	if err := os.WriteFile(file2, []byte(content2), 0600); err != nil {
		t.Fatalf("Failed to write file2: %v", err)
	}

	// Set the model config path
	_ = os.Setenv("RBG_MODEL_CONFIG", modelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Load models
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Find the model - should get first definition (file-a.yaml)
	var foundModel *ModelConfig
	for i := range models {
		if models[i].ID == "my-org/duplicate-model" {
			foundModel = &models[i]
			break
		}
	}

	if foundModel == nil {
		t.Fatal("Model my-org/duplicate-model not found")
	}

	// Should be from file-b.yaml (last alphabetically wins with mode-level merge)
	if foundModel.Name != "Definition B" {
		t.Errorf("Expected 'Definition B' from file-b.yaml, got %q", foundModel.Name)
	}
	// Verify resources from file-b.yaml
	if gpu, ok := foundModel.Modes[0].Resources["nvidia.com/gpu"]; !ok || gpu.String() != "2" {
		t.Errorf("Expected GPU 2 from file-b.yaml, got %v", foundModel.Modes[0].Resources["nvidia.com/gpu"])
	}
}

func TestLoadAllModelsMultipleDuplicatesAggregated(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	modelsDir := filepath.Join(tmpDir, "models")

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create models directory: %v", err)
	}

	// Create a file with 3 definitions of the same model
	file1 := filepath.Join(modelsDir, "triple.yaml")
	content1 := `- id: "my-org/triple-model"
  name: "Definition 1"
  modes:
    - name: standard
      engine: vllm
      image: image-1:latest
      resources:
        nvidia.com/gpu: "1"
- id: "my-org/triple-model"
  name: "Definition 2"
  modes:
    - name: standard
      engine: vllm
      image: image-2:latest
      resources:
        nvidia.com/gpu: "2"
- id: "my-org/triple-model"
  name: "Definition 3"
  modes:
    - name: standard
      engine: vllm
      image: image-3:latest
      resources:
        nvidia.com/gpu: "3"
`
	if err := os.WriteFile(file1, []byte(content1), 0600); err != nil {
		t.Fatalf("Failed to write file1: %v", err)
	}

	// Set the model config path
	_ = os.Setenv("RBG_MODEL_CONFIG", modelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Load models
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Find the model - should get first definition
	var foundModel *ModelConfig
	for i := range models {
		if models[i].ID == "my-org/triple-model" {
			foundModel = &models[i]
			break
		}
	}

	if foundModel == nil {
		t.Fatal("Model my-org/triple-model not found")
	}

	// With mode-level merge, the last definition in the file wins.
	if foundModel.Name != "Definition 3" {
		t.Errorf("Expected last definition 'Definition 3', got %q", foundModel.Name)
	}
	// Verify resources from last definition
	if gpu, ok := foundModel.Modes[0].Resources["nvidia.com/gpu"]; !ok || gpu.String() != "3" {
		t.Errorf("Expected last definition GPU 3, got %v", foundModel.Modes[0].Resources["nvidia.com/gpu"])
	}
}

func TestLoadAllModelsCustomModelConfigPath(t *testing.T) {
	// Create a custom models directory outside of workspace
	customModelsDir := t.TempDir()

	// Create a model file in the custom directory
	modelFile := filepath.Join(customModelsDir, "custom-model.yaml")
	modelContent := `- id: "custom-org/custom-model"
  name: "Custom Model"
  modes:
    - name: standard
      description: "Custom standard mode"
      engine: vllm
      image: vllm/vllm-openai:latest
      resources:
        nvidia.com/gpu: "1"
        cpu: "2"
        memory: 8Gi
      args:
        - "--tensor-parallel-size=1"
`
	if err := os.WriteFile(modelFile, []byte(modelContent), 0600); err != nil {
		t.Fatalf("Failed to write model file: %v", err)
	}

	// Set RBG_MODEL_CONFIG to custom directory
	_ = os.Setenv("RBG_MODEL_CONFIG", customModelsDir)
	defer func() { _ = os.Unsetenv("RBG_MODEL_CONFIG") }()

	// Set RBG_CONFIG to a different directory (should be ignored for models)
	workspaceDir := t.TempDir()
	configFile := filepath.Join(workspaceDir, "config")
	_ = os.Setenv("RBG_CONFIG", configFile)
	defer func() { _ = os.Unsetenv("RBG_CONFIG") }()

	// Load models
	models, err := LoadAllModels()
	if err != nil {
		t.Fatalf("Failed to load models: %v", err)
	}

	// Verify that custom model is loaded from RBG_MODEL_CONFIG path
	var foundCustomModel bool
	for _, model := range models {
		if model.ID == "custom-org/custom-model" {
			foundCustomModel = true

			// Verify model properties
			if model.Name != "Custom Model" {
				t.Errorf("Expected model name 'Custom Model', got %q", model.Name)
			}

			if len(model.Modes) != 1 {
				t.Errorf("Expected 1 mode, got %d", len(model.Modes))
			}

			if model.Modes[0].Engine != "vllm" {
				t.Errorf("Expected engine 'vllm', got %q", model.Modes[0].Engine)
			}

			break
		}
	}

	if !foundCustomModel {
		t.Error("Custom model from RBG_MODEL_CONFIG path not found in loaded models")
	}
}
