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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/plugin/util"
)

func init() {
	Register("sglang", func() Plugin {
		return &SGLangEngine{}
	})
}

// SGLangEngine implements the EnginePlugin interface for SGLang
type SGLangEngine struct {
	Image string
	Port  int32
}

// Name returns the plugin name
func (s *SGLangEngine) Name() string {
	return "sglang"
}

// ConfigFields returns the config fields this plugin accepts
func (s *SGLangEngine) ConfigFields() []util.ConfigField {
	return []util.ConfigField{
		{Key: "image", Description: "SGLang container image (default: lmsysorg/sglang:latest)", Required: false},
		{Key: "port", Description: "port the server listens on (default: 30000)", Required: false},
	}
}

// Init initializes the plugin with config
func (s *SGLangEngine) Init(config map[string]interface{}) error {
	if image, ok := config["image"].(string); ok {
		s.Image = image
	} else {
		s.Image = "lmsysorg/sglang:latest"
	}
	if port, ok := config["port"].(int); ok {
		s.Port = int32(port)
	} else {
		s.Port = 30000
	}
	return nil
}

// GeneratePattern generates a Pattern for running SGLang.
// For multi-node deployment, SGLang uses the same template for both leader and worker,
// with --node-rank distinguishing their roles at runtime.
func (s *SGLangEngine) GeneratePattern(opts GenerateOptions) (*workloadsv1alpha2.Pattern, error) {
	podTemplate, err := s.generatePodTemplate(opts)
	if err != nil {
		return nil, err
	}

	if opts.DistributedSize > 1 {
		// Multi-node deployment using LeaderWorkerPattern
		return &workloadsv1alpha2.Pattern{
			LeaderWorkerPattern: &workloadsv1alpha2.LeaderWorkerPattern{
				Size: &opts.DistributedSize,
				TemplateSource: workloadsv1alpha2.TemplateSource{
					Template: podTemplate,
				},
				// SGLang doesn't need LeaderTemplatePatch/WorkerTemplatePatch
				// because leader and worker use the same startup args,
				// with --node-rank=$(RBG_LWP_WORKER_INDEX) distinguishing roles at runtime.
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

// generatePodTemplate generates the base PodTemplateSpec for SGLang
func (s *SGLangEngine) generatePodTemplate(opts GenerateOptions) (*corev1.PodTemplateSpec, error) {
	// Use override image if provided, otherwise use default
	image := s.Image
	if opts.Image != "" {
		image = opts.Image
	}

	// Build base args
	args := []string{
		"--model-path",
		opts.ModelPath,
		"--served-model-name",
		opts.Name,
		"--host",
		"0.0.0.0",
		"--port",
		fmt.Sprintf("%d", s.Port),
	}

	// Add distributed deployment args for multi-node setup
	if opts.DistributedSize > 1 {
		args = append(args,
			"--dist-init-addr=$(RBG_LWP_LEADER_ADDRESS):6379",
			"--nnodes=$(RBG_LWP_GROUP_SIZE)",
			"--node-rank=$(RBG_LWP_WORKER_INDEX)",
		)
	}

	// Add user-provided args
	args = append(args, opts.Args...)

	// Build env vars
	env := []corev1.EnvVar{
		{
			Name:  "SGLANG_MODEL_PATH",
			Value: opts.ModelPath,
		},
	}
	env = append(env, opts.Env...)

	podSpec := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "sglang",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"python", "-m", "sglang.launch_server"},
					Args:            args,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: s.Port,
						},
					},
					Env:       env,
					Resources: opts.Resources,
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromInt32(s.Port),
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
