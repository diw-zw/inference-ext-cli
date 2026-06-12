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

package svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"

	"sigs.k8s.io/rbgs/api/workloads/constants"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/shared"
	"sigs.k8s.io/rbgs/cli/cmd/llmctl/svc/chat"
	llmmeta "sigs.k8s.io/rbgs/cli/cmd/llmctl/svc/metadata"
	runpkg "sigs.k8s.io/rbgs/cli/cmd/llmctl/svc/run"
	cliconfig "sigs.k8s.io/rbgs/cli/pkg/config"
	engineplugin "sigs.k8s.io/rbgs/cli/pkg/plugin/engine"
	storageplugin "sigs.k8s.io/rbgs/cli/pkg/plugin/storage"
	"sigs.k8s.io/rbgs/cli/pkg/util"
)

// resolveEngine resolves the engine configuration.
// First tries to get from user config, then falls back to registered plugin with defaults.
func resolveEngine(engineType string, cfg *cliconfig.Config) (*cliconfig.EngineConfig, error) {
	// 1. Try to get from user config (if available)
	if cfg != nil {
		if engineCfg, err := cfg.GetEngine(engineType); err == nil {
			return engineCfg, nil
		}
	}

	// 2. Check if it's a registered plugin type
	if !engineplugin.IsRegistered(engineType) {
		return nil, fmt.Errorf("unknown engine type '%s'", engineType)
	}

	// 3. Use default (empty config) - plugin will use its built-in defaults
	fmt.Printf("INFO: Using default configuration for engine '%s'. Run 'llmctl config set-engine %s' to customize.\n", engineType, engineType)
	return &cliconfig.EngineConfig{
		Type:   engineType,
		Config: map[string]interface{}{},
	}, nil
}

// RunParams holds all flag values supplied to the run command.
type RunParams struct {
	Mode             string
	Engine           string
	Image            string
	Storage          string
	Revision         string
	ModelPath        string
	EnvVars          []string
	ArgsList         []string
	DryRun           bool
	Replicas         int32
	Resources        []string // key=value pairs, e.g. "nvidia.com/gpu=1"
	DistributedSize  int32
	ShmSize          string
	ModelPrefetch    bool
	Tolerations      []string
	ImagePullSecrets []string
	HostNetwork      bool
	NodeSelector     []string // key=value pairs
}

// modeConfigResult holds the result of mode config resolution.
type modeConfigResult struct {
	modelCfg     *runpkg.ModelConfig
	modeCfg      *runpkg.ModeConfig
	enginePlugin engineplugin.Plugin
	engineType   string
}

// resolveModeConfig resolves model, mode, and engine configuration.
// If no model config is found but --engine is specified, a minimal config is constructed from flags.
func resolveModeConfig(modelID string, p RunParams, userCfg *cliconfig.Config) (*modeConfigResult, error) {
	var modelCfg *runpkg.ModelConfig
	var modeCfg *runpkg.ModeConfig

	models, err := runpkg.LoadAllModels()
	if err != nil {
		klog.V(1).Infof("Warning: failed to load model configs: %v", err)
	}
	if models != nil {
		modelCfg, err = runpkg.FindModelConfig(models, modelID)
	}

	if modelCfg != nil && err == nil {
		// Model config found — use it
		modeCfg, err = runpkg.FindModeConfig(modelCfg, p.Mode)
		if err != nil {
			return nil, err
		}
	} else {
		// No model config found — fall back to flags
		if p.Engine == "" {
			return nil, fmt.Errorf("no model config found for %q and --engine not specified; "+
				"either add a model config or specify --engine to deploy without a pre-built model config", modelID)
		}
		fmt.Fprintf(os.Stderr, "Warning: no model config found for %q, proceeding with flag-only configuration\n", modelID)
		modelCfg = &runpkg.ModelConfig{ID: modelID, Name: modelID}
		modeCfg = &runpkg.ModeConfig{
			Name:   "custom",
			Engine: p.Engine,
		}
	}

	// Apply flag overrides (works for both config-based and flag-only paths)
	if err := applyFlagOverrides(modeCfg, p); err != nil {
		return nil, err
	}

	engineType := modeCfg.Engine
	engineCfg, err := resolveEngine(engineType, userCfg)
	if err != nil {
		return nil, err
	}
	enginePlugin, err := engineplugin.Get(engineCfg.Type, engineCfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize engine %q: %w", engineType, err)
	}

	return &modeConfigResult{
		modelCfg:     modelCfg,
		modeCfg:      modeCfg,
		enginePlugin: enginePlugin,
		engineType:   engineType,
	}, nil
}

// storageResult holds the result of storage resolution.
type storageResult struct {
	modelPath     string
	storagePlugin storageplugin.Plugin
	storageName   string
}

// resolveStorageAndModelPath resolves storage plugin and model path.
func resolveStorageAndModelPath(modelID string, p RunParams, userCfg *cliconfig.Config) *storageResult {
	var modelPath string
	var storagePlugin storageplugin.Plugin
	var storageName string

	// If the user explicitly specifies a model path, use it directly.
	if p.ModelPath != "" {
		modelPath = filepath.ToSlash(p.ModelPath)
	}

	if userCfg != nil {
		storageName = userCfg.CurrentStorage
		if p.Storage != "" {
			storageName = p.Storage
		}
		if storageName != "" {
			if storageCfg, err := userCfg.GetStorage(storageName); err == nil {
				if sp, err := storageplugin.Get(storageCfg.Type, storageCfg.Config); err == nil {
					storagePlugin = sp
					if modelPath == "" {
						modelPath = filepath.ToSlash(filepath.Join(storageplugin.DefaultMountPath, shared.SanitizeModelID(modelID), shared.SanitizeModelID(p.Revision)))
					}
				}
			}
		}
	}
	if modelPath == "" {
		modelPath = "/model/" + shared.SanitizeModelID(modelID) + "/" + shared.SanitizeModelID(p.Revision)
	}

	return &storageResult{
		modelPath:     modelPath,
		storagePlugin: storagePlugin,
		storageName:   storageName,
	}
}

// parseResources parses a slice of "key=value" strings into a corev1.ResourceList.
func parseResources(resourceFlags []string) (corev1.ResourceList, error) {
	result := corev1.ResourceList{}
	for _, r := range resourceFlags {
		parts := strings.SplitN(r, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid resource format: %q, expected key=value (e.g. nvidia.com/gpu=1)", r)
		}
		qty, err := resource.ParseQuantity(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid resource quantity %q for %q: %w", parts[1], parts[0], err)
		}
		result[corev1.ResourceName(parts[0])] = qty
	}
	return result, nil
}

// parseToleration parses a toleration string into a corev1.Toleration.
// Supported formats:
//   - "key=value:effect" → operator Equal
//   - "key:effect"       → operator Exists
//   - "key=value"        → operator Equal, matches all effects
//   - "key"              → operator Exists, matches all effects
func parseToleration(s string) (corev1.Toleration, error) {
	if s == "" {
		return corev1.Toleration{}, fmt.Errorf("empty toleration string")
	}

	var key, value, effect string
	var operator corev1.TolerationOperator

	if colonIdx := strings.LastIndex(s, ":"); colonIdx != -1 {
		effect = s[colonIdx+1:]
		prefix := s[:colonIdx]
		switch corev1.TaintEffect(effect) {
		case corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		default:
			return corev1.Toleration{}, fmt.Errorf("invalid toleration effect %q, must be NoSchedule, PreferNoSchedule, or NoExecute", effect)
		}
		if eqIdx := strings.Index(prefix, "="); eqIdx != -1 {
			key = prefix[:eqIdx]
			value = prefix[eqIdx+1:]
			operator = corev1.TolerationOpEqual
		} else {
			key = prefix
			operator = corev1.TolerationOpExists
		}
	} else {
		if eqIdx := strings.Index(s, "="); eqIdx != -1 {
			key = s[:eqIdx]
			value = s[eqIdx+1:]
			operator = corev1.TolerationOpEqual
		} else {
			key = s
			operator = corev1.TolerationOpExists
		}
	}

	if key == "" {
		return corev1.Toleration{}, fmt.Errorf("invalid toleration %q: key must not be empty", s)
	}

	return corev1.Toleration{
		Key:      key,
		Operator: operator,
		Value:    value,
		Effect:   corev1.TaintEffect(effect),
	}, nil
}

func parseTolerations(tolerationFlags []string) ([]corev1.Toleration, error) {
	var result []corev1.Toleration
	for _, t := range tolerationFlags {
		tol, err := parseToleration(t)
		if err != nil {
			return nil, err
		}
		result = append(result, tol)
	}
	return result, nil
}

// applyFlagOverrides applies flag values onto a ModeConfig.
// For Image, DistributedSize, ShmSize: flag value overrides config when non-zero.
// For Resources: flag values are merged by key (flag wins on conflict).
// For Env: flag values are appended. For Args: flag values are appended.
func applyFlagOverrides(modeCfg *runpkg.ModeConfig, p RunParams) error {
	if p.Engine != "" {
		modeCfg.Engine = p.Engine
	}
	if p.Image != "" {
		modeCfg.Image = p.Image
	}
	if p.DistributedSize > 0 {
		if modeCfg.Distributed == nil {
			modeCfg.Distributed = &runpkg.DistributedConfig{}
		}
		modeCfg.Distributed.Size = p.DistributedSize
	}
	if p.ShmSize != "" {
		modeCfg.ShmSize = p.ShmSize
	}
	if len(p.Resources) > 0 {
		flagRes, err := parseResources(p.Resources)
		if err != nil {
			return err
		}
		if modeCfg.Resources == nil {
			modeCfg.Resources = corev1.ResourceList{}
		}
		for k, v := range flagRes {
			modeCfg.Resources[k] = v
		}
	}
	for _, ev := range p.EnvVars {
		parts := strings.SplitN(ev, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid environment variable format: %q, expected KEY=VALUE", ev)
		}
		found := false
		for i, existing := range modeCfg.Env {
			if existing.Name == parts[0] {
				modeCfg.Env[i].Value = parts[1]
				found = true
				break
			}
		}
		if !found {
			modeCfg.Env = append(modeCfg.Env, corev1.EnvVar{Name: parts[0], Value: parts[1]})
		}
	}
	modeCfg.Args = append(modeCfg.Args, p.ArgsList...)
	tols, err := parseTolerations(p.Tolerations)
	if err != nil {
		return err
	}
	modeCfg.Tolerations = append(modeCfg.Tolerations, tols...)

	modeCfg.ImagePullSecrets = append(modeCfg.ImagePullSecrets, p.ImagePullSecrets...)
	// Validate the merged list (flag-provided + YAML-loaded entries) so that
	// empty names from either source are caught early with a clear error.
	if _, err := shared.ToImagePullSecrets(modeCfg.ImagePullSecrets); err != nil {
		return err
	}
	if p.HostNetwork {
		modeCfg.HostNetwork = true
	}
	if len(p.NodeSelector) > 0 {
		if modeCfg.NodeSelector == nil {
			modeCfg.NodeSelector = make(map[string]string)
		}
		for _, ns := range p.NodeSelector {
			parts := strings.SplitN(ns, "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				return fmt.Errorf("invalid node-selector format: %q, expected key=value (key must not be empty)", ns)
			}
			modeCfg.NodeSelector[parts[0]] = parts[1]
		}
	}
	return nil
}

// buildGenerateOptions builds GenerateOptions from mode config.
// All flag overrides should already be applied to modeCfg via applyFlagOverrides.
func buildGenerateOptions(name, modelID, modelPath string, modeCfg *runpkg.ModeConfig) engineplugin.GenerateOptions {
	distributedSize := int32(0)
	if modeCfg.Distributed != nil && modeCfg.Distributed.Size > 1 {
		distributedSize = modeCfg.Distributed.Size
	}

	var resources corev1.ResourceRequirements
	if len(modeCfg.Resources) > 0 {
		requests := corev1.ResourceList{}
		limits := corev1.ResourceList{}
		for k, v := range modeCfg.Resources {
			requests[k] = v
			limits[k] = v
		}
		resources.Requests = requests
		resources.Limits = limits
	}

	// applyFlagOverrides already validated the merged list; convert directly.
	imagePullSecrets, _ := shared.ToImagePullSecrets(modeCfg.ImagePullSecrets)

	return engineplugin.GenerateOptions{
		Name:             name,
		ModelID:          modelID,
		ModelPath:        modelPath,
		Image:            modeCfg.Image,
		Args:             modeCfg.Args,
		Env:              modeCfg.Env,
		Resources:        resources,
		DistributedSize:  distributedSize,
		ShmSize:          modeCfg.ShmSize,
		Tolerations:      modeCfg.Tolerations,
		ImagePullSecrets: imagePullSecrets,
		HostNetwork:      modeCfg.HostNetwork,
		NodeSelector:     modeCfg.NodeSelector,
	}
}

// assembleRBG assembles a RoleBasedGroup from pattern and metadata.
func assembleRBG(name, namespace string, pattern *workloadsv1alpha2.Pattern, metadata llmmeta.RunMetadata, replicas int32) *workloadsv1alpha2.RoleBasedGroup {
	podTemplate := getPodTemplateFromPattern(pattern)
	if podTemplate.Labels == nil {
		podTemplate.Labels = make(map[string]string)
	}
	podTemplate.Labels[llmmeta.RunCommandSourceLabelKey] = llmmeta.RunCommandSourceLabelValue

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		klog.V(1).Infof("failed to marshal run metadata: %v", err)
	}

	roleSpec := workloadsv1alpha2.RoleSpec{
		Name:     "inference",
		Replicas: &replicas,
		Pattern:  *pattern,
	}

	return &workloadsv1alpha2.RoleBasedGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workloads.x-k8s.io/v1alpha2",
			Kind:       "RoleBasedGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				llmmeta.RunCommandSourceLabelKey: llmmeta.RunCommandSourceLabelValue,
			},
			Annotations: map[string]string{
				llmmeta.RunCommandMetadataAnnotationKey: string(metadataJSON),
			},
		},
		Spec: workloadsv1alpha2.RoleBasedGroupSpec{
			Roles: []workloadsv1alpha2.RoleSpec{roleSpec},
		},
	}
}

// generateRBG generates a RoleBasedGroup.
// It performs: model config resolution -> pattern generation -> storage mounting -> RBG assembly.
// Returns the generated RBG and metadata.
func generateRBG(name, modelID, namespace string, p RunParams, userCfg *cliconfig.Config, cf *genericclioptions.ConfigFlags) (*workloadsv1alpha2.RoleBasedGroup, llmmeta.RunMetadata, error) {
	// 1. Resolve model/mode/engine config
	modeRes, err := resolveModeConfig(modelID, p, userCfg)
	if err != nil {
		return nil, llmmeta.RunMetadata{}, err
	}

	// 2. Resolve storage and model path
	storageRes := resolveStorageAndModelPath(modelID, p, userCfg)

	// 3. Build GenerateOptions
	opts := buildGenerateOptions(name, modelID, storageRes.modelPath, modeRes.modeCfg)

	// 4. Generate pattern
	pattern, err := modeRes.enginePlugin.GeneratePattern(opts)
	if err != nil {
		return nil, llmmeta.RunMetadata{}, fmt.Errorf("failed to generate engine pattern: %w", err)
	}
	podTemplate := getPodTemplateFromPattern(pattern)
	if podTemplate == nil || len(podTemplate.Spec.Containers) == 0 {
		return nil, llmmeta.RunMetadata{}, fmt.Errorf("engine %q generated pattern with no containers", modeRes.engineType)
	}

	// 5. Apply tolerations, imagePullSecrets, hostNetwork, and nodeSelector to pod template
	if len(opts.Tolerations) > 0 {
		podTemplate.Spec.Tolerations = append(podTemplate.Spec.Tolerations, opts.Tolerations...)
	}
	if len(opts.ImagePullSecrets) > 0 {
		podTemplate.Spec.ImagePullSecrets = append(podTemplate.Spec.ImagePullSecrets, opts.ImagePullSecrets...)
	}
	if opts.HostNetwork {
		podTemplate.Spec.HostNetwork = true
	}
	if len(opts.NodeSelector) > 0 {
		if podTemplate.Spec.NodeSelector == nil {
			podTemplate.Spec.NodeSelector = make(map[string]string)
		}
		for k, v := range opts.NodeSelector {
			podTemplate.Spec.NodeSelector[k] = v
		}
	}

	// 6. Mount storage
	if storageRes.storagePlugin != nil && storageRes.storageName != "" {
		mountOpts := storageplugin.MountOptions{
			StorageName: storageRes.storageName,
			Namespace:   namespace,
			DryRun:      p.DryRun,
			MountPath:   storageplugin.DefaultMountPath,
		}
		if !p.DryRun {
			c, err := util.GetControllerRuntimeClient(cf)
			if err != nil {
				return nil, llmmeta.RunMetadata{}, fmt.Errorf("failed to create controller client: %w", err)
			}
			mountOpts.Client = c
		}
		if err := storageRes.storagePlugin.MountStorage(podTemplate, mountOpts); err != nil {
			return nil, llmmeta.RunMetadata{}, fmt.Errorf("failed to mount storage: %w", err)
		}
	}

	// 7. Add model prefetch lifecycle hook
	if p.ModelPrefetch {
		container := &podTemplate.Spec.Containers[0]
		// Pass model path via environment variable to prevent shell injection.
		// The path is user-controlled (--model-path flag) and must not be
		// embedded directly into a shell command string.
		const prefetchPathEnv = "__MODEL_PREFETCH_PATH"
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  prefetchPathEnv,
			Value: storageRes.modelPath,
		})
		// Prefetch warms the OS page cache by reading all model files.
		// Design choices:
		//  - timeout 600: bounds the entire prefetch so PostStart cannot
		//    block indefinitely and trigger kubelet restart loops.
		//  - find -print0 | xargs -0: null-delimited for filenames with
		//    spaces, newlines, or special characters.
		//  - xargs -r: skip execution when find produces no output (empty
		//    directory) to avoid cat hanging on stdin.
		//  - No -n1: let xargs batch many files per cat invocation,
		//    dramatically reducing process-spawn overhead for large checkpoints.
		//  - -P 4: four parallel cat processes for I/O concurrency.
		//  - 2>/dev/null on find: suppress permission-denied noise.
		//  - >/dev/null 2>&1 on xargs: discard all cat output and errors.
		prefetchCmd := `if [ -d "$__MODEL_PREFETCH_PATH" ]; then timeout 600 sh -c 'find "$__MODEL_PREFETCH_PATH" -type f -print0 2>/dev/null | xargs -0 -r -P 4 cat >/dev/null 2>&1'; fi`
		if container.Lifecycle == nil {
			container.Lifecycle = &corev1.Lifecycle{}
		}
		container.Lifecycle.PostStart = &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", prefetchCmd},
			},
		}
	}

	// 8. Extract port and build metadata
	var resolvedPort int32
	for _, cp := range podTemplate.Spec.Containers[0].Ports {
		if cp.Name == "http" {
			resolvedPort = cp.ContainerPort
			break
		}
	}
	metadata := llmmeta.RunMetadata{
		ModelID:  modelID,
		Engine:   modeRes.engineType,
		Mode:     modeRes.modeCfg.Name,
		Revision: p.Revision,
		Port:     resolvedPort,
	}

	// 9. Assemble RBG
	rbg := assembleRBG(name, namespace, pattern, metadata, p.Replicas)

	return rbg, metadata, nil
}

// printGenerateSummary prints a human-readable summary of the generated RBG.
func printGenerateSummary(w io.Writer, rbg *workloadsv1alpha2.RoleBasedGroup, metadata llmmeta.RunMetadata) {
	_, _ = fmt.Fprintln(w, "# Generated RoleBasedGroup for Model Serving")
	_, _ = fmt.Fprintf(w, "# Name:      %s\n", rbg.Name)
	_, _ = fmt.Fprintf(w, "# Namespace: %s\n", rbg.GetNamespace())
	_, _ = fmt.Fprintf(w, "# Model:     %s\n", metadata.ModelID)
	_, _ = fmt.Fprintf(w, "# Revision:  %s\n", metadata.Revision)
	_, _ = fmt.Fprintf(w, "# Mode:      %s\n", metadata.Mode)
	_, _ = fmt.Fprintf(w, "# Engine:    %s\n", metadata.Engine)
	_, _ = fmt.Fprintln(w, "#")
}

// getPodTemplateFromPattern extracts the pod template from a Pattern
func getPodTemplateFromPattern(pattern *workloadsv1alpha2.Pattern) *corev1.PodTemplateSpec {
	if pattern == nil {
		return nil
	}
	if pattern.StandalonePattern != nil && pattern.StandalonePattern.Template != nil {
		return pattern.StandalonePattern.Template
	}
	if pattern.LeaderWorkerPattern != nil && pattern.LeaderWorkerPattern.Template != nil {
		return pattern.LeaderWorkerPattern.Template
	}
	return nil
}

// createRBG creates a v1alpha2 RoleBasedGroup in Kubernetes
func createRBG(ctx context.Context, rbg *workloadsv1alpha2.RoleBasedGroup, cf *genericclioptions.ConfigFlags) error {
	rbgClient, err := util.GetControllerRuntimeClient(cf)
	if err != nil {
		return fmt.Errorf("failed to create RBG client: %w", err)
	}

	if err := rbgClient.Create(ctx, rbg); err != nil {
		return fmt.Errorf("failed to create RoleBasedGroup: %w", err)
	}

	return nil
}

// waitForRBGReady waits for the RBG to be ready (Ready condition status is True)
func waitForRBGReady(ctx context.Context, name, namespace string, cf *genericclioptions.ConfigFlags, timeout time.Duration) error {
	rbgClient, err := util.GetControllerRuntimeClient(cf)
	if err != nil {
		return fmt.Errorf("failed to create RBG client: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Waiting for RoleBasedGroup '%s' to be ready...\n", name)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastMsg string
	err = wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		rbg := &workloadsv1alpha2.RoleBasedGroup{}
		if err := rbgClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, rbg); err != nil {
			return false, err
		}

		// Check if Ready condition is True
		for _, cond := range rbg.Status.Conditions {
			if cond.Type == string(workloadsv1alpha2.RoleBasedGroupReady) {
				if cond.Status == metav1.ConditionTrue {
					return true, nil
				}
				// Not ready yet, continue polling
				lastMsg = cond.Message
				return false, nil
			}
		}
		// Ready condition not found yet, continue polling
		return false, nil
	})

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout waiting for RoleBasedGroup '%s' to be ready: %s", name, lastMsg)
		}
		return fmt.Errorf("failed to wait for RoleBasedGroup ready: %w", err)
	}

	return nil
}

// findReadyPod finds a ready pod for the given RBG.
// For LeaderWorkerPattern (multi-node), only the leader (ComponentIndex=0) serves the API.
// For StandalonePattern (single-node), ComponentIndex is not set, so we fall back to any ready pod.
func findReadyPod(ctx context.Context, name, namespace string, cf *genericclioptions.ConfigFlags) (string, error) {
	k8sClient, err := util.GetK8SClientSet(cf)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// First try to find a leader pod (ComponentIndex=0) for multi-node deployments
	leaderSelector := labels.SelectorFromSet(labels.Set{
		constants.GroupNameLabelKey:      name,
		constants.RoleNameLabelKey:       "inference",
		constants.ComponentIndexLabelKey: "0",
	}).String()

	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: leaderSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	// If no leader pod found, fall back to any pod with the role (for single-node deployments)
	if len(pods.Items) == 0 {
		fallbackSelector := labels.SelectorFromSet(labels.Set{
			constants.GroupNameLabelKey: name,
			constants.RoleNameLabelKey:  "inference",
		}).String()
		pods, err = k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fallbackSelector,
		})
		if err != nil {
			return "", fmt.Errorf("failed to list pods: %w", err)
		}
	}

	for i := range pods.Items {
		p := &pods.Items[i]
		if isPodReady(p) {
			return p.Name, nil
		}
	}

	return "", fmt.Errorf("no ready pods found for service %q", name)
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// testChatCompletionsWithReconnect tests the API with automatic port-forward reconstruction on disconnect
// The session is automatically stopped when the function returns (success or error)
func testChatCompletionsWithReconnect(
	baseURL, modelName string,
	timeout time.Duration,
	pfSession *chat.PortForwardSession,
	reconnectFunc func() (*chat.PortForwardSession, error),
) error {
	reqBody := map[string]interface{}{
		"model":      modelName,
		"messages":   []map[string]string{{"role": "user", "content": "Hello"}},
		"stream":     false,
		"max_tokens": 10,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		pfSession.Stop()
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	requestTimeout := 30 * time.Second
	if timeout < requestTimeout {
		requestTimeout = timeout
	}

	client := &http.Client{Timeout: requestTimeout}
	endpoint := baseURL + "/v1/chat/completions"

	startTime := time.Now()
	attempt := 0
	reconnectAttempt := 0
	currentSession := pfSession

	for {
		attempt++
		remaining := timeout - time.Since(startTime)
		if remaining <= 0 {
			currentSession.Stop()
			return fmt.Errorf("timeout waiting for API to be ready after %s", timeout)
		}

		// Check if port-forward is still alive before making request
		if !currentSession.IsAlive() {
			reconnectAttempt++
			if reconnectAttempt%5 == 1 {
				fmt.Fprintf(os.Stderr, "  Port-forward disconnected, attempting to reconnect... (attempt %d, elapsed: %s)\n", reconnectAttempt, time.Since(startTime).Round(time.Second))
			}
			currentSession.Stop()

			newSession, err := reconnectFunc()
			if err != nil {
				return fmt.Errorf("failed to reconnect port-forward: %w", err)
			}
			currentSession = newSession
			if reconnectAttempt%5 == 1 {
				fmt.Fprintf(os.Stderr, "  Port-forward reconnected successfully\n")
			}
		}

		resp, err := client.Post(endpoint, "application/json", bytes.NewReader(data))
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			currentSession.Stop()
			return nil
		}

		var errMsg string
		if err != nil {
			errMsg = fmt.Sprintf("error: %v", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			errMsg = fmt.Sprintf("status: %d, body: %s", resp.StatusCode, string(body))
			_ = resp.Body.Close()
		}

		if attempt%5 == 0 {
			fmt.Fprintf(os.Stderr, "  API not ready yet (%s), retrying... (attempt %d, elapsed: %s)\n", errMsg, attempt, time.Since(startTime).Round(time.Second))
		}

		sleepDuration := 5 * time.Second
		if remaining < sleepDuration {
			sleepDuration = remaining
		}
		time.Sleep(sleepDuration)
	}
}

func newRunCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		replicas         int32
		mode             string
		engine           string
		image            string
		envVars          []string
		argsList         []string
		storage          string
		revision         string
		modelPath        string
		dryRun           bool
		waitReady        bool
		waitTimeout      time.Duration
		testAPI          bool
		testAPITimeout   time.Duration
		localPort        int32
		resources        []string
		distributedSize  int32
		shmSize          string
		modelPrefetch    bool
		tolerations      []string
		imagePullSecrets []string
		hostNetwork      bool
		nodeSelector     []string
	)

	cmd := &cobra.Command{
		Use:   "run <name> <model-id> [flags]",
		Short: "Run a model as an inference service",
		Long: `Deploy a model as an inference service on Kubernetes using RoleBasedGroup.

This command creates a RoleBasedGroup resource that deploys an LLM model for inference.
It supports various inference engines (vLLM, SGLang) and deployment modes optimized
for different use cases (latency, throughput, etc.).

The command will:
  1. Load the model configuration from the built-in models database (if available)
  2. Generate a pod template using the specified inference engine
  3. Create a RoleBasedGroup resource in the cluster

If no model configuration is found, you can still deploy by specifying --engine
(and optionally --image, --resource, etc.) to provide configuration via flags.

Prerequisites:
  - The model should be available in storage (use 'llmctl model pull' first)
  - Storage must be configured (use 'llmctl config add-storage')

Examples:
  # Quick start with default config
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B

  # Use a specific mode
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B --mode throughput

  # Override engine
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B --mode custom --engine sglang

  # Run with multiple replicas
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B --replicas 3

  # Deploy a custom model without any model config
  llmctl svc run my-model org/new-model --engine vllm --resource nvidia.com/gpu=1

  # Use private registry with image pull secrets
  kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --image-pull-secret my-registry-secret
  kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --image-pull-secret secret-a --image-pull-secret secret-b

  # Use host network
  kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --host-network

  # Schedule on specific nodes
  kubectl rbg llm svc run my-qwen Qwen/Qwen3.5-0.8B --node-selector gpu-type=a100 --node-selector zone=us-east-1a

  # Dry run to preview the generated configuration
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B --dry-run`,
		Example: `  # Quick start with default config
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B

  # Use a specific mode
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B --mode throughput

  # Override engine
  llmctl svc run my-qwen Qwen/Qwen3.5-0.8B --mode custom --engine sglang`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			modelID := args[1]
			namespace := util.GetNamespace(cf)

			// Load user config (best-effort — optional for engine and storage resolution)
			userCfg, cfgErr := cliconfig.Load()
			if cfgErr != nil {
				klog.V(1).Infof("Warning: failed to load user config: %v", cfgErr)
			}

			// Generate RBG (includes config resolution, pattern generation, storage mounting)
			params := RunParams{
				Mode:             mode,
				Engine:           engine,
				Image:            image,
				Storage:          storage,
				Revision:         revision,
				ModelPath:        modelPath,
				EnvVars:          envVars,
				ArgsList:         argsList,
				DryRun:           dryRun,
				Replicas:         replicas,
				Resources:        resources,
				DistributedSize:  distributedSize,
				ShmSize:          shmSize,
				ModelPrefetch:    modelPrefetch,
				Tolerations:      tolerations,
				ImagePullSecrets: imagePullSecrets,
				HostNetwork:      hostNetwork,
				NodeSelector:     nodeSelector,
			}
			rbg, metadata, err := generateRBG(name, modelID, namespace, params, userCfg, cf)
			if err != nil {
				return err
			}

			printGenerateSummary(os.Stdout, rbg, metadata)

			if dryRun {
				fmt.Println("# DRY RUN: No workload will be created")
				fmt.Println()
				return shared.PrintRBG(rbg)
			}

			// Create the RoleBasedGroup workload
			ctx := context.Background()
			if err := createRBG(ctx, rbg, cf); err != nil {
				klog.ErrorS(err, "Failed to create RoleBasedGroup")
				return err
			}

			fmt.Printf("✓ RoleBasedGroup '%s' created successfully in namespace '%s'\n", name, namespace)

			// Wait for RBG to be ready if requested
			if waitReady || testAPI {
				if err := waitForRBGReady(ctx, name, namespace, cf, waitTimeout); err != nil {
					return err
				}
				fmt.Printf("✓ RoleBasedGroup '%s' is ready\n", name)
			}

			// Test API if requested
			if testAPI {
				// Find a ready pod
				podName, err := findReadyPod(ctx, name, namespace, cf)
				if err != nil {
					return fmt.Errorf("failed to find ready pod: %w", err)
				}

				// Get kubeconfig path for port-forward
				kubeconfig := ""
				if cf.KubeConfig != nil {
					kubeconfig = *cf.KubeConfig
				}

				// Start port-forward
				fmt.Fprintf(os.Stderr, "Testing API endpoint...\n")
				pfSession, err := chat.StartPortForward(kubeconfig, namespace, podName, localPort, metadata.Port, 30*time.Second)
				if err != nil {
					return fmt.Errorf("failed to start port-forward: %w", err)
				}

				// Test the API with automatic port-forward reconstruction
				baseURL := fmt.Sprintf("http://localhost:%d", localPort)
				if err := testChatCompletionsWithReconnect(baseURL, name, testAPITimeout, pfSession, func() (*chat.PortForwardSession, error) {
					// Reconnect function: find new ready pod and restart port-forward
					newPodName, err := findReadyPod(ctx, name, namespace, cf)
					if err != nil {
						return nil, err
					}
					return chat.StartPortForward(kubeconfig, namespace, newPodName, localPort, metadata.Port, 30*time.Second)
				}); err != nil {
					return fmt.Errorf("API test failed: %w", err)
				}
				fmt.Printf("✓ API endpoint is ready (/v1/chat/completions)\n")
			}

			return nil
		},
	}

	cmd.Flags().Int32Var(&replicas, "replicas", 1, "Number of replicas")
	cmd.Flags().StringVar(&mode, "mode", "", "Run mode (default: first mode in model config)")
	cmd.Flags().StringVar(&engine, "engine", "", "Inference engine override: vllm, sglang (default: from mode config)")
	cmd.Flags().StringVar(&image, "image", "", "Container image override (default: from mode config)")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "Environment variables (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&argsList, "arg", nil, "Additional arguments for the engine")
	cmd.Flags().StringArrayVar(&resources, "resource", nil, "Resource requirements (key=value, e.g. nvidia.com/gpu=1)")
	cmd.Flags().Int32Var(&distributedSize, "distributed-size", 0, "Multi-node deployment size (<=1 means standalone)")
	cmd.Flags().StringVar(&shmSize, "shm-size", "", "Shared memory size (e.g. 8Gi, 16Gi)")
	cmd.Flags().StringVar(&storage, "storage", "", "Storage to use (overrides default)")
	cmd.Flags().StringVar(&revision, "revision", "main", "Model revision")
	cmd.Flags().StringVar(&modelPath, "model-path", "", "Absolute model path inside the container. Storage is mounted at /models, so the default path is /models/<model>/<revision>")
	cmd.Flags().BoolVar(&modelPrefetch, "model-prefetch", false, "Add a postStart lifecycle hook to prefetch model files into page cache. "+
		"The hook runs synchronously (pod stays in ContainerCreating until it finishes). "+
		"A 600s soft timeout is enforced to avoid kubelet hook-deadline restarts. "+
		"Best suited for models under ~100 GB on fast storage; larger checkpoints may exceed the timeout")
	cmd.Flags().StringArrayVar(&tolerations, "toleration", nil, "Pod tolerations (key=value:effect, key:effect, or key)")
	cmd.Flags().StringArrayVar(&imagePullSecrets, "image-pull-secret", nil, "Image pull secret names for private registries (can be specified multiple times)")
	cmd.Flags().BoolVar(&hostNetwork, "host-network", false, "Use host network for the pod")
	cmd.Flags().StringArrayVar(&nodeSelector, "node-selector", nil, "Node selector labels (key=value, can be specified multiple times)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the generated template without creating the workload")
	cmd.Flags().BoolVar(&waitReady, "wait", true, "Wait for the RoleBasedGroup to be ready before returning")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 20*time.Minute, "Timeout for waiting for RoleBasedGroup to be ready")
	cmd.Flags().BoolVar(&testAPI, "test-api", true, "Test the /v1/chat/completions API endpoint after the service is ready")
	cmd.Flags().DurationVar(&testAPITimeout, "test-api-timeout", 5*time.Minute, "Timeout for testing the API endpoint")
	cmd.Flags().Int32Var(&localPort, "local-port", 32432, "Local port for port-forward when testing API")

	return cmd
}
