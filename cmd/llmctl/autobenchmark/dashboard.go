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

package autobenchmark

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/constant"
	"sigs.k8s.io/rbgs/cli/pkg/util"
)

const (
	dashboardImage   = "rolebasedgroup/rbgs-autobenchmark-dashboard:latest"
	dashboardPort    = 80
	defaultLocalPort = 18888
)

type dashboardOptions struct {
	cf         *genericclioptions.ConfigFlags
	configFile string
	name       string
	image      string
	localPort  int
}

func newDashboardCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	opts := &dashboardOptions{
		cf:        cf,
		image:     dashboardImage,
		localPort: defaultLocalPort,
	}

	cmd := &cobra.Command{
		Use:   "dashboard [name]",
		Short: "Launch a web dashboard for experiment results",
		Long: `Create a Deployment serving the auto-benchmark results UI,
mount the experiment's output PVC, and port-forward to localhost.

The dashboard is available while this command is running. Press Ctrl+C to stop and clean up.

If [name] is provided, it overrides the experiment name from the config file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.name = args[0]
			}
			return opts.run(cmd.Context())
		},
	}

	cmd.Flags().StringVarP(&opts.configFile, "config", "f", "", "Path to auto-benchmark config file (required)")
	cmd.Flags().StringVar(&opts.image, "image", dashboardImage, "Dashboard container image")
	cmd.Flags().IntVarP(&opts.localPort, "port", "p", defaultLocalPort, "Local port for port-forward")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func (o *dashboardOptions) run(ctx context.Context) error {
	// Parse config
	cfg, err := config.ParseFile(o.configFile)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	expName := cfg.Name
	if o.name != "" {
		expName = o.name
	}

	// Validate experiment name for use as Kubernetes label value
	if err := validateExpName(expName); err != nil {
		return err
	}

	// Kubernetes client
	clientset, err := util.GetK8SClientSet(o.cf)
	if err != nil {
		return fmt.Errorf("getting kubernetes client: %w", err)
	}
	namespace := util.GetNamespace(o.cf)

	// Compute PVC subPath: {results.subPath}/{experiment-name}
	fullSubPath := filepath.Join(cfg.Results.SubPath, expName)

	// Sanitize experiment name to comply with DNS-1035 label requirements.
	// Use "abd-{name}" prefix to ensure the result always starts with a letter.
	safeName := sanitizeResourceName(expName)
	deployName := fmt.Sprintf("abd-%s", safeName)
	svcName := deployName

	// Use sanitized name for selector labels to ensure DNS-1123 compliance
	selectorLabels := map[string]string{
		"app":                          "auto-benchmark-dashboard",
		constant.AutoBenchmarkLabelKey: safeName,
	}
	// Store original name in annotations for display
	annotations := map[string]string{
		constant.AutoBenchmarkOriginalNameAnnotationKey: expName,
	}

	// --- Create Deployment ---
	fmt.Printf("Creating Deployment %s... ", deployName)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        deployName,
			Namespace:   namespace,
			Labels:      selectorLabels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: selectorLabels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "dashboard",
							Image: o.image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: dashboardPort},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/usr/share/nginx/html/data",
									SubPath:   fullSubPath,
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: cfg.Results.PVC,
								},
							},
						},
					},
				},
			},
		},
	}

	// Delete existing if present, then create
	deployClient := clientset.AppsV1().Deployments(namespace)
	_, err = deployClient.Get(ctx, deployName, metav1.GetOptions{})
	if err == nil {
		fmt.Printf("replacing existing... ")
		if err := deployClient.Delete(ctx, deployName, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("deleting existing Deployment: %w", err)
		}
	}

	_, err = deployClient.Create(ctx, deploy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating Deployment: %w", err)
	}
	fmt.Println("done")

	// --- Create Service ---
	fmt.Printf("Creating Service %s... ", svcName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svcName,
			Namespace:   namespace,
			Labels:      selectorLabels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports: []corev1.ServicePort{
				{
					Port:       dashboardPort,
					TargetPort: intstr.FromInt(dashboardPort),
				},
			},
		},
	}

	svcClient := clientset.CoreV1().Services(namespace)
	_, err = svcClient.Get(ctx, svcName, metav1.GetOptions{})
	if err == nil {
		fmt.Printf("replacing existing... ")
		if err := svcClient.Delete(ctx, svcName, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("deleting existing Service: %w", err)
		}
	}

	_, err = svcClient.Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating Service: %w", err)
	}
	fmt.Println("done")

	// --- Port-forward ---
	fmt.Printf("Waiting for Deployment %s to be ready... ", deployName)
	if err := o.waitForDeploymentReady(ctx, namespace, deployName); err != nil {
		return fmt.Errorf("waiting for deployment: %w", err)
	}

	fmt.Printf("Starting port-forward svc/%s :%d -> :%d...\n", svcName, o.localPort, dashboardPort)

	return o.runPortForward(ctx, namespace, svcName, deployName)
}

func (o *dashboardOptions) runPortForward(ctx context.Context, namespace, svcName, deployName string) error {
	// Build kubectl command
	kubeconfig := ""
	if o.cf.KubeConfig != nil {
		kubeconfig = *o.cf.KubeConfig
	}

	args := []string{
		"port-forward",
		"-n", namespace,
		fmt.Sprintf("svc/%s", svcName),
		fmt.Sprintf("%d:%d", o.localPort, dashboardPort),
	}
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting port-forward: %w", err)
	}

	// Monitor stdout for ready signal
	readyCh := make(chan struct{})
	var readyOnce sync.Once
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Forwarding from") {
				readyOnce.Do(func() { close(readyCh) })
			}
			fmt.Printf("kubectl: %s\n", line)
		}
	}()

	// Monitor stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintf(os.Stderr, "kubectl: %s\n", scanner.Text())
		}
	}()

	// Cleanup on interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-readyCh:
		dashboardURL := fmt.Sprintf("http://localhost:%d", o.localPort)
		fmt.Printf("\nDashboard available at: %s\n", dashboardURL)
		fmt.Println("Press Ctrl+C to stop")
		openBrowser(dashboardURL)
	case <-sigCh:
		_ = cmd.Process.Kill()
		_ = o.cleanup(ctx, namespace, deployName, svcName)
		return fmt.Errorf("interrupted")
	}

	// Block until signal
	<-sigCh
	fmt.Println("\nCleaning up...")

	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	return o.cleanup(ctx, namespace, deployName, svcName)
}

func (o *dashboardOptions) cleanup(ctx context.Context, namespace, deployName, svcName string) error {
	clientset, err := util.GetK8SClientSet(o.cf)
	if err != nil {
		return fmt.Errorf("cleanup: getting client: %w", err)
	}

	// Delete Deployment
	if err := clientset.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete Deployment %s: %v\n", deployName, err)
	} else {
		fmt.Printf("Deleted Deployment %s\n", deployName)
	}

	// Delete Service
	if err := clientset.CoreV1().Services(namespace).Delete(ctx, svcName, metav1.DeleteOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete Service %s: %v\n", svcName, err)
	} else {
		fmt.Printf("Deleted Service %s\n", svcName)
	}

	return nil
}

func (o *dashboardOptions) waitForDeploymentReady(ctx context.Context, namespace, deployName string) error {
	clientset, err := util.GetK8SClientSet(o.cf)
	if err != nil {
		return fmt.Errorf("getting client: %w", err)
	}

	const (
		pollInterval = 5 * time.Second
		pollTimeout  = 5 * time.Minute
	)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	timeoutCh := time.After(pollTimeout)
	dotCount := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			fmt.Println()
			return fmt.Errorf("deployment %s did not become ready within %v", deployName, pollTimeout)
		case <-ticker.C:
			deploy, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting deployment: %w", err)
			}

			if deploy.Status.ReadyReplicas > 0 {
				fmt.Println()
				return nil
			}

			// Print dots to show progress
			if dotCount%20 == 0 && dotCount > 0 {
				fmt.Println()
			}
			fmt.Print(".")
			dotCount++
		}
	}
}

func int32Ptr(i int32) *int32 { return &i }

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

var invalidNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

// sanitizeResourceName ensures a name complies with Kubernetes DNS-1035 label requirements:
// lowercase alphanumeric characters or '-', starting with an alphabetic character.
func sanitizeResourceName(name string) string {
	name = strings.ToLower(name)
	name = invalidNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	// If the name starts with a digit, prefix with 'n'
	if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
		name = "n" + name
	}
	// If empty after sanitizing, use a fallback
	if name == "" {
		name = "default"
	}
	if len(name) > 50 {
		name = name[:50]
		name = strings.TrimRight(name, "-")
	}
	return name
}
