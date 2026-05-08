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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

const checkpointFile = "checkpoint.json"

// StateManager handles experiment state persistence (checkpoint/resume).
type StateManager struct {
	baseDir string
}

// NewStateManager creates a StateManager that persists to the given directory.
func NewStateManager(baseDir string) *StateManager {
	return &StateManager{baseDir: baseDir}
}

// Save atomically writes the experiment state to disk.
// Uses write-to-temp + rename to avoid corruption on crashes.
func (sm *StateManager) Save(state *abtypes.ExperimentState) error {
	if err := os.MkdirAll(sm.baseDir, 0755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	target := filepath.Join(sm.baseDir, checkpointFile)
	tmp := target + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp checkpoint: %w", err)
	}

	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("renaming checkpoint: %w", err)
	}

	return nil
}

// Load reads the experiment state from disk. Returns nil if no checkpoint exists.
func (sm *StateManager) Load() (*abtypes.ExperimentState, error) {
	path := filepath.Join(sm.baseDir, checkpointFile)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading checkpoint: %w", err)
	}

	var state abtypes.ExperimentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshaling checkpoint: %w", err)
	}

	return &state, nil
}

// Exists returns whether a checkpoint file exists.
func (sm *StateManager) Exists() bool {
	_, err := os.Stat(filepath.Join(sm.baseDir, checkpointFile))
	return err == nil
}
