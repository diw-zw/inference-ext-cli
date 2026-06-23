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

package engine

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/plugin/util"
)

func init() {
	Register("vllm", func() Plugin {
		return &VLLMEngine{}
	})
}

// VLLMEngine implements the EnginePlugin interface for vLLM
type VLLMEngine struct {
	Image string
	Port  int32
}

// Name returns the plugin name
func (v *VLLMEngine) Name() string {
	return "vllm"
}

// ConfigFields returns the config fields this plugin accepts
func (v *VLLMEngine) ConfigFields() []util.ConfigField {
	return []util.ConfigField{
		{Key: "image", Description: "vLLM container image (default: vllm/vllm-openai:latest)", Required: false},
		{Key: "port", Description: "port the server listens on (default: 8000)", Required: false},
	}
}

// Init initializes the plugin with config
func (v *VLLMEngine) Init(config map[string]interface{}) error {
	if image, ok := config["image"].(string); ok {
		v.Image = image
	} else {
		v.Image = "vllm/vllm-openai:latest"
	}
	if port, ok := config["port"].(int); ok {
		v.Port = int32(port)
	} else {
		v.Port = 8000
	}
	return nil
}

// GeneratePattern generates a Pattern for running vLLM.
// For multi-node deployment, vLLM requires --headless flag for worker nodes.
func (v *VLLMEngine) GeneratePattern(opts GenerateOptions) (*workloadsv1alpha2.Pattern, error) {
	podTemplate, err := v.generatePodTemplate(opts)
	if err != nil {
		return nil, err
	}

	if opts.DistributedSize > 1 {
		// Multi-node deployment using LeaderWorkerPattern
		// vLLM requires --headless for worker nodes
		// WorkerTemplatePatch must contain complete args because Strategic Merge Patch
		// replaces args entirely rather than appending.
		workerPatch, err := v.generateWorkerPatch(opts)
		if err != nil {
			return nil, fmt.Errorf("failed to generate worker patch: %w", err)
		}

		return &workloadsv1alpha2.Pattern{
			LeaderWorkerPattern: &workloadsv1alpha2.LeaderWorkerPattern{
				Size: &opts.DistributedSize,
				TemplateSource: workloadsv1alpha2.TemplateSource{
					Template: podTemplate,
				},
				WorkerTemplatePatch: workerPatch,
			},
		}, nil
	}

	// Single-node deployment using StandalonePattern
	return &workloadsv1alpha2.Pattern{
		StandalonePattern: &workloadsv1alpha2.StandalonePattern{
			TemplateSource: workloadsv1alpha2.TemplateSource{
				Template: podTemplate,
			},
		},
	}, nil
}

// generateWorkerPatch creates a patch for worker nodes with complete args.
// Note: Strategic Merge Patch replaces args entirely, so we must include all args.
func (v *VLLMEngine) generateWorkerPatch(opts GenerateOptions) (*runtime.RawExtension, error) {
	// Generate complete worker args (same as leader but with --headless appended)
	workerArgs := v.generateArgs(opts, true)

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []map[string]interface{}{
				{
					"name": "vllm",
					"args": workerArgs,
				},
			},
		},
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}

	return &runtime.RawExtension{
		Raw: patchJSON,
	}, nil
}

// generateArgs generates the args for vLLM container.
// If isWorker is true, --headless flag is appended for worker nodes.
func (v *VLLMEngine) generateArgs(opts GenerateOptions, isWorker bool) []string {
	args := []string{
		"--model",
		opts.ModelPath,
		"--served-model-name",
		opts.Name,
		"--host",
		"0.0.0.0",
		"--port",
		fmt.Sprintf("%d", v.Port),
	}

	// Add distributed deployment args for multi-node setup
	if opts.DistributedSize > 1 {
		args = append(args,
			"--nnodes", "$(RBG_LWP_GROUP_SIZE)",
			"--node-rank", "$(RBG_LWP_WORKER_INDEX)",
			"--master-addr", "$(RBG_LWP_LEADER_ADDRESS)",
		)
	}

	// Add user-provided args
	args = append(args, opts.Args...)

	// Worker nodes need --headless flag
	if isWorker {
		args = append(args, "--headless")
	}

	return args
}

// generatePodTemplate generates the base PodTemplateSpec for vLLM.
// The base template is used for leader nodes in multi-node deployment.
func (v *VLLMEngine) generatePodTemplate(opts GenerateOptions) (*corev1.PodTemplateSpec, error) {
	// Use override image if provided, otherwise use default
	image := v.Image
	if opts.Image != "" {
		image = opts.Image
	}

	// Build args for leader/base template (isWorker=false)
	args := v.generateArgs(opts, false)

	podSpec := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "vllm",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            args,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: v.Port,
						},
					},
					Env:       opts.Env,
					Resources: opts.Resources,
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromInt32(v.Port),
							},
						},
						InitialDelaySeconds: ReadinessProbeInitialDelaySeconds,
						PeriodSeconds:       ReadinessProbePeriodSeconds,
						FailureThreshold:    ReadinessProbeFailureThreshold,
					},
				},
			},
		},
	}

	// Add shared memory volume if ShmSize is specified
	if opts.ShmSize != "" {
		shmQuantity, err := resource.ParseQuantity(opts.ShmSize)
		if err != nil {
			return nil, fmt.Errorf("invalid shmSize %q: %w", opts.ShmSize, err)
		}

		// Add volume to pod
		podSpec.Spec.Volumes = append(podSpec.Spec.Volumes, corev1.Volume{
			Name: "shm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: &shmQuantity,
				},
			},
		})

		// Add volume mount to container
		podSpec.Spec.Containers[0].VolumeMounts = append(podSpec.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "shm",
			MountPath: "/dev/shm",
		})
	}

	return podSpec, nil
}
