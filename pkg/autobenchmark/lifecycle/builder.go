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

package lifecycle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"sigs.k8s.io/rbgs/api/workloads/constants"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

// Builder creates trial-specific RBG specs from base templates.
type Builder struct {
	mapper *ParamMapper
}

// NewBuilder creates a Builder for the given backend.
func NewBuilder(backend string) (*Builder, error) {
	mapper, err := NewParamMapper(backend)
	if err != nil {
		return nil, err
	}
	return &Builder{mapper: mapper}, nil
}

// LoadTemplate reads an RBG YAML file and extracts the RoleBasedGroup object.
// Handles multi-document YAML (RBG + Service).
func LoadTemplate(path string) (*workloadsv1alpha2.RoleBasedGroup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading template file %q: %w", path, err)
	}
	return ParseRBGFromYAML(data)
}

// ParseRBGFromYAML extracts the RoleBasedGroup from a potentially multi-document YAML.
func ParseRBGFromYAML(data []byte) (*workloadsv1alpha2.RoleBasedGroup, error) {
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading YAML document: %w", err)
		}
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		// Decode YAML to JSON first (K8s objects use JSON tags)
		jsonData, err := yaml.ToJSON(doc)
		if err != nil {
			continue // skip non-parseable documents
		}

		// Check if this is an RBG by looking at kind
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(jsonData, &meta); err != nil {
			continue
		}
		if meta.Kind != "RoleBasedGroup" {
			continue
		}

		var rbg workloadsv1alpha2.RoleBasedGroup
		if err := json.Unmarshal(jsonData, &rbg); err != nil {
			return nil, fmt.Errorf("unmarshaling RoleBasedGroup: %w", err)
		}
		return &rbg, nil
	}
	return nil, fmt.Errorf("no RoleBasedGroup found in YAML")
}

// BuildTrial creates a trial-specific RBG spec by deep-copying the base and
// overlaying the search params onto the container commands.
//
// Role-specific params: keys in params map to role names.
// "default" key applies to all roles.
func (b *Builder) BuildTrial(base *workloadsv1alpha2.RoleBasedGroup, trialIndex int, params abtypes.RoleParamSet) (*workloadsv1alpha2.RoleBasedGroup, error) {
	// Deep-copy via JSON round-trip
	trial, err := deepCopyRBG(base)
	if err != nil {
		return nil, fmt.Errorf("deep-copying RBG: %w", err)
	}

	// Generate trial name
	trial.Name = fmt.Sprintf("%s-trial-%d", base.Name, trialIndex)
	// Clear resource version (new object)
	trial.ResourceVersion = ""
	trial.UID = ""

	// Overlay params on each role
	for i := range trial.Spec.Roles {
		role := &trial.Spec.Roles[i]

		// Collect params for this role: start with "default", then override with role-specific
		merged := make(map[string]any)
		if defaultParams, ok := params["default"]; ok {
			for k, v := range defaultParams {
				merged[k] = v
			}
		}
		if roleParams, ok := params[role.Name]; ok {
			for k, v := range roleParams {
				merged[k] = v
			}
		}
		if len(merged) == 0 {
			continue
		}

		if err := b.overlayRoleParams(role, merged); err != nil {
			return nil, fmt.Errorf("overlaying params on role %q: %w", role.Name, err)
		}
	}

	return trial, nil
}

// overlayRoleParams overlays params onto the first container's command args in a role.
func (b *Builder) overlayRoleParams(role *workloadsv1alpha2.RoleSpec, params map[string]any) error {
	podSpec := getRolePodSpec(role)
	if podSpec == nil {
		return fmt.Errorf("role %q has no PodSpec", role.Name)
	}

	if len(podSpec.Containers) == 0 {
		return fmt.Errorf("role %q has no containers", role.Name)
	}

	container := &podSpec.Containers[0]
	newArgs, err := b.mapper.OverlayArgs(mergeCommandArgs(container), params)
	if err != nil {
		return err
	}

	// Write back: if original used Command, keep that pattern
	if len(container.Command) > 0 {
		container.Command = newArgs
		container.Args = nil
	} else {
		container.Args = newArgs
	}
	return nil
}

// mergeCommandArgs returns the full command line (Command + Args).
// In Kubernetes, the executed process is command[0:] + args[0:],
// so we must merge both to get the complete argument list.
func mergeCommandArgs(c *corev1.Container) []string {
	result := make([]string, 0, len(c.Command)+len(c.Args))
	result = append(result, c.Command...)
	result = append(result, c.Args...)
	return result
}

// getRolePodSpec extracts the PodSpec from a RoleSpec regardless of pattern type.
func getRolePodSpec(role *workloadsv1alpha2.RoleSpec) *corev1.PodSpec {
	if sp := role.StandalonePattern; sp != nil {
		if sp.Template != nil {
			return &sp.Template.Spec
		}
	}
	if lw := role.LeaderWorkerPattern; lw != nil {
		if lw.Template != nil {
			return &lw.Template.Spec
		}
	}
	return nil
}

// GetTrialName returns the trial-specific RBG name.
func GetTrialName(baseName string, trialIndex int) string {
	return fmt.Sprintf("%s-trial-%d", baseName, trialIndex)
}

// GetServiceEndpoint returns the inference endpoint URL for an RBG by convention.
// Service name is resolved via the controller's GetServiceName helper.
func GetServiceEndpoint(rbg *workloadsv1alpha2.RoleBasedGroup, role *workloadsv1alpha2.RoleSpec, namespace string, port int) string {
	// TODO: use helper function in rbg-api
	// svcName := rbg.GetServiceName(role)
	svcName := GetServiceName(rbg, role)
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svcName, namespace, port)
}

// GetLeaderPodEndpoint returns the inference endpoint URL for a leader pod in leader-worker pattern.
// Pod naming and service naming are delegated to the RoleInstance controller helpers so that
// auto-benchmark and the controller share the same naming rules.
// The endpoint uses pod DNS under the headless service:
//
//	{podName}.{svcName}.{namespace}.svc.cluster.local:port
func GetLeaderPodEndpoint(rbg *workloadsv1alpha2.RoleBasedGroup, role *workloadsv1alpha2.RoleSpec, namespace string, port int) string {
	// TODO: use helper function in rbg-api
	// svcName := rbg.GetServiceName(role)
	// instanceName := rbg.GetWorkloadName(role)
	svcName := GetServiceName(rbg, role)
	instanceName := GetWorkloadName(rbg, role)
	podName := FormatComponentPodName(instanceName, "leader", 0, constants.LeaderWorkerSetTemplateType)
	return fmt.Sprintf("http://%s.%s.%s.svc.cluster.local:%d", podName, svcName, namespace, port)
}

func deepCopyRBG(src *workloadsv1alpha2.RoleBasedGroup) (*workloadsv1alpha2.RoleBasedGroup, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst workloadsv1alpha2.RoleBasedGroup
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil, err
	}
	return &dst, nil
}

func FormatComponentPodName(instanceName, componentName string, id int32, roleTemplateType constants.RoleTemplateType) string {
	switch roleTemplateType {
	case constants.LeaderWorkerSetTemplateType:
		podIndex := id
		if componentName == "worker" {
			podIndex++
		}
		return fmt.Sprintf("%s-%d", instanceName, podIndex)
	case constants.ComponentsTemplateType:
		return fmt.Sprintf("%s-%s-%d", instanceName, componentName, id)
	default:
		return instanceName
	}
}

// GetServiceName returns the service name for a role.
// Because ServiceName needs to follow DNS naming conventions,
// which do not allow names to start with a number. Therefore, the s- prefix
// is added to the service name to meet this requirement.
func GetServiceName(rbg *workloadsv1alpha2.RoleBasedGroup, role *workloadsv1alpha2.RoleSpec) string {
	svcName := fmt.Sprintf("s-%s-%s", rbg.Name, role.Name)
	if len(svcName) > 63 {
		svcName = svcName[:63]
		// After truncation, trim trailing hyphens (and ensure the name ends with an alphanumeric)
		// to maintain DNS-1123/DNS-1035 validity.
		svcName = strings.TrimRight(svcName, "-")
	}
	return svcName
}

// GetWorkloadName returns the workload name for a role.
func GetWorkloadName(rbg *workloadsv1alpha2.RoleBasedGroup, role *workloadsv1alpha2.RoleSpec) string {
	if rbg == nil {
		return ""
	}

	workloadName := fmt.Sprintf("%s-%s", rbg.Name, role.Name)

	// Kubernetes name length is limited to 63 characters
	if len(workloadName) > 63 {
		workloadName = workloadName[:63]
		workloadName = strings.TrimRight(workloadName, "-")
	}
	return workloadName
}
