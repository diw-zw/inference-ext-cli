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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
)

func TestTemplateIterator_Basic(t *testing.T) {
	templates := []config.TemplateRef{
		{Name: "t1", Template: "t1.yaml"},
		{Name: "t2", Template: "t2.yaml"},
		{Name: "t3", Template: "t3.yaml"},
	}

	iter := NewTemplateIterator(templates, 0)
	assert.True(t, iter.HasNext())
	assert.Equal(t, 0, iter.CurrentIndex())

	ref := iter.Next()
	require.NotNil(t, ref)
	assert.Equal(t, "t1", ref.Name)
	assert.Equal(t, 1, iter.CurrentIndex())

	ref = iter.Next()
	require.NotNil(t, ref)
	assert.Equal(t, "t2", ref.Name)

	ref = iter.Next()
	require.NotNil(t, ref)
	assert.Equal(t, "t3", ref.Name)

	assert.False(t, iter.HasNext())
	assert.Nil(t, iter.Next())
}

func TestTemplateIterator_Empty(t *testing.T) {
	iter := NewTemplateIterator(nil, 0)
	assert.False(t, iter.HasNext())
	assert.Nil(t, iter.Next())
}

func TestTemplateIterator_StartIndex(t *testing.T) {
	templates := []config.TemplateRef{
		{Name: "t1"},
		{Name: "t2"},
		{Name: "t3"},
	}

	// Start from index 2 (resume scenario)
	iter := NewTemplateIterator(templates, 2)
	assert.True(t, iter.HasNext())

	ref := iter.Next()
	require.NotNil(t, ref)
	assert.Equal(t, "t3", ref.Name)

	assert.False(t, iter.HasNext())
}

func TestTemplateIterator_Skip(t *testing.T) {
	templates := []config.TemplateRef{
		{Name: "t1"},
		{Name: "t2"},
		{Name: "t3"},
		{Name: "t4"},
	}

	iter := NewTemplateIterator(templates, 0)
	iter.Skip(2) // Skip first 2

	assert.True(t, iter.HasNext())
	ref := iter.Next()
	require.NotNil(t, ref)
	assert.Equal(t, "t3", ref.Name)
}

func TestTemplateIterator_SkipBeyondEnd(t *testing.T) {
	templates := []config.TemplateRef{
		{Name: "t1"},
	}

	iter := NewTemplateIterator(templates, 0)
	iter.Skip(10) // Skip more than available
	assert.False(t, iter.HasNext())
}

func TestTemplateIterator_NegativeStartIndex(t *testing.T) {
	templates := []config.TemplateRef{
		{Name: "t1"},
	}

	iter := NewTemplateIterator(templates, -5)
	assert.Equal(t, 0, iter.CurrentIndex())
	assert.True(t, iter.HasNext())
}
