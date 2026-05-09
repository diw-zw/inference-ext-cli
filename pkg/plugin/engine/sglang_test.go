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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSGLangEngine_Name(t *testing.T) {
	s := &SGLangEngine{}
	assert.Equal(t, "sglang", s.Name())
}

func TestSGLangEngine_ConfigFields(t *testing.T) {
	s := &SGLangEngine{}
	fields := s.ConfigFields()
	assert.Len(t, fields, 2)
	keys := []string{fields[0].Key, fields[1].Key}
	assert.Contains(t, keys, "image")
	assert.Contains(t, keys, "port")
}

func TestSGLangEngine_Init_Defaults(t *testing.T) {
	s := &SGLangEngine{}
	err := s.Init(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "lmsysorg/sglang:latest", s.Image)
	assert.Equal(t, int32(30000), s.Port)
}

func TestSGLangEngine_Init_Custom(t *testing.T) {
	s := &SGLangEngine{}
	err := s.Init(map[string]interface{}{
		"image": "my-sglang:v1",
		"port":  8888,
	})
	require.NoError(t, err)
	assert.Equal(t, "my-sglang:v1", s.Image)
	assert.Equal(t, int32(8888), s.Port)
}

func TestSGLangEngine_GeneratePattern(t *testing.T) {
	s := &SGLangEngine{}
	require.NoError(t, s.Init(map[string]interface{}{}))

	pattern, err := s.GeneratePattern(GenerateOptions{
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
	assert.Equal(t, "sglang", c.Name)
	assert.Equal(t, s.Image, c.Image)
	assert.Equal(t, []string{"python", "-m", "sglang.launch_server"}, c.Command)
	assert.Contains(t, c.Args, "--model-path")
	assert.Contains(t, c.Args, "/models/mymodel")
	assert.Contains(t, c.Args, "--served-model-name")
	assert.Contains(t, c.Args, "mymodel")
	assert.Contains(t, c.Args, "--host")
	assert.Contains(t, c.Args, "0.0.0.0")
	assert.Contains(t, c.Args, "--port")
	assert.Contains(t, c.Args, "30000")
	require.Len(t, c.Ports, 1)
	assert.Equal(t, int32(30000), c.Ports[0].ContainerPort)
	assert.Equal(t, "http", c.Ports[0].Name)
}

func TestSGLangEngine_GeneratePattern_EnvVar(t *testing.T) {
	s := &SGLangEngine{}
	require.NoError(t, s.Init(map[string]interface{}{}))

	pattern, err := s.GeneratePattern(GenerateOptions{
		Name:      "m",
		ModelID:   "id",
		ModelPath: "/path/to/model",
	})
	require.NoError(t, err)
	tpl := pattern.StandalonePattern.Template
	envMap := map[string]string{}
	for _, e := range tpl.Spec.Containers[0].Env {
		envMap[e.Name] = e.Value
	}
	assert.Equal(t, "/path/to/model", envMap["SGLANG_MODEL_PATH"])
}

func TestSGLangEngine_GeneratePattern_Distributed(t *testing.T) {
	s := &SGLangEngine{}
	require.NoError(t, s.Init(map[string]interface{}{}))

	pattern, err := s.GeneratePattern(GenerateOptions{
		Name:            "mymodel",
		ModelID:         "org/model",
		ModelPath:       "/models/mymodel",
		Args:            []string{"--tp-size=2"},
		DistributedSize: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, pattern)
	require.NotNil(t, pattern.LeaderWorkerPattern)
	require.Equal(t, int32(2), *pattern.LeaderWorkerPattern.Size)
	require.NotNil(t, pattern.LeaderWorkerPattern.Template)

	tpl := pattern.LeaderWorkerPattern.Template
	require.Len(t, tpl.Spec.Containers, 1)

	c := tpl.Spec.Containers[0]
	// Check distributed args are injected
	assert.Contains(t, c.Args, "--dist-init-addr=$(RBG_LWP_LEADER_ADDRESS):6379")
	assert.Contains(t, c.Args, "--nnodes=$(RBG_LWP_GROUP_SIZE)")
	assert.Contains(t, c.Args, "--node-rank=$(RBG_LWP_WORKER_INDEX)")
	// Check user args are preserved
	assert.Contains(t, c.Args, "--tp-size=2")
}

func TestGet_SGLang_InitAndReturn(t *testing.T) {
	p, err := Get("sglang", map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "sglang", p.Name())
}

func TestValidateConfig_SGLang_OK(t *testing.T) {
	err := ValidateConfig("sglang", map[string]interface{}{"port": 12345})
	assert.NoError(t, err)
}

func TestValidateConfig_SGLang_UnknownField(t *testing.T) {
	err := ValidateConfig("sglang", map[string]interface{}{"badfield": "x"})
	assert.Error(t, err)
}

func TestGetFields_SGLang(t *testing.T) {
	fields := GetFields("sglang")
	require.NotNil(t, fields)
	assert.Len(t, fields, 2)
}
