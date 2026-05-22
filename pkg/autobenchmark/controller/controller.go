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
	"context"
	"fmt"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/constant"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/evaluator"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/lifecycle"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/search"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

// Controller orchestrates the auto-benchmark experiment loop.
type Controller struct {
	cfg       *config.AutoBenchmarkConfig
	client    client.Client
	clientset kubernetes.Interface
	namespace string

	builder *lifecycle.Builder
	manager *lifecycle.RBGManager
	eval    evaluator.Evaluator

	state     *StateManager
	reportDir string

	// parsed durations
	timeout         time.Duration
	rbgReadyTimeout time.Duration
	trialTimeout    time.Duration

	// sanitized experiment name for label values (DNS-1123 compliant)
	expNameLabel string
}

// NewController creates a Controller from config.
func NewController(
	cfg *config.AutoBenchmarkConfig,
	c client.Client,
	cs kubernetes.Interface,
	namespace string,
	stateDir string,
	reportDir string,
) (*Controller, error) {
	builder, err := lifecycle.NewBuilder(cfg.Backend)
	if err != nil {
		return nil, fmt.Errorf("creating builder: %w", err)
	}

	// Validate algorithm name early (don't store; Run creates per-template instances).
	if _, err := search.Get(cfg.Strategy.Algorithm); err != nil {
		return nil, fmt.Errorf("creating search algorithm: %w", err)
	}

	eval, err := evaluator.Get(cfg.Evaluator.Type)
	if err != nil {
		return nil, fmt.Errorf("creating evaluator: %w", err)
	}

	if err := eval.Init(cfg.Evaluator.Config); err != nil {
		return nil, fmt.Errorf("initializing evaluator: %w", err)
	}

	// Duration strings are validated during config parsing (setDefaults + Validate),
	// so errors here are effectively unreachable in normal flow.
	timeout, _ := time.ParseDuration(cfg.Strategy.Timeout)
	rbgReady, _ := time.ParseDuration(cfg.Execution.RBGReadyTimeout)
	trialTO, _ := time.ParseDuration(cfg.Execution.TrialTimeout)

	// Sanitize experiment name for use as Kubernetes label value
	expNameLabel := sanitizeLabelValue(cfg.Name)

	return &Controller{
		cfg:             cfg,
		client:          c,
		clientset:       cs,
		namespace:       namespace,
		builder:         builder,
		manager:         lifecycle.NewRBGManager(c, namespace),
		eval:            eval,
		state:           NewStateManager(stateDir),
		reportDir:       reportDir,
		timeout:         timeout,
		rbgReadyTimeout: rbgReady,
		trialTimeout:    trialTO,
		expNameLabel:    expNameLabel,
	}, nil
}

// Run executes the main orchestration loop.
func (ctrl *Controller) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("experiment", ctrl.cfg.Name)
	ctx = log.IntoContext(ctx, logger)

	// Apply overall timeout
	if ctrl.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ctrl.timeout)
		defer cancel()
	}

	// Load or initialize experiment state
	expState, err := ctrl.loadOrInitState(ctx)
	if err != nil {
		return fmt.Errorf("initializing state: %w", err)
	}

	// Create algorithm instance once; Init will be called per template.
	algoInstance, err := search.Get(ctrl.cfg.Strategy.Algorithm)
	if err != nil {
		return err
	}
	// Close subprocess (if any) when the experiment finishes.
	if closer, ok := algoInstance.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	// Template iteration loop
	iter := NewTemplateIterator(ctrl.cfg.Templates, expState.CurrentTemplateIdx)

	for iter.HasNext() {
		if err := ctx.Err(); err != nil {
			logger.Info("Experiment timeout reached, saving state")
			break
		}

		tmplRef := iter.Next()
		tmplIdx := iter.CurrentIndex() - 1 // index of current template
		logger.Info("Processing template", "index", tmplIdx, "template", tmplRef.Name)

		// Ensure template state exists
		for len(expState.Templates) <= tmplIdx {
			expState.Templates = append(expState.Templates, abtypes.TemplateState{
				Name: tmplRef.Name,
			})
		}
		ts := &expState.Templates[tmplIdx]

		if ts.Completed {
			logger.Info("Template already completed, skipping", "template", tmplRef.Name)
			continue
		}

		// (Re-)initialize algorithm for this template. The algorithm instance
		// is created once before the loop; Init reuses the subprocess.
		if err := algoInstance.Init(ctx, tmplRef.Name, ctrl.cfg.SearchSpace, ctrl.cfg.Strategy); err != nil {
			return fmt.Errorf("initializing algorithm for template %q: %w", tmplRef.Name, err)
		}
		if len(ts.AlgorithmState) > 0 {
			if err := algoInstance.UnmarshalState(ts.AlgorithmState); err != nil {
				logger.Info("Failed to restore algorithm state, continuing", "template", tmplRef.Name, "error", err.Error())
			}
		}

		// Load base RBG template
		baseRBG, err := lifecycle.LoadTemplate(tmplRef.Template)
		if err != nil {
			return fmt.Errorf("loading template %q: %w", tmplRef.Name, err)
		}

		// Trial loop for this template
		err = ctrl.runTrials(ctx, expState, ts, tmplIdx, baseRBG, algoInstance)
		if err != nil {
			logger.Error(err, "Error running trials", "template", tmplRef.Name)
		}

		// Only mark completed if we didn't time out mid-loop — a timed-out
		// template should be resumable on the next run.
		if ctx.Err() != nil {
			logger.Info("Experiment timeout during template, not marking completed", "template", tmplRef.Name)
			ts.BestTrial = SelectBest(ts.Trials)
		} else {
			ts.Completed = true
			ts.BestTrial = SelectBest(ts.Trials)
		}

		// Update global best
		if ts.BestTrial != nil {
			if expState.GlobalBest == nil || ts.BestTrial.Score > expState.GlobalBest.Score {
				expState.GlobalBest = ts.BestTrial
			}
		}

		expState.CurrentTemplateIdx = iter.CurrentIndex()
		if err := ctrl.state.Save(expState); err != nil {
			logger.Error(err, "Failed to save checkpoint")
		}
	}

	// Generate report
	endTime := time.Now()
	report := BuildReport(expState)
	if err := WriteReportJSON(ctrl.reportDir, report); err != nil {
		logger.Error(err, "Failed to write report", "format", "json")
	}
	if err := WriteReportYAML(ctrl.reportDir, report); err != nil {
		logger.Error(err, "Failed to write report", "format", "yaml")
	}
	// Write full result detail for UI consumption
	result := BuildResult(expState, ctrl.cfg, endTime)
	if err := WriteResultJSON(ctrl.reportDir, result); err != nil {
		logger.Error(err, "Failed to write result detail", "format", "json")
	}

	// Write best trial RBG YAML
	if expState.GlobalBest != nil {
		if err := ctrl.writeBestTrialYAML(expState.GlobalBest); err != nil {
			logger.Error(err, "Failed to write best trial YAML")
		}
	}

	logger.Info("Experiment completed", "summary", report.Summary)
	return nil
}

// runTrials executes the trial loop for a single template.
func (ctrl *Controller) runTrials(
	ctx context.Context,
	expState *abtypes.ExperimentState,
	ts *abtypes.TemplateState,
	tmplIdx int,
	baseRBG *v1alpha2.RoleBasedGroup,
	algo search.SearchAlgorithm,
) error {
	logger := log.FromContext(ctx).WithValues("template", ts.Name)

	for !algo.IsDone(ts.Trials) {
		if err := ctx.Err(); err != nil {
			return nil // timeout, graceful exit
		}

		trialIdx := len(ts.Trials)
		params, err := algo.SuggestNext(ts.Trials)
		if err != nil {
			logger.Error(err, "Algorithm suggest failed")
			break
		}

		logger.Info("Starting trial", "trialIndex", trialIdx, "params", params)

		result := ctrl.executeTrial(ctx, baseRBG, ts.Name, trialIdx, params)
		if result.Error != "" {
			logger.Info("Trial failed", "trialIndex", trialIdx, "error", result.Error, "duration", time.Duration(result.Duration).String())
		} else {
			logger.Info("Trial completed", "trialIndex", trialIdx, "feasible", result.IsSLAFeasible(), "score", result.Score, "duration", time.Duration(result.Duration).String())
		}
		ts.Trials = append(ts.Trials, result)

		// Save algorithm state
		algoState, err := algo.MarshalState()
		if err == nil {
			ts.AlgorithmState = algoState
		}

		// Checkpoint after each trial
		expState.CurrentTemplateIdx = tmplIdx
		if err := ctrl.state.Save(expState); err != nil {
			logger.Error(err, "Failed to save checkpoint")
		}

		// Write incremental result detail for mid-flight visibility
		resultDetail := BuildResult(expState, ctrl.cfg, time.Time{})
		if err := WriteResultJSON(ctrl.reportDir, resultDetail); err != nil {
			logger.Error(err, "Failed to write result detail")
		}

		// Check early termination conditions
		if et := CheckEarlyTermination(ts.Trials, ctrl.cfg.Strategy.EarlyTermination); et.Terminated {
			logger.Info("Early termination triggered", "reason", et.Reason)
			ts.TerminationReason = et.Reason
			break
		}
	}

	return nil
}

// executeTrial runs a single trial: deploy RBG -> wait ready -> run benchmark in-process -> collect results -> cleanup.
func (ctrl *Controller) executeTrial(
	ctx context.Context,
	baseRBG *v1alpha2.RoleBasedGroup,
	templateName string,
	trialIdx int,
	params abtypes.RoleParamSet,
) abtypes.TrialResult {
	logger := log.FromContext(ctx).WithValues("trialIndex", trialIdx)
	ctx = log.IntoContext(ctx, logger)

	start := time.Now()
	result := abtypes.TrialResult{
		TrialIndex:   trialIdx,
		TemplateName: templateName,
		Params:       params,
		StartTime:    start,
	}

	// Apply trial timeout early so it governs all trial phases
	// (creation, readiness, and benchmark execution).
	trialCtx := ctx
	if ctrl.trialTimeout > 0 {
		var cancel context.CancelFunc
		trialCtx, cancel = context.WithTimeout(ctx, ctrl.trialTimeout)
		defer cancel()
	}

	// Build trial RBG
	trialRBG, err := ctrl.builder.BuildTrial(baseRBG, trialIdx, params)
	if err != nil {
		result.Error = fmt.Sprintf("build trial: %v", err)
		result.EndTime = time.Now()
		result.Duration = abtypes.Duration(result.EndTime.Sub(start))
		return result
	}

	// Tag trial RBG with experiment label for scoped cleanup
	if trialRBG.Labels == nil {
		trialRBG.Labels = make(map[string]string)
	}
	trialRBG.Labels[constant.AutoBenchmarkLabelKey] = ctrl.expNameLabel
	if trialRBG.Annotations == nil {
		trialRBG.Annotations = make(map[string]string)
	}
	trialRBG.Annotations[constant.AutoBenchmarkOriginalNameAnnotationKey] = ctrl.cfg.Name

	trialName := trialRBG.Name
	rbgCreated := false

	defer func() {
		if !rbgCreated {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		err := wait.ExponentialBackoffWithContext(cleanupCtx, wait.Backoff{
			Duration: 5 * time.Second,
			Factor:   1,
			Steps:    3,
		}, func(ctx context.Context) (bool, error) {
			if delErr := ctrl.manager.Delete(ctx, trialName); delErr != nil {
				logger.Info("Failed to cleanup trial RBG, retrying", "rbgName", trialName, "error", delErr.Error())
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			logger.Error(err, "Failed to cleanup trial RBG after retries", "rbgName", trialName)
		}
	}()

	// Create trial RBG
	if err := ctrl.manager.Create(trialCtx, trialRBG); err != nil {
		result.Error = fmt.Sprintf("create RBG: %v", err)
		result.EndTime = time.Now()
		result.Duration = abtypes.Duration(result.EndTime.Sub(start))
		ctrl.annotateTrialTimeout(&result, trialCtx)
		return result
	}
	rbgCreated = true

	// Wait for RBG to be fully ready (Pod Ready + inference endpoint serving).
	endpoint, err := ctrl.waitRBGFullyReady(trialCtx, trialRBG, trialName, ctrl.rbgReadyTimeout)
	if err != nil {
		result.Error = fmt.Sprintf("RBG not ready: %v", err)
		result.EndTime = time.Now()
		result.Duration = abtypes.Duration(result.EndTime.Sub(start))
		ctrl.annotateTrialTimeout(&result, trialCtx)

		scenario := ctrl.cfg.Scenario
		resultDir := filepath.Join(ctrl.reportDir, scenario.Name, templateName, fmt.Sprintf("trial-%d", trialIdx))
		ctrl.collectFailureLogs(logger, trialName, resultDir, &result)
		return result
	}
	modelName := extractServedModelName(baseRBG, ctrl.cfg.Backend)

	// Result directory: {reportDir}/{scenario}/{templateName}/trial-{idx}
	scenario := ctrl.cfg.Scenario
	resultDir := filepath.Join(ctrl.reportDir, scenario.Name, templateName, fmt.Sprintf("trial-%d", trialIdx))

	// Snapshot pod restart counts before benchmark so we can detect mid-run crashes.
	preRunRestarts := ctrl.snapshotRestartCounts(logger, trialName)

	// Run benchmark in-process
	evalCtx := evaluator.EvalContext{
		Endpoint:  endpoint,
		ModelName: modelName,
		Backend:   ctrl.cfg.Backend,
		Scenario:  scenario,
		OutputDir: resultDir,
	}

	if err := ctrl.eval.Run(trialCtx, evalCtx); err != nil {
		result.Error = fmt.Sprintf("benchmark failed: %v", err)
		result.EndTime = time.Now()
		result.Duration = abtypes.Duration(result.EndTime.Sub(start))
		ctrl.annotateTrialTimeout(&result, trialCtx)
		ctrl.collectBenchmarkFailureLogs(logger, trialName, resultDir, preRunRestarts)
		return result
	}

	// Collect results from local output directory
	metrics, err := ctrl.eval.CollectResults(resultDir)
	if err != nil {
		result.Error = fmt.Sprintf("collecting results: %v", err)
		result.EndTime = time.Now()
		result.Duration = abtypes.Duration(result.EndTime.Sub(start))
		ctrl.annotateTrialTimeout(&result, trialCtx)
		return result
	}

	// Evaluate SLA
	result.Metrics = metrics
	result.Constraints, result.Score = EvaluateSLA(metrics, ctrl.cfg.Objectives)
	result.EndTime = time.Now()
	result.Duration = abtypes.Duration(result.EndTime.Sub(start))

	return result
}

// annotateTrialTimeout appends trial timeout context to an error result when
// the trial context's deadline has been exceeded.
func (ctrl *Controller) annotateTrialTimeout(result *abtypes.TrialResult, trialCtx context.Context) {
	if result.Error != "" && trialCtx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Sprintf("%s (trial timeout after %s)", result.Error, ctrl.trialTimeout)
	}
}

// loadOrInitState loads checkpoint or creates new state.
func (ctrl *Controller) loadOrInitState(ctx context.Context) (*abtypes.ExperimentState, error) {
	existing, err := ctrl.state.Load()
	if err != nil {
		return nil, err
	}
	if existing != nil {
		logger := log.FromContext(ctx)
		logger.Info("Resuming experiment from checkpoint", "templateIndex", existing.CurrentTemplateIdx)
		return existing, nil
	}

	return &abtypes.ExperimentState{
		ExperimentID: ctrl.cfg.Name,
		StartTime:    time.Now(),
	}, nil
}
