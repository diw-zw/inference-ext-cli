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
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/rbgs/api/workloads/constants"
	abtypes "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/types"
)

const failureLogTailLines int64 = 200

// podRestartSnapshot maps "podName/containerName" to its RestartCount.
type podRestartSnapshot map[string]int32

// snapshotRestartCounts records the current RestartCount of every container
// across all pods belonging to the trial RBG. Returns nil on any error.
func (ctrl *Controller) snapshotRestartCounts(trialName string) podRestartSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	podList, err := ctrl.clientset.CoreV1().Pods(ctrl.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{constants.GroupNameLabelKey: trialName}.String(),
	})
	if err != nil || len(podList.Items) == 0 {
		return nil
	}

	snap := make(podRestartSnapshot)
	for i := range podList.Items {
		pod := &podList.Items[i]
		for _, cs := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
			snap[pod.Name+"/"+cs.Name] = cs.RestartCount
		}
	}
	return snap
}

// collectBenchmarkFailureLogs checks pod state after eval.Run failure and
// collects logs only when pods actually crashed during the benchmark:
//   - Pod in Failed phase → collect current logs (before RBG controller deletes it)
//   - Pod Running but RestartCount increased → collect previous container logs
//   - Neither → benchmark tool's own problem, skip
func (ctrl *Controller) collectBenchmarkFailureLogs(trialName string, resultDir string, preRunRestarts podRestartSnapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := log.FromContext(ctx).WithValues("trialName", trialName)

	podList, err := ctrl.clientset.CoreV1().Pods(ctrl.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{constants.GroupNameLabelKey: trialName}.String(),
	})
	if err != nil || len(podList.Items) == 0 {
		return
	}

	var needLogs bool
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase == corev1.PodFailed {
			needLogs = true
			break
		}
		if preRunRestarts != nil {
			for _, cs := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
				if cs.RestartCount > preRunRestarts[pod.Name+"/"+cs.Name] {
					needLogs = true
					break
				}
			}
		}
		if needLogs {
			break
		}
	}

	if !needLogs {
		return
	}

	logDir := filepath.Join(resultDir, "pod-logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logger.Info("Failed to create pod-logs directory", "error", err.Error())
		return
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		logPath := filepath.Join(logDir, pod.Name+".log")
		ctrl.writePodLogs(ctx, logPath, pod)
	}

	logger.Info("Collected benchmark failure logs", "logDir", logDir)
}

// collectFailureLogs fetches failure context from all pods belonging to the
// trial RBG. For Pending pods (never started), it appends a short summary
// directly to result.Error. For pods that did run, it writes the last N lines
// of container logs to {resultDir}/pod-logs/{podName}.log.
// Errors during collection are logged but never propagated.
func (ctrl *Controller) collectFailureLogs(trialName string, resultDir string, result *abtypes.TrialResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := log.FromContext(ctx).WithValues("trialName", trialName)

	podList, err := ctrl.clientset.CoreV1().Pods(ctrl.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{constants.GroupNameLabelKey: trialName}.String(),
	})
	if err != nil {
		logger.Info("Failed to list pods for failure log collection", "error", err.Error())
		return
	}

	if len(podList.Items) == 0 {
		logger.Info("No pods found for failure log collection")
		return
	}

	var pendingSummaries []string
	var logFileCount int

	for i := range podList.Items {
		pod := &podList.Items[i]

		if pod.Status.Phase == corev1.PodPending {
			if s := summarizePendingPod(pod); s != "" {
				pendingSummaries = append(pendingSummaries, s)
			}
			continue
		}

		logDir := filepath.Join(resultDir, "pod-logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger.Info("Failed to create pod-logs directory", "error", err.Error())
			continue
		}
		logPath := filepath.Join(logDir, pod.Name+".log")
		ctrl.writePodLogs(ctx, logPath, pod)
		logFileCount++
	}

	if len(pendingSummaries) > 0 {
		result.Error = fmt.Sprintf("%s; pending pods: %s",
			result.Error, strings.Join(pendingSummaries, "; "))
	}

	if logFileCount > 0 {
		logger.Info("Collected failure logs", "logFiles", logFileCount, "dir", filepath.Join(resultDir, "pod-logs"))
	}
}

// summarizePendingPod returns a one-line summary of why a pod is stuck in Pending.
func summarizePendingPod(pod *corev1.Pod) string {
	// Check container statuses (e.g. ImagePullBackOff, OOMKilled, Error)
	for _, cs := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			if cs.State.Waiting.Message != "" {
				return fmt.Sprintf("%s: %s (%s)", pod.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
			return fmt.Sprintf("%s: %s", pod.Name, cs.State.Waiting.Reason)
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			return fmt.Sprintf("%s: %s (exitCode=%d)", pod.Name, cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
		}
	}
	// Check pod conditions (e.g. Unschedulable)
	for _, c := range pod.Status.Conditions {
		if c.Status == corev1.ConditionFalse && c.Reason != "" {
			if c.Message != "" {
				return fmt.Sprintf("%s: %s (%s)", pod.Name, c.Reason, c.Message)
			}
			return fmt.Sprintf("%s: %s", pod.Name, c.Reason)
		}
	}
	if pod.Status.Reason != "" {
		return fmt.Sprintf("%s: %s", pod.Name, pod.Status.Reason)
	}
	return fmt.Sprintf("%s: Pending", pod.Name)
}

// writePodLogs fetches the last N lines of logs from all containers in a pod.
// When a container has restarted (e.g. OOM crash), it also fetches the previous
// instance's logs which contain the actual crash output.
func (ctrl *Controller) writePodLogs(ctx context.Context, logPath string, pod *corev1.Pod) {
	var sb strings.Builder
	tailLines := failureLogTailLines

	restartCounts := make(map[string]int32)
	for _, cs := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		restartCounts[cs.Name] = cs.RestartCount
	}

	allContainers := make([]string, 0)
	for _, c := range pod.Spec.InitContainers {
		allContainers = append(allContainers, c.Name)
	}
	for _, c := range pod.Spec.Containers {
		allContainers = append(allContainers, c.Name)
	}

	for _, containerName := range allContainers {
		if restartCounts[containerName] > 0 {
			sb.WriteString(fmt.Sprintf("=== Container: %s (previous, restartCount=%d) ===\n", containerName, restartCounts[containerName]))
			ctrl.fetchContainerLogs(ctx, &sb, pod.Name, containerName, tailLines, true)
		}

		sb.WriteString(fmt.Sprintf("=== Container: %s ===\n", containerName))
		ctrl.fetchContainerLogs(ctx, &sb, pod.Name, containerName, tailLines, false)
	}

	_ = os.WriteFile(logPath, []byte(sb.String()), 0644)
}

func (ctrl *Controller) fetchContainerLogs(ctx context.Context, sb *strings.Builder, podName, containerName string, tailLines int64, previous bool) {
	req := ctrl.clientset.CoreV1().Pods(ctrl.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
		Previous:  previous,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		sb.WriteString(fmt.Sprintf("(failed to get logs: %v)\n\n", err))
		return
	}
	logBytes, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil {
		sb.WriteString(fmt.Sprintf("(failed to read logs: %v)\n\n", err))
		return
	}
	sb.Write(logBytes)
	sb.WriteString("\n\n")
}
