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
	// Last resort fallback: assume a default worker role at the engine's default port.
	return lifecycle.GetServiceEndpoint(trialRBG, &v1alpha2.RoleSpec{Name: "worker"}, ctrl.namespace, defaultEnginePort(ctrl.cfg.Backend))
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

// extractServedModelName reads the served-model-name flag from the base template's container args.
// Falls back to the RBG metadata name if the flag is not found.
func extractServedModelName(rbg *v1alpha2.RoleBasedGroup) string {
	// Both vllm and sglang use --served-model-name.
	const flag = "--served-model-name"
	for _, role := range rbg.Spec.Roles {
		podSpec := getRolePodSpec(&role)
		if podSpec == nil {
			continue
		}
		for _, c := range podSpec.Containers {
			allArgs := make([]string, 0, len(c.Command)+len(c.Args))
			allArgs = append(allArgs, c.Command...)
			allArgs = append(allArgs, c.Args...)
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
// inference endpoint to respond with HTTP 200 on /health AND pass a warmup
// inference request. The warmup probe verifies end-to-end readiness, which is
// critical for PD disaggregation where router /health may return 200 before
// prefill/decode workers have fully registered.
func (ctrl *Controller) waitRBGFullyReady(
	ctx context.Context,
	trialRBG *v1alpha2.RoleBasedGroup,
	trialName string,
	modelName string,
	timeout time.Duration,
) (endpoint string, err error) {
	logger := log.FromContext(ctx)
	rbgReady := false
	healthPassed := false
	httpClient := &http.Client{Timeout: 30 * time.Second}

	endpoint = ctrl.resolveEndpoint(trialRBG)

	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if !rbgReady {
			rbg, err := ctrl.manager.Get(ctx, trialName)
			if err != nil {
				logger.V(2).Info("RBG not found yet", "error", err.Error())
				return false, nil
			}
			if !isRBGReady(rbg) {
				return false, nil
			}
			logger.Info("RBG is ready, waiting for inference endpoint", "rbgName", trialName)
			rbgReady = true
		}

		if !healthPassed {
			healthURL := endpoint + "/health"
			resp, err := httpClient.Get(healthURL)
			if err != nil {
				logger.V(2).Info("Endpoint not ready yet", "error", err.Error())
				return false, nil
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				logger.V(2).Info("Endpoint returned non-OK status", "statusCode", resp.StatusCode)
				return false, nil
			}
			logger.Info("Health check passed, sending warmup probe", "endpoint", endpoint)
			healthPassed = true
		}

		// Warmup probe: send a real completion request to verify end-to-end readiness.
		if err := ctrl.warmupProbe(ctx, httpClient, endpoint, modelName); err != nil {
			logger.V(1).Info("Warmup probe failed, retrying", "error", err.Error())
			return false, nil
		}

		logger.Info("Inference endpoint is fully ready (warmup passed)", "endpoint", endpoint)
		return true, nil
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
	name = strings.Trim(name, "-._")
	if name == "" {
		name = "default"
	}
	return name
}

// warmupProbe sends a minimal inference request to verify the pipeline is
// functional end-to-end. It first tries /v1/completions; if the engine returns
// 404 or 405 (route not found), it falls back to /v1/chat/completions for
// chat-only deployments. Specific 4xx responses are interpreted as follows:
//   - 400/422: engine is actively processing requests (request format rejected)
//   - 401/403: engine is reachable but may have auth misconfiguration (logged as warning)
//   - other 4xx: engine is reachable (logged as warning for operator visibility)
//
// Only connection errors and 5xx responses trigger a retry.
func (ctrl *Controller) warmupProbe(ctx context.Context, client *http.Client, endpoint, modelName string) error {
	logger := log.FromContext(ctx)

	// Try /v1/completions first.
	completionsURL := endpoint + "/v1/completions"
	completionsBody := fmt.Sprintf(`{"model":%q,"prompt":"hi","max_tokens":1}`, modelName)

	statusCode, err := doWarmupRequest(ctx, client, completionsURL, completionsBody)
	if err != nil {
		return fmt.Errorf("warmup request failed: %w", err)
	}
	if statusCode < 400 {
		return nil // success
	}

	// 404/405 means the route doesn't exist — fall back to chat completions.
	if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed {
		chatURL := endpoint + "/v1/chat/completions"
		chatBody := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"max_tokens":1}`, modelName)

		statusCode, err = doWarmupRequest(ctx, client, chatURL, chatBody)
		if err != nil {
			return fmt.Errorf("warmup chat request failed: %w", err)
		}
		if statusCode < 400 {
			return nil // success via chat endpoint
		}
	}

	// Any 4xx means the engine is reachable. Log a warning for status codes
	// that may indicate misconfiguration (e.g., auth issues) so operators can
	// distinguish from genuine request-format rejections (400/422).
	if statusCode >= 400 && statusCode < 500 {
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			logger.Info("WARNING: warmup probe received auth error — engine is reachable but may have authentication misconfiguration",
				"statusCode", statusCode, "endpoint", endpoint)
		} else if statusCode != http.StatusBadRequest && statusCode != http.StatusUnprocessableEntity {
			logger.Info("Warmup probe received unexpected 4xx — treating as reachable",
				"statusCode", statusCode, "endpoint", endpoint)
		}
		return nil
	}

	// 5xx: server-side error, not ready yet.
	return fmt.Errorf("warmup returned HTTP %d", statusCode)
}

// doWarmupRequest sends a POST request and returns the HTTP status code.
// The response body is always closed.
func doWarmupRequest(ctx context.Context, client *http.Client, url, body string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}
