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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata", name)
}

func TestLoadTemplate_AggRBG(t *testing.T) {
	rbg, err := LoadTemplate(testdataPath("agg-rbg.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "test-agg", rbg.Name)
	require.Len(t, rbg.Spec.Roles, 1)
	assert.Equal(t, "worker", rbg.Spec.Roles[0].Name)
}

func TestLoadTemplate_DisaggMultiDoc(t *testing.T) {
	rbg, err := LoadTemplate(testdataPath("disagg-rbg.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "test-disagg", rbg.Name)
	require.Len(t, rbg.Spec.Roles, 2)
	assert.Equal(t, "prefill", rbg.Spec.Roles[0].Name)
	assert.Equal(t, "decode", rbg.Spec.Roles[1].Name)
}

func TestLoadTemplate_FileNotFound(t *testing.T) {
	_, err := LoadTemplate("/nonexistent/path.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading template file")
}

func TestLoadTemplate_NoRBGInYAML(t *testing.T) {
	// Create a temp file with only a Service
	dir := t.TempDir()
	path := filepath.Join(dir, "service-only.yaml")
	err := os.WriteFile(path, []byte(`apiVersion: v1
kind: Service
metadata:
  name: test-svc
spec:
  ports:
  - port: 8000
`), 0644)
	require.NoError(t, err)

	_, err = LoadTemplate(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no RoleBasedGroup found")
}

func TestBuilder_BuildTrial_AggOverlay(t *testing.T) {
	base, err := LoadTemplate(testdataPath("agg-rbg.yaml"))
	require.NoError(t, err)

	builder, err := NewBuilder("sglang")
	require.NoError(t, err)

	params := abtypes.RoleParamSet{
		"default": abtypes.ParamSet{
			"gpuMemoryUtilization": 0.95,
		},
	}

	trial, err := builder.BuildTrial(base, 3, params)
	require.NoError(t, err)

	// Verify trial name
	assert.Equal(t, "test-agg-trial-3", trial.Name)

	// Verify overlay: --mem-fraction-static should be 0.95 (was 0.85)
	workerCmd := trial.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command
	idx := indexOf(workerCmd, "--mem-fraction-static")
	require.NotEqual(t, -1, idx, "flag --mem-fraction-static not found in command")
	assert.Equal(t, "0.95", workerCmd[idx+1])
}

func TestBuilder_BuildTrial_DisaggRoleSpecificOverlay(t *testing.T) {
	base, err := LoadTemplate(testdataPath("disagg-rbg.yaml"))
	require.NoError(t, err)

	builder, err := NewBuilder("sglang")
	require.NoError(t, err)

	params := abtypes.RoleParamSet{
		"default": abtypes.ParamSet{
			"maxNumSeqs": 128,
		},
		"prefill": abtypes.ParamSet{
			"chunkedPrefillSize": 4096,
		},
	}

	trial, err := builder.BuildTrial(base, 0, params)
	require.NoError(t, err)
	assert.Equal(t, "test-disagg-trial-0", trial.Name)

	// Prefill role should have both default + prefill-specific params
	prefillCmd := trial.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command
	assert.Contains(t, prefillCmd, "--max-running-requests")
	assert.Contains(t, prefillCmd, "--chunked-prefill-size")

	// Decode role should have default params but NOT prefill-specific
	decodeCmd := trial.Spec.Roles[1].StandalonePattern.Template.Spec.Containers[0].Command
	assert.Contains(t, decodeCmd, "--max-running-requests")
	assert.NotContains(t, decodeCmd, "--chunked-prefill-size")
}

func TestBuilder_BuildTrial_FlagReplaceVsAppend(t *testing.T) {
	base, err := LoadTemplate(testdataPath("agg-rbg.yaml"))
	require.NoError(t, err)

	builder, err := NewBuilder("sglang")
	require.NoError(t, err)

	// Base already has --mem-fraction-static 0.85 → should replace
	// Base does NOT have --max-running-requests → should append
	params := abtypes.RoleParamSet{
		"default": abtypes.ParamSet{
			"gpuMemoryUtilization": 0.95,
			"maxNumSeqs":           256,
		},
	}

	trial, err := builder.BuildTrial(base, 1, params)
	require.NoError(t, err)

	cmd := trial.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command

	// Replaced value
	memIdx := indexOf(cmd, "--mem-fraction-static")
	require.NotEqual(t, -1, memIdx)
	assert.Equal(t, "0.95", cmd[memIdx+1])

	// Appended flag
	seqsIdx := indexOf(cmd, "--max-running-requests")
	require.NotEqual(t, -1, seqsIdx)
	assert.Equal(t, "256", cmd[seqsIdx+1])
}

func TestBuilder_BuildTrial_DeepCopySafety(t *testing.T) {
	base, err := LoadTemplate(testdataPath("agg-rbg.yaml"))
	require.NoError(t, err)

	// Save original command for comparison
	origCmd := make([]string, len(base.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command))
	copy(origCmd, base.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command)

	builder, err := NewBuilder("sglang")
	require.NoError(t, err)

	_, err = builder.BuildTrial(base, 0, abtypes.RoleParamSet{
		"default": abtypes.ParamSet{"gpuMemoryUtilization": 0.99},
	})
	require.NoError(t, err)

	// Verify base is NOT modified
	assert.Equal(t, origCmd, base.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command)
	assert.Equal(t, "test-agg", base.Name) // name should not change
}

func TestBuilder_BuildTrial_TrialNaming(t *testing.T) {
	base, err := LoadTemplate(testdataPath("agg-rbg.yaml"))
	require.NoError(t, err)

	builder, err := NewBuilder("sglang")
	require.NoError(t, err)

	for _, idx := range []int{0, 1, 42} {
		trial, err := builder.BuildTrial(base, idx, abtypes.RoleParamSet{})
		require.NoError(t, err)
		assert.Equal(t, GetTrialName("test-agg", idx), trial.Name)
		assert.Empty(t, trial.ResourceVersion)
		assert.Empty(t, trial.UID)
	}
}

func TestBuilder_BuildTrial_SplitCommandArgs(t *testing.T) {
	// Template uses command: [python3, -m, sglang.launch_server] + args: [--model-path, ...]
	// This verifies mergeCommandArgs correctly merges both fields.
	base, err := LoadTemplate(testdataPath("agg-rbg-split-args.yaml"))
	require.NoError(t, err)

	builder, err := NewBuilder("sglang")
	require.NoError(t, err)

	params := abtypes.RoleParamSet{
		"default": abtypes.ParamSet{
			"gpuMemoryUtilization": 0.95,
		},
	}

	trial, err := builder.BuildTrial(base, 0, params)
	require.NoError(t, err)

	// After overlay, everything is written to Command, Args is nil
	cmd := trial.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Command
	args := trial.Spec.Roles[0].StandalonePattern.Template.Spec.Containers[0].Args

	// Args from original should be merged into Command
	assert.Contains(t, cmd, "--model-path")
	assert.Contains(t, cmd, "/models/llama")
	assert.Contains(t, cmd, "--port")
	assert.Contains(t, cmd, "8000")
	assert.Contains(t, cmd, "--tensor-parallel-size")

	// --mem-fraction-static should be replaced with 0.95
	memIdx := indexOf(cmd, "--mem-fraction-static")
	require.NotEqual(t, -1, memIdx, "flag --mem-fraction-static not found in merged command")
	assert.Equal(t, "0.95", cmd[memIdx+1])

	// Args should be nil (everything moved to Command)
	assert.Nil(t, args)
}

func TestGetServiceEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		port      int
		want      string
	}{
		{
			name:      "standard",
			namespace: "default",
			port:      8000,
			want:      "http://s-my-rbg-worker.default.svc.cluster.local:8000",
		},
		{
			name:      "disagg inference",
			namespace: "benchmark",
			port:      8000,
			want:      "http://s-test-disagg-router.benchmark.svc.cluster.local:8000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rbg *workloadsv1alpha2.RoleBasedGroup
			var role *workloadsv1alpha2.RoleSpec
			switch tt.name {
			case "standard":
				rbg = &workloadsv1alpha2.RoleBasedGroup{ObjectMeta: metav1.ObjectMeta{Name: "my-rbg"}}
				role = &workloadsv1alpha2.RoleSpec{Name: "worker"}
			case "disagg inference":
				rbg = &workloadsv1alpha2.RoleBasedGroup{ObjectMeta: metav1.ObjectMeta{Name: "test-disagg"}}
				role = &workloadsv1alpha2.RoleSpec{Name: "router"}
			}
			got := GetServiceEndpoint(rbg, role, tt.namespace, tt.port)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetTrialName(t *testing.T) {
	assert.Equal(t, "my-rbg-trial-0", GetTrialName("my-rbg", 0))
	assert.Equal(t, "my-rbg-trial-42", GetTrialName("my-rbg", 42))
}

// indexOf finds the index of a string in a slice, or -1 if not found.
func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}
