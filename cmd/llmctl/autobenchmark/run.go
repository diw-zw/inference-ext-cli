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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/constant"
	"sigs.k8s.io/rbgs/cli/pkg/util"
)

type runOptions struct {
	cf             *genericclioptions.ConfigFlags
	configFile     string
	name           string
	image          string
	serviceAccount string
}

func newRunCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	opts := &runOptions{
		cf:    cf,
		image: controllerImage,
	}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start an auto-benchmark experiment",
		Long: `Parse the auto-benchmark config, create a ConfigMap with all templates,
and submit a controller Job that runs the experiment autonomously in the cluster.

The controller Job requires a ServiceAccount with permissions to manage RoleBasedGroup resources.
See the guide for RBAC setup instructions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	cmd.Flags().StringVarP(&opts.configFile, "config", "f", "", "Path to auto-benchmark config file (required)")
	cmd.Flags().StringVar(&opts.name, "name", "", "Experiment name (defaults to auto-generated name from config)")
	cmd.Flags().StringVar(&opts.image, "image", controllerImage, "Controller image")
	cmd.Flags().StringVar(&opts.serviceAccount, "service-account", "", "ServiceAccount for the controller Job (required)")
	_ = cmd.MarkFlagRequired("config")
	_ = cmd.MarkFlagRequired("service-account")

	return cmd
}

func (o *runOptions) run(ctx context.Context) error {
	// Parse and validate config
	cfg, err := config.ParseFile(o.configFile)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Override name from CLI flag if provided
	if o.name != "" {
		cfg.Name = o.name
	}

	// Resolve template paths relative to config file directory so that
	// Validate's file-existence check and the subsequent ReadFile use
	// the same absolute paths.
	configDir := filepath.Dir(o.configFile)
	for i, tmpl := range cfg.Templates {
		if !filepath.IsAbs(tmpl.Template) {
			cfg.Templates[i].Template = filepath.Join(configDir, tmpl.Template)
		}
	}

	if err := config.Validate(cfg, true); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	expName := cfg.Name
	if err := validateExpName(expName); err != nil {
		return err
	}
	// Sanitize the experiment name for Kubernetes resource naming (DNS-1123)
	safeName := sanitizeResourceName(expName)

	clientset, err := util.GetK8SClientSet(o.cf)
	if err != nil {
		return fmt.Errorf("getting kubernetes client: %w", err)
	}

	namespace := util.GetNamespace(o.cf)

	// Build ConfigMap data: config YAML + each template file
	cmData := make(map[string]string)

	for i, tmpl := range cfg.Templates {
		data, err := os.ReadFile(tmpl.Template)
		if err != nil {
			return fmt.Errorf("reading template %q: %w", tmpl.Template, err)
		}
		// Use sanitized name as key
		key := sanitizeCMKey(tmpl.Name + ".yaml")
		cmData[key] = string(data)

		// Rewrite template path to match ConfigMap mount location inside container
		cfg.Templates[i].Template = "/etc/autobenchmark/" + key
	}

	// Marshal the modified config (with rewritten template paths) into ConfigMap
	configYAML, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	cmData["config.yaml"] = string(configYAML)

	// Create ConfigMap
	cmName := fmt.Sprintf("ab-%s-config", safeName)
	if len(cmName) > 63 {
		cmName = cmName[:63]
	}
	cmName = strings.TrimRight(cmName, "-")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: namespace,
			Labels:    map[string]string{constant.AutoBenchmarkLabelKey: safeName},
			Annotations: map[string]string{
				constant.AutoBenchmarkOriginalNameAnnotationKey: expName,
			},
		},
		Data: cmData,
	}

	if _, err := clientset.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating ConfigMap: %w", err)
	}
	fmt.Printf("Created ConfigMap %q\n", cmName)

	// Create controller Job
	jobName := fmt.Sprintf("ab-%s", safeName)
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	jobName = strings.TrimRight(jobName, "-")

	backoffLimit := int32(0)
	ttl := int32(86400 * 7) // 7 days

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				constant.AutoBenchmarkLabelKey: safeName,
			},
			Annotations: map[string]string{
				constant.AutoBenchmarkOriginalNameAnnotationKey: expName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: o.serviceAccount,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "controller",
							Image:           o.image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/autobenchmark"},
							Args: []string{
								"--config", "/etc/autobenchmark/config.yaml",
								"--namespace", namespace,
								"--data-dir", "/data",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/autobenchmark",
									ReadOnly:  true,
								},
								{
									Name:      "data",
									MountPath: "/data",
									SubPath:   cfg.Results.SubPath,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
								},
							},
						},
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

	if _, err := clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating controller Job: %w", err)
	}

	fmt.Printf("Created auto-benchmark Job %q in namespace %q\n", jobName, namespace)
	fmt.Printf("Monitor with: llmctl auto-benchmark status %s\n", expName)
	fmt.Printf("View logs with: llmctl auto-benchmark logs %s\n", expName)
	return nil
}

var invalidCMKeyChars = regexp.MustCompile(`[^-._a-zA-Z0-9]+`)

// validateExpName validates the experiment name for use as a Kubernetes label value.
// Label values must be 63 characters or less and contain only alphanumeric characters,
// '-', '_', or '.'.
func validateExpName(name string) error {
	if name == "" {
		return fmt.Errorf("experiment name cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("experiment name %q exceeds 63 character limit (length: %d)", name, len(name))
	}
	// Kubernetes label values: alphanumeric, '-', '_', '.'
	validLabelValueChars := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !validLabelValueChars.MatchString(name) {
		return fmt.Errorf("experiment name %q contains invalid characters; only alphanumeric, '-', '_', '.' are allowed", name)
	}
	return nil
}

func sanitizeCMKey(name string) string {
	name = strings.ToLower(name)
	name = invalidCMKeyChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name
}
