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

package storage

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/rbgs/cli/pkg/plugin/util"
)

const ossStorageType = "oss"

func init() {
	Register(ossStorageType, func() Plugin {
		return &OSSStorage{}
	})
}

// OSSStorage implements the StoragePlugin interface for Alibaba Cloud OSS storage
type OSSStorage struct {
	// config fields
	storageSize     string
	url             string
	bucket          string
	subpath         string
	secretName      string
	secretNamespace string
}

// Name returns the plugin name
func (o *OSSStorage) Name() string {
	return ossStorageType
}

// ConfigFields returns the config fields this plugin accepts.
// akId and akSecret are required during initial configuration (add-storage/init) to
// create the Secret via PreAdd. After PreAdd, the saved config uses secretName and
// secretNamespace instead.
func (o *OSSStorage) ConfigFields() []util.ConfigField {
	return []util.ConfigField{
		{Key: "url", Description: "OSS endpoint URL (e.g., oss-cn-hangzhou.aliyuncs.com)", Required: true},
		{Key: "bucket", Description: "OSS bucket name", Required: true},
		{Key: "subpath", Description: "subpath within the bucket", Required: false},
		{Key: "akId", Description: "Alibaba Cloud AccessKey ID", Required: true},
		{Key: "akSecret", Description: "Alibaba Cloud AccessKey Secret", Required: true, Masked: util.MaskAll},
	}
}

// Init initializes the plugin with a config that has been processed by PreAdd.
// The config must contain secretName and secretNamespace (the Secret reference
// created by PreAdd). Direct credentials (akId/akSecret) are not accepted here.
func (o *OSSStorage) Init(config map[string]interface{}) error {
	o.storageSize = "1Ti"

	if url, ok := config["url"].(string); !ok || url == "" {
		return fmt.Errorf("url is required in storage config for oss type")
	} else {
		o.url = url
	}

	if bucket, ok := config["bucket"].(string); !ok || bucket == "" {
		return fmt.Errorf("bucket is required in storage config for oss type")
	} else {
		o.bucket = bucket
	}

	// subpath is optional
	o.subpath, _ = config["subpath"].(string)
	if o.subpath == "" {
		o.subpath = "/"
	}

	// Check for secretName and secretNamespace (required after PreAdd)
	if secretName, ok := config["secretName"].(string); ok && secretName != "" {
		o.secretName = secretName
	} else {
		return fmt.Errorf("secretName is required in storage config for oss type")
	}

	if secretNamespace, ok := config["secretNamespace"].(string); ok && secretNamespace != "" {
		o.secretNamespace = secretNamespace
	} else {
		return fmt.Errorf("secretNamespace is required in storage config for oss type")
	}

	return nil
}

// PreMount verifies PV and PVC resources exist. Secret is expected to already exist
// (created by PreAdd) and is referenced via secretName/secretNamespace.
func (o *OSSStorage) preMount(c client.Client, storageName, namespace string) error {
	ctx := context.Background()
	pvName := storageName
	pvcName := storageName

	// Verify secret credentials are set
	if o.secretName == "" || o.secretNamespace == "" {
		return fmt.Errorf("secretName/secretNamespace are not set, PreAdd must be called before MountStorage")
	}

	// Step 1: Verify Secret exists (created by PreAdd)
	if err := o.verifySecretExists(ctx, c); err != nil {
		return fmt.Errorf("failed to verify secret: %w", err)
	}

	// Step 2: Create or verify PV
	if err := o.createOrVerifyPV(ctx, c, pvName, namespace); err != nil {
		return fmt.Errorf("failed to create/verify PV: %w", err)
	}

	// Step 3: Create or verify PVC
	if err := o.createOrVerifyPVC(ctx, c, pvcName, pvName, namespace); err != nil {
		return fmt.Errorf("failed to create/verify PVC: %w", err)
	}

	return nil
}

// verifySecretExists verifies that the referenced secret exists
func (o *OSSStorage) verifySecretExists(ctx context.Context, c client.Client) error {
	if o.secretName == "" || o.secretNamespace == "" {
		return fmt.Errorf("secretName/secretNamespace are not set")
	}

	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Name: o.secretName, Namespace: o.secretNamespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("secret %s/%s not found, was PreAdd called?", o.secretNamespace, o.secretName)
		}
		return err
	}

	// Verify secret has required fields
	if _, ok := secret.Data["akId"]; !ok {
		return fmt.Errorf("secret %s/%s missing akId field", o.secretNamespace, o.secretName)
	}
	if _, ok := secret.Data["akSecret"]; !ok {
		return fmt.Errorf("secret %s/%s missing akSecret field", o.secretNamespace, o.secretName)
	}

	return nil
}

// createOrVerifyPV creates the PV or verifies it if already exists
func (o *OSSStorage) createOrVerifyPV(ctx context.Context, c client.Client, pvName, namespace string) error {
	if o.secretName == "" || o.secretNamespace == "" {
		return fmt.Errorf("secretName/secretNamespace are not set")
	}

	pv := &corev1.PersistentVolume{}
	err := c.Get(ctx, types.NamespacedName{Name: pvName}, pv)
	if err == nil {
		// PV exists, verify it
		return o.verifyPV(pv)
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create new PV
	storageQuantity, err := resource.ParseQuantity(o.storageSize)
	if err != nil {
		return fmt.Errorf("invalid storageSize %q: %w", o.storageSize, err)
	}

	newPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvName,
			Namespace: namespace,
			Labels: map[string]string{
				"alicloud-pvname": pvName,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: storageQuantity,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "ossplugin.csi.alibabacloud.com",
					VolumeHandle: pvName,
					NodePublishSecretRef: &corev1.SecretReference{
						Name:      o.secretName,
						Namespace: o.secretNamespace,
					},
					VolumeAttributes: map[string]string{
						"bucket":    o.bucket,
						"otherOpts": "-o max_stat_cache_size=0 -o allow_other",
						"path":      o.subpath,
						"url":       o.url,
					},
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              ossStorageType,
			VolumeMode:                    func() *corev1.PersistentVolumeMode { v := corev1.PersistentVolumeFilesystem; return &v }(),
		},
	}
	return c.Create(ctx, newPV)
}

// verifyPV verifies that the existing PV has correct configuration
func (o *OSSStorage) verifyPV(pv *corev1.PersistentVolume) error {
	if o.secretName == "" || o.secretNamespace == "" {
		return fmt.Errorf("secretName/secretNamespace are not set")
	}

	if pv.Spec.CSI == nil {
		return fmt.Errorf("PV %s already exists but is not a CSI volume", pv.Name)
	}
	if pv.Spec.CSI.Driver != "ossplugin.csi.alibabacloud.com" {
		return fmt.Errorf("PV %s already exists but uses different CSI driver", pv.Name)
	}
	if pv.Spec.CSI.NodePublishSecretRef == nil {
		return fmt.Errorf("PV %s already exists but has no NodePublishSecretRef", pv.Name)
	}
	if pv.Spec.CSI.NodePublishSecretRef.Name != o.secretName {
		return fmt.Errorf("PV %s already exists but references different secret %q (expected %q)",
			pv.Name, pv.Spec.CSI.NodePublishSecretRef.Name, o.secretName)
	}
	if pv.Spec.CSI.NodePublishSecretRef.Namespace != o.secretNamespace {
		return fmt.Errorf("PV %s already exists but references secret in different namespace %q (expected %q)",
			pv.Name, pv.Spec.CSI.NodePublishSecretRef.Namespace, o.secretNamespace)
	}

	// Validate VolumeHandle matches storage name
	if pv.Spec.CSI.VolumeHandle != pv.Name {
		return fmt.Errorf("PV %s already exists but has unexpected VolumeHandle %q (expected %q)",
			pv.Name, pv.Spec.CSI.VolumeHandle, pv.Name)
	}

	// Validate OSS-specific VolumeAttributes match current config
	attrs := pv.Spec.CSI.VolumeAttributes
	if attrs == nil {
		return fmt.Errorf("PV %s already exists but has no VolumeAttributes", pv.Name)
	}
	if attrs["bucket"] != o.bucket {
		return fmt.Errorf("PV %s already exists but points to different bucket %q (expected %q)",
			pv.Name, attrs["bucket"], o.bucket)
	}
	if attrs["url"] != o.url {
		return fmt.Errorf("PV %s already exists but points to different URL %q (expected %q)",
			pv.Name, attrs["url"], o.url)
	}
	if attrs["path"] != o.subpath {
		return fmt.Errorf("PV %s already exists but points to different path %q (expected %q)",
			pv.Name, attrs["path"], o.subpath)
	}
	expectedOpts := "-o max_stat_cache_size=0 -o allow_other"
	if attrs["otherOpts"] != expectedOpts {
		return fmt.Errorf("PV %s already exists but has different otherOpts %q (expected %q)",
			pv.Name, attrs["otherOpts"], expectedOpts)
	}

	return nil
}

// createOrVerifyPVC creates the PVC or verifies it if already exists
func (o *OSSStorage) createOrVerifyPVC(ctx context.Context, c client.Client, pvcName, pvName, namespace string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	err := c.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: namespace}, pvc)
	if err == nil {
		// PVC exists, verify it
		return o.verifyPVC(pvc, pvName)
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create new PVC
	storageQuantity, err := resource.ParseQuantity(o.storageSize)
	if err != nil {
		return fmt.Errorf("invalid storageSize %q: %w", o.storageSize, err)
	}

	newPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQuantity,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"alicloud-pvname": pvName,
				},
			},
			StorageClassName: func() *string { s := ossStorageType; return &s }(),
			VolumeMode:       func() *corev1.PersistentVolumeMode { v := corev1.PersistentVolumeFilesystem; return &v }(),
			VolumeName:       pvName,
		},
	}
	return c.Create(ctx, newPVC)
}

// verifyPVC verifies that the existing PVC has correct configuration
func (o *OSSStorage) verifyPVC(pvc *corev1.PersistentVolumeClaim, pvName string) error {
	if pvc.Spec.VolumeName != pvName {
		return fmt.Errorf("PVC %s already exists but references different PV %q (expected %q)",
			pvc.Name, pvc.Spec.VolumeName, pvName)
	}
	return nil
}

// MountStorage provisions required resources (Secret, PV, PVC) and mounts the storage to the pod template
func (o *OSSStorage) MountStorage(podTemplate *corev1.PodTemplateSpec, opts MountOptions) error {
	// Provision Kubernetes resources (Secret, PV, PVC) unless dry-run or no client
	if !opts.DryRun && opts.Client != nil && opts.StorageName != "" {
		if err := o.preMount(opts.Client, opts.StorageName, opts.Namespace); err != nil {
			return fmt.Errorf("failed to prepare storage resources: %w", err)
		}
	}

	pvcName := opts.StorageName
	mountPath := opts.MountPath

	// Add volume
	volume := corev1.Volume{
		Name: "model-storage",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	}
	podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, volume)

	// Add volume mount to all containers
	volumeMount := corev1.VolumeMount{
		Name:      "model-storage",
		MountPath: mountPath,
	}

	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].VolumeMounts = append(
			podTemplate.Spec.Containers[i].VolumeMounts,
			volumeMount,
		)
	}

	// Add volume mount to init containers if any
	for i := range podTemplate.Spec.InitContainers {
		podTemplate.Spec.InitContainers[i].VolumeMounts = append(
			podTemplate.Spec.InitContainers[i].VolumeMounts,
			volumeMount,
		)
	}

	return nil
}

// PreAdd creates a Kubernetes Secret for OSS credentials and returns a modified config
// with secretName and secretNamespace instead of raw credentials.
// This is called before the storage configuration is saved to the config file.
func (o *OSSStorage) PreAdd(opts PreAddOptions) (map[string]interface{}, error) {
	ctx := context.Background()

	// Extract credentials from config
	akId, ok := opts.Config["akId"].(string)
	if !ok || akId == "" {
		return nil, fmt.Errorf("akId is required for OSS storage")
	}
	akSecret, ok := opts.Config["akSecret"].(string)
	if !ok || akSecret == "" {
		return nil, fmt.Errorf("akSecret is required for OSS storage")
	}

	// Generate secret name based on storage name
	secretName := opts.StorageName + "-oss-secret"

	// Create or update the secret
	secret := &corev1.Secret{}
	err := opts.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: opts.Namespace}, secret)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to check existing secret: %w", err)
		}
		// Secret doesn't exist, create it
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: opts.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"akId":     []byte(akId),
				"akSecret": []byte(akSecret),
			},
		}
		if err := opts.Client.Create(ctx, newSecret); err != nil {
			return nil, fmt.Errorf("failed to create secret: %w", err)
		}
	} else {
		// Secret exists, verify it has the same credentials
		existingAkId, ok1 := secret.Data["akId"]
		existingAkSecret, ok2 := secret.Data["akSecret"]
		if !ok1 || !ok2 || string(existingAkId) != akId || string(existingAkSecret) != akSecret {
			return nil, fmt.Errorf("secret %s/%s already exists with different credentials", opts.Namespace, secretName)
		}
	}

	// Build new config with secretName/secretNamespace instead of raw credentials
	newConfig := make(map[string]interface{})
	for k, v := range opts.Config {
		// Skip sensitive fields
		if k == "akId" || k == "akSecret" {
			continue
		}
		newConfig[k] = v
	}

	// Add flat secret reference fields
	newConfig["secretName"] = secretName
	newConfig["secretNamespace"] = opts.Namespace

	return newConfig, nil
}
