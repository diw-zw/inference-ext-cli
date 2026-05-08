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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVLLMEngine_Name(t *testing.T) {
	v := &VLLMEngine{}
	assert.Equal(t, "vllm", v.Name())
}

func TestVLLMEngine_ConfigFields(t *testing.T) {
	v := &VLLMEngine{}
	fields := v.ConfigFields()
	assert.Len(t, fields, 2)
	keys := []string{fields[0].Key, fields[1].Key}
	assert.Contains(t, keys, "image")
	assert.Contains(t, keys, "port")
}

func TestVLLMEngine_Init_Defaults(t *testing.T) {
	v := &VLLMEngine{}
	err := v.Init(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "vllm/vllm-openai:latest", v.Image)
	assert.Equal(t, int32(8000), v.Port)
}

func TestVLLMEngine_Init_Custom(t *testing.T) {
	v := &VLLMEngine{}
	err := v.Init(map[string]interface{}{
		"image": "my-registry/vllm:v0.4",
		"port":  9090,
	})
	require.NoError(t, err)
	assert.Equal(t, "my-registry/vllm:v0.4", v.Image)
	assert.Equal(t, int32(9090), v.Port)
}

func TestVLLMEngine_GeneratePattern(t *testing.T) {
	v := &VLLMEngine{}
	require.NoError(t, v.Init(map[string]interface{}{}))

	pattern, err := v.GeneratePattern(GenerateOptions{
		Name:      "mymodel",
		ModelID:   "org/model",
		ModelPath: "/models/mymodel",
	})
	require.NoError(t, err)
	require.NotNil(t, pattern)
	require.NotNil(t, pattern.StandalonePattern)
	require.NotNil(t, pattern.StandalonePattern.Template)

	tpl := pattern.StandalonePattern.Template
	require.Len(t, tpl.Spec.Containers, 1)

	c := tpl.Spec.Containers[0]
	assert.Equal(t, "vllm", c.Name)
	assert.Equal(t, v.Image, c.Image)
	assert.Contains(t, c.Args, "--model")
	assert.Contains(t, c.Args, "/models/mymodel")
	assert.Contains(t, c.Args, "--served-model-name")
	assert.Contains(t, c.Args, "mymodel")
	assert.Contains(t, c.Args, "--host")
	assert.Contains(t, c.Args, "0.0.0.0")
	assert.Contains(t, c.Args, "--port")
	assert.Contains(t, c.Args, "8000")
	require.Len(t, c.Ports, 1)
	assert.Equal(t, int32(8000), c.Ports[0].ContainerPort)
	assert.Equal(t, "http", c.Ports[0].Name)
}

func TestVLLMEngine_GeneratePattern_Distributed(t *testing.T) {
	v := &VLLMEngine{}
	require.NoError(t, v.Init(map[string]interface{}{}))

	pattern, err := v.GeneratePattern(GenerateOptions{
		Name:            "mymodel",
		ModelID:         "org/model",
		ModelPath:       "/models/mymodel",
		Args:            []string{"--tensor-parallel-size=2"},
		DistributedSize: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, pattern)
	require.NotNil(t, pattern.LeaderWorkerPattern)
	require.Equal(t, int32(2), *pattern.LeaderWorkerPattern.Size)
	require.NotNil(t, pattern.LeaderWorkerPattern.Template)
	require.NotNil(t, pattern.LeaderWorkerPattern.WorkerTemplatePatch)

	tpl := pattern.LeaderWorkerPattern.Template
	require.Len(t, tpl.Spec.Containers, 1)

	c := tpl.Spec.Containers[0]
	// Check distributed args are injected (args are separate elements)
	assert.Contains(t, c.Args, "--nnodes")
	assert.Contains(t, c.Args, "$(RBG_LWP_GROUP_SIZE)")
	assert.Contains(t, c.Args, "--node-rank")
	assert.Contains(t, c.Args, "$(RBG_LWP_WORKER_INDEX)")
	assert.Contains(t, c.Args, "--master-addr")
	assert.Contains(t, c.Args, "$(RBG_LWP_LEADER_ADDRESS)")
	// Check user args are preserved
	assert.Contains(t, c.Args, "--tensor-parallel-size=2")
	// Leader should NOT have --headless
	assert.NotContains(t, c.Args, "--headless")
}

func TestVLLMEngine_GeneratePattern_Distributed_WorkerPatch(t *testing.T) {
	v := &VLLMEngine{}
	require.NoError(t, v.Init(map[string]interface{}{}))

	pattern, err := v.GeneratePattern(GenerateOptions{
		Name:            "mymodel",
		ModelID:         "org/model",
		ModelPath:       "/models/mymodel",
		Args:            []string{"--tensor-parallel-size=2"},
		DistributedSize: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, pattern.LeaderWorkerPattern.WorkerTemplatePatch)

	// Parse the WorkerTemplatePatch to verify args
	patch := pattern.LeaderWorkerPattern.WorkerTemplatePatch
	var patchData map[string]interface{}
	require.NoError(t, json.Unmarshal(patch.Raw, &patchData), "failed to parse WorkerTemplatePatch")

	// Navigate to containers[0].args
	spec, ok := patchData["spec"].(map[string]interface{})
	require.True(t, ok, "patch should have spec")
	containers, ok := spec["containers"].([]interface{})
	require.True(t, ok, "spec should have containers")
	require.Len(t, containers, 1, "should have one container")

	container, ok := containers[0].(map[string]interface{})
	require.True(t, ok, "container should be a map")
	assert.Equal(t, "vllm", container["name"], "container name should be vllm")

	args, ok := container["args"].([]interface{})
	require.True(t, ok, "container should have args")

	// Convert args to []string for easier assertions
	var argsSlice []string
	for _, arg := range args {
		if s, ok := arg.(string); ok {
			argsSlice = append(argsSlice, s)
		}
	}

	// Verify worker patch contains ALL required args
	// (Strategic Merge Patch replaces args entirely, so all args must be present)
	assert.Contains(t, argsSlice, "--model", "worker args should contain --model")
	assert.Contains(t, argsSlice, "/models/mymodel", "worker args should contain model path")
	assert.Contains(t, argsSlice, "--served-model-name", "worker args should contain --served-model-name")
	assert.Contains(t, argsSlice, "mymodel", "worker args should contain model name")
	assert.Contains(t, argsSlice, "--nnodes", "worker args should contain --nnodes")
	assert.Contains(t, argsSlice, "--node-rank", "worker args should contain --node-rank")
	assert.Contains(t, argsSlice, "--master-addr", "worker args should contain --master-addr")
	assert.Contains(t, argsSlice, "--tensor-parallel-size=2", "worker args should preserve user args")
	assert.Contains(t, argsSlice, "--headless", "worker args should have --headless flag")
}

func TestGet_VLLM_InitAndReturn(t *testing.T) {
	p, err := Get("vllm", map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "vllm", p.Name())
}

func TestValidateConfig_VLLM_OK(t *testing.T) {
	err := ValidateConfig("vllm", map[string]interface{}{"image": "custom:latest"})
	assert.NoError(t, err)
}

func TestValidateConfig_VLLM_UnknownField(t *testing.T) {
	err := ValidateConfig("vllm", map[string]interface{}{"badfield": "x"})
	assert.Error(t, err)
}

func TestGetFields_VLLM(t *testing.T) {
	fields := GetFields("vllm")
	require.NotNil(t, fields)
	assert.Len(t, fields, 2)
}
