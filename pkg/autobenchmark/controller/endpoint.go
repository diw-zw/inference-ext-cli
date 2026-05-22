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
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/lifecycle"
)

// resolveEndpoint determines the inference endpoint for a trial RBG.
func (ctrl *Controller) resolveEndpoint(trialRBG *v1alpha2.RoleBasedGroup) string {
	for _, role := range trialRBG.Spec.Roles {
		port := ctrl.resolveRolePort(&role)
		if port <= 0 {
			continue
		}
		// For leader-worker pattern, route directly to the leader pod via headless service DNS.
		// The RBG controller only creates headless services; using service-level DNS may resolve
		// to worker pods, which do not serve inference requests.
		if role.LeaderWorkerPattern != nil {
			return lifecycle.GetLeaderPodEndpoint(trialRBG, &role, ctrl.namespace, port)
		}
		return lifecycle.GetServiceEndpoint(trialRBG, &role, ctrl.namespace, port)
	}
	// Last resort fallback: assume a default worker role at port 8000.
	return lifecycle.GetServiceEndpoint(trialRBG, &v1alpha2.RoleSpec{Name: "worker"}, ctrl.namespace, 8000)
}

// resolveRolePort extracts the inference port for a role.
// Priority: args --port > ServicePorts > container ports > engine default.
func (ctrl *Controller) resolveRolePort(role *v1alpha2.RoleSpec) int {
	podSpec := getRolePodSpec(role)
	if podSpec != nil && len(podSpec.Containers) > 0 {
		// 1. Check container args for explicit --port.
		for _, c := range podSpec.Containers {
			for i, arg := range c.Args {
				if arg == "--port" && i+1 < len(c.Args) {
					if p, err := strconv.Atoi(c.Args[i+1]); err == nil && p > 0 {
						return p
					}
				}
			}
		}
		// 2. Check container ports.
		for _, c := range podSpec.Containers {
			for _, p := range c.Ports {
				if p.ContainerPort > 0 {
					return int(p.ContainerPort)
				}
			}
		}
	}
	// 3. Check ServicePorts.
	if len(role.ServicePorts) > 0 {
		return int(role.ServicePorts[0].Port)
	}
	// 4. Fall back to engine default.
	return defaultEnginePort(ctrl.cfg.Backend)
}

func defaultEnginePort(backend string) int {
	switch backend {
	case "sglang":
		return 30000
	case "vllm":
		return 8000
	default:
		return 8000
	}
}

// extractServedModelName reads --served-model-name from the base template's container args.
// Falls back to the RBG metadata name if the flag is not found.
func extractServedModelName(rbg *v1alpha2.RoleBasedGroup, backend string) string {
	flag := "--served-model-name"
	if backend == "vllm" {
		flag = "--served-model-name"
	}
	for _, role := range rbg.Spec.Roles {
		podSpec := getRolePodSpec(&role)
		if podSpec == nil {
			continue
		}
		for _, c := range podSpec.Containers {
			allArgs := append(c.Command, c.Args...)
			for i, arg := range allArgs {
				if arg == flag && i+1 < len(allArgs) {
					return allArgs[i+1]
				}
			}
		}
	}
	return rbg.Name
}

// getRolePodSpec extracts the PodSpec from a RoleSpec regardless of pattern type.
func getRolePodSpec(role *v1alpha2.RoleSpec) *corev1.PodSpec {
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

// waitRBGFullyReady waits for both the RBG to report Ready=True and the
// inference endpoint to respond with HTTP 200, sharing a single timeout.
// A bool tracks whether the RBG ready phase is complete so that subsequent
// polls only hit the endpoint (reducing unnecessary API calls).
func (ctrl *Controller) waitRBGFullyReady(
	ctx context.Context,
	trialRBG *v1alpha2.RoleBasedGroup,
	trialName string,
	timeout time.Duration,
) (endpoint string, err error) {
	logger := log.FromContext(ctx)
	rbgReady := false
	httpClient := &http.Client{Timeout: 5 * time.Second}

	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if !rbgReady {
			rbg, err := ctrl.manager.Get(ctx, trialName)
			if err != nil {
				logger.V(2).Info("RBG not found yet", "error", err.Error())
				return false, nil // retry on transient errors
			}
			if !isRBGReady(rbg) {
				return false, nil
			}
			logger.Info("RBG is ready, waiting for inference endpoint", "rbgName", trialName)
			rbgReady = true
		}

		endpoint = ctrl.resolveEndpoint(trialRBG)
		healthURL := endpoint + "/health"
		resp, err := httpClient.Get(healthURL)
		if err != nil {
			logger.V(2).Info("Endpoint not ready yet", "error", err.Error())
			return false, nil // retry
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusOK {
			logger.Info("Inference endpoint is ready", "healthURL", healthURL)
			return true, nil
		}
		logger.V(2).Info("Endpoint returned non-OK status", "statusCode", resp.StatusCode)
		return false, nil
	})
	return endpoint, err
}

// isRBGReady checks if the RBG has a Ready=True condition.
func isRBGReady(rbg *v1alpha2.RoleBasedGroup) bool {
	for _, c := range rbg.Status.Conditions {
		if c.Type == string(v1alpha2.RoleBasedGroupReady) && c.Status == "True" {
			return true
		}
	}
	return false
}

// sanitizeLabelValue ensures a string is valid for use as a Kubernetes label value.
// Label values must be 63 characters or less and contain only alphanumeric characters,
// '-', '_', or '.'. This function truncates and sanitizes as needed.
func sanitizeLabelValue(name string) string {
	if len(name) > 63 {
		name = name[:63]
	}
	// Replace invalid characters with '-'
	invalidChars := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	name = invalidChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "default"
	}
	return name
}
