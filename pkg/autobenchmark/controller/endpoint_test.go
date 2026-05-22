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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
)

func standaloneRole(podSpec corev1.PodSpec) *v1alpha2.RoleSpec {
	return &v1alpha2.RoleSpec{
		Pattern: v1alpha2.Pattern{
			StandalonePattern: &v1alpha2.StandalonePattern{
				TemplateSource: v1alpha2.TemplateSource{
					Template: &corev1.PodTemplateSpec{Spec: podSpec},
				},
			},
		},
	}
}

func leaderWorkerRole(podSpec corev1.PodSpec) *v1alpha2.RoleSpec {
	return &v1alpha2.RoleSpec{
		Pattern: v1alpha2.Pattern{
			LeaderWorkerPattern: &v1alpha2.LeaderWorkerPattern{
				TemplateSource: v1alpha2.TemplateSource{
					Template: &corev1.PodTemplateSpec{Spec: podSpec},
				},
			},
		},
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal string", "my-experiment", "my-experiment"},
		{"with dots and underscores", "exp.v1_test", "exp.v1_test"},
		{"truncate long string", strings.Repeat("a", 100), strings.Repeat("a", 63)},
		{"special chars replaced", "exp name@v1!", "exp-name-v1"},
		{"empty after sanitize", "@@@@", "default"},
		{"leading and trailing dashes trimmed", "-my-exp-", "my-exp"},
		{"all dashes becomes default", "---", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeLabelValue(tt.input))
		})
	}
}

func TestDefaultEnginePort(t *testing.T) {
	tests := []struct {
		backend string
		want    int
	}{
		{"sglang", 30000},
		{"vllm", 8000},
		{"unknown", 8000},
	}

	for _, tt := range tests {
		t.Run(tt.backend, func(t *testing.T) {
			assert.Equal(t, tt.want, defaultEnginePort(tt.backend))
		})
	}
}

func TestGetRolePodSpec(t *testing.T) {
	dummyPodSpec := corev1.PodSpec{
		Containers: []corev1.Container{{Name: "main"}},
	}

	tests := []struct {
		name    string
		role    *v1alpha2.RoleSpec
		wantNil bool
	}{
		{
			name:    "standalone pattern",
			role:    standaloneRole(dummyPodSpec),
			wantNil: false,
		},
		{
			name:    "leader-worker pattern",
			role:    leaderWorkerRole(dummyPodSpec),
			wantNil: false,
		},
		{
			name:    "nil patterns",
			role:    &v1alpha2.RoleSpec{},
			wantNil: true,
		},
		{
			name: "standalone with nil template",
			role: &v1alpha2.RoleSpec{
				Pattern: v1alpha2.Pattern{
					StandalonePattern: &v1alpha2.StandalonePattern{},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := getRolePodSpec(tt.role)
			if tt.wantNil {
				assert.Nil(t, ps)
			} else {
				require.NotNil(t, ps)
				assert.Equal(t, "main", ps.Containers[0].Name)
			}
		})
	}
}

func TestIsRBGReady(t *testing.T) {
	tests := []struct {
		name string
		rbg  *v1alpha2.RoleBasedGroup
		want bool
	}{
		{
			name: "Ready=True",
			rbg: &v1alpha2.RoleBasedGroup{
				Status: v1alpha2.RoleBasedGroupStatus{
					Conditions: []metav1.Condition{
						{Type: string(v1alpha2.RoleBasedGroupReady), Status: "True"},
					},
				},
			},
			want: true,
		},
		{
			name: "Ready=False",
			rbg: &v1alpha2.RoleBasedGroup{
				Status: v1alpha2.RoleBasedGroupStatus{
					Conditions: []metav1.Condition{
						{Type: string(v1alpha2.RoleBasedGroupReady), Status: "False"},
					},
				},
			},
			want: false,
		},
		{
			name: "no conditions",
			rbg: &v1alpha2.RoleBasedGroup{
				Status: v1alpha2.RoleBasedGroupStatus{},
			},
			want: false,
		},
		{
			name: "other condition type only",
			rbg: &v1alpha2.RoleBasedGroup{
				Status: v1alpha2.RoleBasedGroupStatus{
					Conditions: []metav1.Condition{
						{Type: "SomeOtherCondition", Status: "True"},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isRBGReady(tt.rbg))
		})
	}
}

func TestExtractServedModelName(t *testing.T) {
	tests := []struct {
		name    string
		rbg     *v1alpha2.RoleBasedGroup
		backend string
		want    string
	}{
		{
			name: "flag in args",
			rbg: &v1alpha2.RoleBasedGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "my-rbg"},
				Spec: v1alpha2.RoleBasedGroupSpec{
					Roles: []v1alpha2.RoleSpec{
						*standaloneRole(corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "engine", Args: []string{"--served-model-name", "llama-3"}},
							},
						}),
					},
				},
			},
			backend: "vllm",
			want:    "llama-3",
		},
		{
			name: "flag in command",
			rbg: &v1alpha2.RoleBasedGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "my-rbg"},
				Spec: v1alpha2.RoleBasedGroupSpec{
					Roles: []v1alpha2.RoleSpec{
						*standaloneRole(corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "engine",
									Command: []string{"python", "-m", "vllm.entrypoints.openai.api_server", "--served-model-name", "gpt-custom"},
								},
							},
						}),
					},
				},
			},
			backend: "vllm",
			want:    "gpt-custom",
		},
		{
			name: "flag not found - fallback to rbg name",
			rbg: &v1alpha2.RoleBasedGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "my-rbg"},
				Spec: v1alpha2.RoleBasedGroupSpec{
					Roles: []v1alpha2.RoleSpec{
						*standaloneRole(corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "engine", Args: []string{"--port", "8080"}},
							},
						}),
					},
				},
			},
			backend: "vllm",
			want:    "my-rbg",
		},
		{
			name: "no roles",
			rbg: &v1alpha2.RoleBasedGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-rbg"},
			},
			backend: "sglang",
			want:    "empty-rbg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractServedModelName(tt.rbg, tt.backend))
		})
	}
}

func TestResolveRolePort(t *testing.T) {
	tests := []struct {
		name    string
		role    *v1alpha2.RoleSpec
		backend string
		want    int
	}{
		{
			name: "port from args",
			role: standaloneRole(corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "engine", Args: []string{"--port", "9090"}},
				},
			}),
			backend: "vllm",
			want:    9090,
		},
		{
			name: "port from container ports",
			role: standaloneRole(corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "engine", Ports: []corev1.ContainerPort{{ContainerPort: 7070}}},
				},
			}),
			backend: "vllm",
			want:    7070,
		},
		{
			name: "port from service ports",
			role: &v1alpha2.RoleSpec{
				ServicePorts: []corev1.ServicePort{{Port: 6060}},
			},
			backend: "vllm",
			want:    6060,
		},
		{
			name:    "fallback to vllm default",
			role:    &v1alpha2.RoleSpec{},
			backend: "vllm",
			want:    8000,
		},
		{
			name:    "fallback to sglang default",
			role:    &v1alpha2.RoleSpec{},
			backend: "sglang",
			want:    30000,
		},
		{
			name: "args take priority over container ports and service ports",
			role: func() *v1alpha2.RoleSpec {
				r := standaloneRole(corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "engine",
							Args:  []string{"--port", "9090"},
							Ports: []corev1.ContainerPort{{ContainerPort: 7070}},
						},
					},
				})
				r.ServicePorts = []corev1.ServicePort{{Port: 6060}}
				return r
			}(),
			backend: "vllm",
			want:    9090,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := &Controller{cfg: &config.AutoBenchmarkConfig{Backend: tt.backend}}
			assert.Equal(t, tt.want, ctrl.resolveRolePort(tt.role))
		})
	}
}
