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

package evaluator

import (
	"context"
	"fmt"
	"sync"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

// Evaluator defines the interface for benchmark evaluation tools.
type Evaluator interface {
	// Name returns the evaluator name.
	Name() string

	// Init initializes the evaluator with plugin-specific configuration.
	// Called once after factory creation, before any Run calls.
	Init(config map[string]interface{}) error

	// Run executes the benchmark tool as a subprocess.
	Run(ctx context.Context, evalCtx EvalContext) error

	// CollectResults reads result files from the given directory and extracts metrics.
	CollectResults(resultDir string) (*abtypes.Metrics, error)
}

// EvalContext provides context for running a benchmark evaluation.
type EvalContext struct {
	Endpoint  string              // inference service URL (e.g., http://svc:8000)
	ModelName string              // model name for the benchmark tool
	Backend   string              // inference backend (sglang | vllm)
	Scenario  config.ScenarioSpec // workload scenario (generic format)
	OutputDir string              // local directory for result output
}

// Factory is a function that creates a new Evaluator instance.
type Factory func() Evaluator

var (
	registry = make(map[string]Factory)
	mu       sync.RWMutex
)

// Register registers an Evaluator factory by name.
func Register(name string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = factory
}

// Get creates a new Evaluator instance by name.
func Get(name string) (Evaluator, error) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown evaluator: %q", name)
	}
	return factory(), nil
}
