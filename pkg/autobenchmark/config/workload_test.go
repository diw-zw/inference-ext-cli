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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorkload_Fixed(t *testing.T) {
	w, err := ParseWorkload("fixed(100,1000)")
	require.NoError(t, err)
	assert.Equal(t, WorkloadFixed, w.Type)
	assert.Equal(t, 100, w.InputTokens)
	assert.Equal(t, 1000, w.OutputTokens)
}

func TestParseWorkload_Normal(t *testing.T) {
	w, err := ParseWorkload("normal(480,240/300,150)")
	require.NoError(t, err)
	assert.Equal(t, WorkloadNormal, w.Type)
	assert.Equal(t, 480, w.InputMean)
	assert.Equal(t, 240, w.InputStdDev)
	assert.Equal(t, 300, w.OutputMean)
	assert.Equal(t, 150, w.OutputStdDev)
}

func TestParseWorkload_Uniform(t *testing.T) {
	w, err := ParseWorkload("uniform(100,500/200,800)")
	require.NoError(t, err)
	assert.Equal(t, WorkloadUniform, w.Type)
	assert.Equal(t, 100, w.InputMin)
	assert.Equal(t, 500, w.InputMax)
	assert.Equal(t, 200, w.OutputMin)
	assert.Equal(t, 800, w.OutputMax)
}

func TestParseWorkload_Dataset(t *testing.T) {
	w, err := ParseWorkload("dataset")
	require.NoError(t, err)
	assert.Equal(t, WorkloadDataset, w.Type)
}

func TestParseWorkload_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"random text", "foobar"},
		{"genai-bench format D", "D(100,1000)"},
		{"genai-bench format N", "N(480,240)/(300,150)"},
		{"genai-bench format E", "E(1024)"},
		{"missing parens", "fixed100,1000"},
		{"extra spaces", "fixed( 100, 1000)"},
		{"negative numbers", "fixed(-100,1000)"},
		{"float numbers", "fixed(100.5,1000)"},
		{"incomplete normal", "normal(480,240)"},
		{"incomplete uniform", "uniform(100,500)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseWorkload(tt.input)
			assert.Error(t, err, "expected error for input %q", tt.input)
		})
	}
}

func TestValidateWorkload(t *testing.T) {
	assert.NoError(t, ValidateWorkload("fixed(100,1000)"))
	assert.NoError(t, ValidateWorkload("normal(480,240/300,150)"))
	assert.NoError(t, ValidateWorkload("uniform(100,500/200,800)"))
	assert.NoError(t, ValidateWorkload("dataset"))
	assert.Error(t, ValidateWorkload("invalid"))
}
