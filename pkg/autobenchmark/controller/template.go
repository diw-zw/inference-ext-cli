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
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
)

// TemplateIterator iterates over the list of template references.
type TemplateIterator struct {
	templates []config.TemplateRef
	index     int
}

// NewTemplateIterator creates a new iterator starting at the given index.
func NewTemplateIterator(templates []config.TemplateRef, startIndex int) *TemplateIterator {
	if startIndex < 0 {
		startIndex = 0
	}
	return &TemplateIterator{
		templates: templates,
		index:     startIndex,
	}
}

// HasNext returns true if there are more templates to iterate.
func (ti *TemplateIterator) HasNext() bool {
	return ti.index < len(ti.templates)
}

// Next returns the current template and advances the iterator.
// Returns nil if no more templates.
func (ti *TemplateIterator) Next() *config.TemplateRef {
	if !ti.HasNext() {
		return nil
	}
	ref := &ti.templates[ti.index]
	ti.index++
	return ref
}

// CurrentIndex returns the current iterator position.
func (ti *TemplateIterator) CurrentIndex() int {
	return ti.index
}

// Skip advances the iterator past completed templates (for resume).
func (ti *TemplateIterator) Skip(n int) {
	ti.index += n
	if ti.index > len(ti.templates) {
		ti.index = len(ti.templates)
	}
}
