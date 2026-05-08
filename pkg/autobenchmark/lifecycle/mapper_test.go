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

package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewParamMapper(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		wantErr string
	}{
		{name: "sglang", backend: "sglang"},
		{name: "vllm", backend: "vllm"},
		{name: "unsupported", backend: "trt-llm", wantErr: "unsupported backend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, err := NewParamMapper(tt.backend)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.backend, pm.Backend())
		})
	}
}

func TestParamMapper_MapParam(t *testing.T) {
	tests := []struct {
		name     string
		backend  string
		param    string
		wantFlag string
		wantErr  string
	}{
		// SGLang mappings
		{name: "sglang/gpuMemUtil", backend: "sglang", param: "gpuMemoryUtilization", wantFlag: "--mem-fraction-static"},
		{name: "sglang/maxNumSeqs", backend: "sglang", param: "maxNumSeqs", wantFlag: "--max-running-requests"},
		{name: "sglang/chunkedPrefillSize", backend: "sglang", param: "chunkedPrefillSize", wantFlag: "--chunked-prefill-size"},
		{name: "sglang/contextLength", backend: "sglang", param: "contextLength", wantFlag: "--context-length"},
		{name: "sglang/tensorParallel", backend: "sglang", param: "tensorParallelSize", wantFlag: "--tensor-parallel-size"},
		{name: "sglang/enableCudaGraph", backend: "sglang", param: "enableCudaGraph", wantFlag: "--enable-cuda-graph"},

		// vLLM mappings
		{name: "vllm/gpuMemUtil", backend: "vllm", param: "gpuMemoryUtilization", wantFlag: "--gpu-memory-utilization"},
		{name: "vllm/maxNumSeqs", backend: "vllm", param: "maxNumSeqs", wantFlag: "--max-num-seqs"},
		{name: "vllm/contextLength", backend: "vllm", param: "contextLength", wantFlag: "--max-model-len"},
		{name: "vllm/enforceEager", backend: "vllm", param: "enforceEager", wantFlag: "--enforce-eager"},

		// Unknown param
		{name: "sglang/unknown", backend: "sglang", param: "nonExistentParam", wantErr: "unknown parameter"},
		{name: "vllm/unknown", backend: "vllm", param: "nonExistentParam", wantErr: "unknown parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, err := NewParamMapper(tt.backend)
			require.NoError(t, err)

			flag, err := pm.MapParam(tt.param)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFlag, flag)
		})
	}
}

func TestParamMapper_OverlayArgs(t *testing.T) {
	tests := []struct {
		name     string
		backend  string
		args     []string
		params   map[string]interface{}
		wantArgs []string
		wantErr  string
	}{
		{
			name:     "replace existing flag value",
			backend:  "sglang",
			args:     []string{"python3", "-m", "sglang.launch_server", "--mem-fraction-static", "0.85"},
			params:   map[string]interface{}{"gpuMemoryUtilization": 0.95},
			wantArgs: []string{"python3", "-m", "sglang.launch_server", "--mem-fraction-static", "0.95"},
		},
		{
			name:     "append new flag",
			backend:  "sglang",
			args:     []string{"python3", "-m", "sglang.launch_server"},
			params:   map[string]interface{}{"gpuMemoryUtilization": 0.9},
			wantArgs: []string{"python3", "-m", "sglang.launch_server", "--mem-fraction-static", "0.9"},
		},
		{
			name:     "multiple params",
			backend:  "sglang",
			args:     []string{"python3", "-m", "sglang.launch_server", "--mem-fraction-static", "0.85"},
			params:   map[string]interface{}{"gpuMemoryUtilization": 0.9, "maxNumSeqs": 128},
			wantArgs: nil, // just check it doesn't error, order may vary
		},
		{
			name:    "unknown param returns error",
			backend: "sglang",
			args:    []string{"python3"},
			params:  map[string]interface{}{"unknownParam": "x"},
			wantErr: "unknown parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, err := NewParamMapper(tt.backend)
			require.NoError(t, err)

			result, err := pm.OverlayArgs(tt.args, tt.params)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			if tt.wantArgs != nil {
				assert.Equal(t, tt.wantArgs, result)
			}

			// For multiple params, verify both flags exist
			if tt.name == "multiple params" {
				assert.Contains(t, result, "--mem-fraction-static")
				assert.Contains(t, result, "--max-running-requests")
			}
		})
	}
}

func TestOverlayArgs_DoesNotMutateOriginal(t *testing.T) {
	pm, err := NewParamMapper("sglang")
	require.NoError(t, err)

	original := []string{"python3", "-m", "sglang.launch_server", "--mem-fraction-static", "0.85"}
	originalCopy := make([]string, len(original))
	copy(originalCopy, original)

	_, err = pm.OverlayArgs(original, map[string]interface{}{"maxNumSeqs": 128})
	require.NoError(t, err)

	// Original should not be mutated (the append added a new flag)
	assert.Equal(t, originalCopy, original)
}

func TestReplaceOrAppendFlag(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		flag  string
		value string
		want  []string
	}{
		{
			name:  "replace space-separated",
			args:  []string{"--foo", "old", "--bar", "baz"},
			flag:  "--foo",
			value: "new",
			want:  []string{"--foo", "new", "--bar", "baz"},
		},
		{
			name:  "replace equals format",
			args:  []string{"--foo=old", "--bar", "baz"},
			flag:  "--foo",
			value: "new",
			want:  []string{"--foo=new", "--bar", "baz"},
		},
		{
			name:  "append when not found",
			args:  []string{"--bar", "baz"},
			flag:  "--foo",
			value: "new",
			want:  []string{"--bar", "baz", "--foo", "new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceOrAppendFlag(tt.args, tt.flag, tt.value)
			assert.Equal(t, tt.want, got)
		})
	}
}
