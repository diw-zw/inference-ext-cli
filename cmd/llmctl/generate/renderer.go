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

package generate

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
)

// RenderDeploymentYAML generates RBG deployment YAML from generator config
func RenderDeploymentYAML(plan *DeploymentPlan) error {
	var yamlContent string
	var err error

	switch plan.Mode {
	case "disagg":
		yamlContent, err = renderDisaggYAML(plan)
	case "agg":
		yamlContent, err = renderAggYAML(plan)
	default:
		return fmt.Errorf("unknown deployment mode: %s", plan.Mode)
	}

	if err != nil {
		return fmt.Errorf("failed to render %s YAML: %w", plan.Mode, err)
	}

	// Write YAML to file
	if err := os.WriteFile(plan.OutputPath, []byte(yamlContent), 0644); err != nil {
		return fmt.Errorf("failed to write YAML to %s: %w", plan.OutputPath, err)
	}

	klog.V(2).Infof("Successfully generated %s deployment YAML: %s", plan.Mode, plan.OutputPath)
	return nil
}

// renderDisaggYAML generates YAML for Prefill-Decode disaggregated mode
func renderDisaggYAML(plan *DeploymentPlan) (string, error) {
	config := plan.Config
	prefillParams := GetWorkerParams(config.Params.Prefill)
	decodeParams := GetWorkerParams(config.Params.Decode)

	// Get base name for the deployment
	baseName := getDeployName(plan.ModelName, plan.BackendName, "pd")
	modelPath := getModelPath(plan.ModelName)
	image := getImage(plan.BackendName)

	// Build RoleBasedGroup using native API types
	rbg := &workloadsv1alpha2.RoleBasedGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: workloadsv1alpha2.GroupVersion.String(),
			Kind:       "RoleBasedGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      baseName,
			Namespace: "default",
		},
		Spec: workloadsv1alpha2.RoleBasedGroupSpec{
			Roles: []workloadsv1alpha2.RoleSpec{
				buildRouterRoleSpec(baseName, image, modelPath, plan.BackendName, plan),
				buildPrefillRoleSpec(image, modelPath, plan.BackendName, config.Workers.PrefillWorkers, prefillParams, plan),
				buildDecodeRoleSpec(image, modelPath, plan.BackendName, config.Workers.DecodeWorkers, decodeParams, plan),
			},
		},
	}

	// Build Service
	service := buildServiceSpec(baseName, "router")

	// Combine RBG and Service
	return marshalMultiDocYAML(rbg, service)
}

// renderAggYAML generates YAML for aggregated mode
func renderAggYAML(plan *DeploymentPlan) (string, error) {
	config := plan.Config
	aggParams := GetWorkerParams(config.Params.Agg)

	baseName := getDeployName(plan.ModelName, plan.BackendName, "agg")
	modelPath := getModelPath(plan.ModelName)
	image := getImage(plan.BackendName)

	// Build RoleBasedGroup using native API types
	rbg := &workloadsv1alpha2.RoleBasedGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: workloadsv1alpha2.GroupVersion.String(),
			Kind:       "RoleBasedGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      baseName,
			Namespace: "default",
		},
		Spec: workloadsv1alpha2.RoleBasedGroupSpec{
			Roles: []workloadsv1alpha2.RoleSpec{
				buildWorkerRoleSpec(image, modelPath, plan.BackendName, config.Workers.AggWorkers, aggParams, plan),
			},
		},
	}

	// Build Service
	service := buildServiceSpec(baseName, "worker")

	return marshalMultiDocYAML(rbg, service)
}

// buildRouterRoleSpec creates the router role spec using native API types
func buildRouterRoleSpec(baseName, image, modelPath, backend string, plan *DeploymentPlan) workloadsv1alpha2.RoleSpec {
	if backend != BackendSGLang {
		klog.Fatalf("Router role configuration for backend %s not implemented", backend)
	}

	// Build command with dynamic prefill and decode endpoints
	command := []string{
		"python3",
		"-m",
		"sglang_router.launch_router",
		"--pd-disaggregation",
	}

	// Add all prefill worker endpoints
	prefillReplicas := plan.Config.Workers.PrefillWorkers
	for i := 0; i < prefillReplicas; i++ {
		command = append(command, "--prefill")
		command = append(command, fmt.Sprintf("http://%s-prefill-%d.s-%s-prefill:8000", baseName, i, baseName))
	}

	// Add all decode worker endpoints
	decodeReplicas := plan.Config.Workers.DecodeWorkers
	for i := 0; i < decodeReplicas; i++ {
		command = append(command, "--decode")
		command = append(command, fmt.Sprintf("http://%s-decode-%d.s-%s-decode:8000", baseName, i, baseName))
	}

	// Add common parameters
	command = append(command,
		"--host",
		"0.0.0.0",
		"--port",
		"8000",
	)

	podTemplate := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "model",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: normalizeModelName(plan.ModelName),
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "schedule",
					Image:           image,
					ImagePullPolicy: corev1.PullAlways,
					Command:         command,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "model",
							MountPath: modelPath,
						},
					},
				},
			},
		},
	}

	return workloadsv1alpha2.RoleSpec{
		Name:         "router",
		Dependencies: []string{"prefill", "decode"},
		Replicas:     ptr.To[int32](1),
		Pattern: workloadsv1alpha2.Pattern{
			StandalonePattern: &workloadsv1alpha2.StandalonePattern{
				TemplateSource: workloadsv1alpha2.TemplateSource{
					Template: &podTemplate,
				},
			},
		},
	}
}

// buildPrefillRoleSpec creates the prefill role spec using native API types
func buildPrefillRoleSpec(image, modelPath, backend string, replicas int, params WorkerParams, plan *DeploymentPlan) workloadsv1alpha2.RoleSpec {
	command := buildPrefillCommand(backend, modelPath, params)
	containerName := fmt.Sprintf("%s-prefill", backend)
	podTemplate := buildWorkerPodTemplate(image, modelPath, containerName, command, params.TensorParallelSize, plan, true)

	return workloadsv1alpha2.RoleSpec{
		Name:     "prefill",
		Replicas: ptr.To(int32(replicas)),
		Pattern: workloadsv1alpha2.Pattern{
			StandalonePattern: &workloadsv1alpha2.StandalonePattern{
				TemplateSource: workloadsv1alpha2.TemplateSource{
					Template: &podTemplate,
				},
			},
		},
	}
}

// buildDecodeRoleSpec creates the decode role spec using native API types
func buildDecodeRoleSpec(image, modelPath, backend string, replicas int, params WorkerParams, plan *DeploymentPlan) workloadsv1alpha2.RoleSpec {
	command := buildDecodeCommand(backend, modelPath, params)
	containerName := fmt.Sprintf("%s-decode", backend)
	podTemplate := buildWorkerPodTemplate(image, modelPath, containerName, command, params.TensorParallelSize, plan, true)

	return workloadsv1alpha2.RoleSpec{
		Name:     "decode",
		Replicas: ptr.To(int32(replicas)),
		Pattern: workloadsv1alpha2.Pattern{
			StandalonePattern: &workloadsv1alpha2.StandalonePattern{
				TemplateSource: workloadsv1alpha2.TemplateSource{
					Template: &podTemplate,
				},
			},
		},
	}
}

// buildWorkerRoleSpec creates the worker role spec for aggregated mode using native API types
func buildWorkerRoleSpec(image, modelPath, backend string, replicas int, params WorkerParams, plan *DeploymentPlan) workloadsv1alpha2.RoleSpec {
	command := buildAggCommand(backend, modelPath, params)
	containerName := fmt.Sprintf("%s-worker", backend)
	podTemplate := buildWorkerPodTemplate(image, modelPath, containerName, command, params.TensorParallelSize, plan, false)

	return workloadsv1alpha2.RoleSpec{
		Name:     "worker",
		Replicas: ptr.To(int32(replicas)),
		Pattern: workloadsv1alpha2.Pattern{
			StandalonePattern: &workloadsv1alpha2.StandalonePattern{
				TemplateSource: workloadsv1alpha2.TemplateSource{
					Template: &podTemplate,
				},
			},
		},
	}
}

// buildWorkerPodTemplate creates a common pod template for worker roles
// withShmSizeLimit controls whether to set a size limit (30Gi) on the shm volume
func buildWorkerPodTemplate(image, modelPath, containerName string, command []string, tensorParallelSize int, plan *DeploymentPlan, withShmSizeLimit bool) corev1.PodTemplateSpec {
	gpuQuantity := resource.MustParse(fmt.Sprintf("%d", tensorParallelSize))

	container := corev1.Container{
		Name:            containerName,
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		Env:             []corev1.EnvVar{buildPodIPEnvVar()},
		Command:         command,
		Ports:           []corev1.ContainerPort{buildHTTPContainerPort()},
		ReadinessProbe:  buildTCPReadinessProbe(),
		Resources:       buildGPUResourceRequirements(gpuQuantity),
		VolumeMounts: []corev1.VolumeMount{
			buildModelVolumeMount(modelPath),
			buildShmVolumeMount(),
		},
	}

	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				buildModelVolume(plan.ModelName),
				buildShmVolume(withShmSizeLimit),
			},
			Containers: []corev1.Container{container},
		},
	}
}

// buildModelVolume creates a PVC-backed volume for model storage
func buildModelVolume(modelName string) corev1.Volume {
	return corev1.Volume{
		Name: "model",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: normalizeModelName(modelName),
			},
		},
	}
}

// buildShmVolume creates a shared memory volume
// withSizeLimit controls whether to set a 30Gi size limit
func buildShmVolume(withSizeLimit bool) corev1.Volume {
	vol := corev1.Volume{
		Name: "shm",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory,
			},
		},
	}

	if withSizeLimit {
		shmSize := resource.MustParse("30Gi")
		vol.EmptyDir.SizeLimit = &shmSize
	}

	return vol
}

// buildPodIPEnvVar creates the POD_IP environment variable
func buildPodIPEnvVar() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "POD_IP",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "status.podIP",
			},
		},
	}
}

// buildHTTPContainerPort creates the HTTP container port (8000)
func buildHTTPContainerPort() corev1.ContainerPort {
	return corev1.ContainerPort{
		ContainerPort: 8000,
		Name:          "http",
	}
}

// buildTCPReadinessProbe creates a TCP readiness probe for port 8000
func buildTCPReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(8000),
			},
		},
	}
}

// buildGPUResourceRequirements creates GPU resource requirements
func buildGPUResourceRequirements(gpuQuantity resource.Quantity) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			"nvidia.com/gpu": gpuQuantity,
		},
		Requests: corev1.ResourceList{
			"nvidia.com/gpu": gpuQuantity,
		},
	}
}

// buildModelVolumeMount creates a volume mount for the model
func buildModelVolumeMount(modelPath string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "model",
		MountPath: modelPath,
	}
}

// buildShmVolumeMount creates a volume mount for shared memory
func buildShmVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "shm",
		MountPath: "/dev/shm",
	}
}

// buildServiceSpec creates a Kubernetes Service resource using native API types
func buildServiceSpec(baseName, targetRole string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      baseName,
			Namespace: "default",
			Labels: map[string]string{
				"app": baseName,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8000,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(8000),
				},
			},
			Selector: map[string]string{
				"rolebasedgroup.workloads.x-k8s.io/name": baseName,
				"rolebasedgroup.workloads.x-k8s.io/role": targetRole,
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// buildPrefillCommand constructs the prefill worker command
func buildPrefillCommand(backend, modelPath string, params WorkerParams) []string {
	return buildSGLangCommand(backend, modelPath, params, "prefill")
}

// buildDecodeCommand constructs the decode worker command
func buildDecodeCommand(backend, modelPath string, params WorkerParams) []string {
	return buildSGLangCommand(backend, modelPath, params, "decode")
}

// buildAggCommand constructs the aggregated mode worker command
func buildAggCommand(backend, modelPath string, params WorkerParams) []string {
	return buildSGLangCommand(backend, modelPath, params, "")
}

// buildSGLangCommand builds a SGLang server command with optional disaggregation mode
// disaggMode can be "prefill", "decode", or "" for aggregated mode
func buildSGLangCommand(backend, modelPath string, params WorkerParams, disaggMode string) []string {
	if backend != BackendSGLang {
		return []string{"echo", fmt.Sprintf("Backend %s not yet supported", backend)}
	}

	// Build base command arguments
	args := []string{
		"-m",
		"sglang.launch_server",
		"--model-path",
		modelPath,
		"--enable-metrics",
	}

	// Add disaggregation mode if specified (for prefill/decode)
	if disaggMode != "" {
		args = append(args, "--disaggregation-mode", disaggMode)
	}

	// Add common server parameters
	args = append(args,
		"--port",
		"8000",
		"--host",
		"$(POD_IP)",
	)

	// Add parallelization parameters
	args = appendParallelizationParams(args, params)

	return append([]string{"python3"}, args...)
}

// appendParallelizationParams adds parallelization parameters to the command args
func appendParallelizationParams(args []string, params WorkerParams) []string {
	// Add tensor-parallel-size
	if params.TensorParallelSize > 0 {
		args = append(args, "--tensor-parallel-size", fmt.Sprintf("%d", params.TensorParallelSize))
	}

	// Add pipeline-parallel-size
	if params.PipelineParallelSize > 0 {
		args = append(args, "--pipeline-parallel-size", fmt.Sprintf("%d", params.PipelineParallelSize))
	}

	// Add data-parallel-size
	if params.DataParallelSize > 0 {
		args = append(args, "--data-parallel-size", fmt.Sprintf("%d", params.DataParallelSize))
	}

	// Add expert-parallel-size
	if params.MoEExpertParallelSize > 0 {
		args = append(args, "--expert-parallel-size", fmt.Sprintf("%d", params.MoEExpertParallelSize))
	}

	// Add moe-dense-tp-size
	if params.MoETensorParallelSize > 0 {
		args = append(args, "--moe-dense-tp-size", fmt.Sprintf("%d", params.MoETensorParallelSize))
	}

	return args
}

// getDeployName generates a deploy name with a random suffix to avoid conflicts
// The suffix is a 5-character lowercase hex string that complies with DNS naming rules
func getDeployName(modelName, backend, suffix string) string {
	// Generate a random 5-character suffix (DNS-safe: lowercase letters and numbers)
	randomSuffix := generateRandomSuffix(5)
	return fmt.Sprintf("%s-%s-%s-%s", normalizeModelName(modelName), backend, suffix, randomSuffix)
}

// generateRandomSuffix generates a random lowercase hex string of specified length
// Uses timestamp as seed to ensure uniqueness across different runs
func generateRandomSuffix(length int) string {
	// Use current timestamp (nanoseconds) as seed for randomness
	source := rand.NewSource(time.Now().UnixNano())
	rng := rand.New(source)

	// Calculate how many random bytes we need (2 hex chars per byte)
	bytes := make([]byte, (length+1)/2)
	for i := range bytes {
		bytes[i] = byte(rng.Intn(256))
	}

	hexString := hex.EncodeToString(bytes)
	return hexString[:length]
}

// getModelPath determines the model path based on HuggingFace ID or model name
// Returns /models/{subpath}/ where subpath is the last valid segment after path resolution
func getModelPath(modelName string) string {
	// Return the last segment in the resolved path
	return fmt.Sprintf("/models/%s/", normalizeModelName(modelName))
}

// getImage selects the appropriate container image
func getImage(backend string) string {
	// Default images per backend
	switch backend {
	case BackendSGLang:
		return "lmsysorg/sglang:latest"
	case BackendVLLM:
		return "vllm/vllm-openai:latest"
	case BackendTRTLLM:
		return "nvcr.io/nvidia/ai-dynamo/tensorrtllm-runtime:latest"
	default:
		return "lmsysorg/sglang:latest"
	}
}

// marshalMultiDocYAML marshals multiple documents into a YAML string
// Handles both regular Kubernetes objects and ApplyConfiguration objects
func marshalMultiDocYAML(docs ...interface{}) (string, error) {
	var result strings.Builder

	for i, doc := range docs {
		if i > 0 {
			result.WriteString("---\n")
		}

		// Convert ApplyConfiguration to unstructured format
		unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(doc)
		if err != nil {
			return "", fmt.Errorf("failed to convert document %d to unstructured: %w", i, err)
		}

		// Marshal to YAML using yaml.v2
		yamlBytes, err := yaml.Marshal(unstructuredObj)
		if err != nil {
			return "", fmt.Errorf("failed to marshal document %d: %w", i, err)
		}

		result.Write(yamlBytes)
	}

	return result.String(), nil
}
