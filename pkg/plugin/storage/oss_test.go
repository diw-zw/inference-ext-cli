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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testOSSConfig() map[string]interface{} {
	return map[string]interface{}{
		"url":      "oss-cn-hangzhou.aliyuncs.com",
		"bucket":   "test-bucket",
		"subpath":  "models",
		"akId":     "test-ak-id",
		"akSecret": "test-ak-secret",
	}
}

// testOSSConfigWithSecretRef returns a config with secretName/secretNamespace (as returned by PreAdd)
func testOSSConfigWithSecretRef() map[string]interface{} {
	return map[string]interface{}{
		"url":             "oss-cn-hangzhou.aliyuncs.com",
		"bucket":          "test-bucket",
		"subpath":         "models",
		"secretName":      "test-oss-oss-secret",
		"secretNamespace": "default",
	}
}

func TestOSSStorage_Name(t *testing.T) {
	p := &OSSStorage{}
	assert.Equal(t, "oss", p.Name())
}

func TestOSSStorage_ConfigFields(t *testing.T) {
	p := &OSSStorage{}
	fields := p.ConfigFields()
	require.Len(t, fields, 5)

	fieldKeys := make([]string, len(fields))
	for i, f := range fields {
		fieldKeys[i] = f.Key
	}
	assert.Contains(t, fieldKeys, "url")
	assert.Contains(t, fieldKeys, "bucket")
	assert.Contains(t, fieldKeys, "subpath")
	assert.Contains(t, fieldKeys, "akId")
	assert.Contains(t, fieldKeys, "akSecret")
	// storageSize is not a config field, it's fixed at 1Ti
	assert.NotContains(t, fieldKeys, "storageSize")

	// Check required fields:
	// - url, bucket, akId, akSecret are always required
	// - subpath is optional (akId/akSecret only needed during add-storage)
	requiredFields := map[string]bool{"url": true, "bucket": true, "akId": true, "akSecret": true}
	for _, f := range fields {
		if requiredFields[f.Key] {
			assert.True(t, f.Required, "field %s should be required", f.Key)
		} else {
			assert.False(t, f.Required, "field %s should be optional", f.Key)
		}
	}
}

func TestOSSStorage_Init_MissingRequiredFields(t *testing.T) {
	requiredFields := []string{"url", "bucket"}

	for _, field := range requiredFields {
		t.Run("missing_"+field, func(t *testing.T) {
			config := testOSSConfigWithSecretRef()
			delete(config, field)
			p := &OSSStorage{}
			err := p.Init(config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), field)
		})
	}
}

func TestOSSStorage_Init_EmptyRequiredFields(t *testing.T) {
	requiredFields := []string{"url", "bucket"}

	for _, field := range requiredFields {
		t.Run("empty_"+field, func(t *testing.T) {
			config := testOSSConfigWithSecretRef()
			config[field] = ""
			p := &OSSStorage{}
			err := p.Init(config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), field)
		})
	}
}

func TestOSSStorage_Init_MissingSecretRef(t *testing.T) {
	config := testOSSConfig() // config without secretName/secretNamespace
	p := &OSSStorage{}
	err := p.Init(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secretName is required")
}

func TestOSSStorage_Init_OK_WithSecretRef(t *testing.T) {
	config := map[string]interface{}{
		"url":             "oss-cn-hangzhou.aliyuncs.com",
		"bucket":          "test-bucket",
		"subpath":         "models",
		"secretName":      "test-secret",
		"secretNamespace": "default",
	}
	p := &OSSStorage{}
	err := p.Init(config)
	require.NoError(t, err)
	assert.Equal(t, "1Ti", p.storageSize)
	assert.Equal(t, "oss-cn-hangzhou.aliyuncs.com", p.url)
	assert.Equal(t, "test-bucket", p.bucket)
	assert.Equal(t, "models", p.subpath)
	assert.Equal(t, "test-secret", p.secretName)
	assert.Equal(t, "default", p.secretNamespace)
}

func TestOSSStorage_Init_OK_WithoutSubpath(t *testing.T) {
	config := testOSSConfigWithSecretRef()
	delete(config, "subpath")
	p := &OSSStorage{}
	err := p.Init(config)
	require.NoError(t, err)
	assert.Equal(t, "/", p.subpath)
}

func TestOSSStorage_Init_StorageSizeFixed(t *testing.T) {
	// storageSize is fixed at 1Ti, even if specified in config it's ignored
	config := testOSSConfigWithSecretRef()
	config["storageSize"] = "500Gi" // This should be ignored
	p := &OSSStorage{}
	err := p.Init(config)
	require.NoError(t, err)
	assert.Equal(t, "1Ti", p.storageSize, "storageSize should always be 1Ti")
}

// testOSSWithSecretRef creates an OSSStorage with secretName/secretNamespace for testing
func testOSSWithSecretRef() *OSSStorage {
	return &OSSStorage{
		storageSize:     "100Gi",
		url:             "oss-cn-hangzhou.aliyuncs.com",
		bucket:          "test-bucket",
		subpath:         "models",
		secretName:      "test-oss-oss-secret",
		secretNamespace: "default",
	}
}

func TestOSSStorage_MountStorage_AddsVolumeAndMount(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create the secret that PreAdd would have created
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		},
	}

	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.NoError(t, err)

	require.Len(t, tpl.Spec.Volumes, 1)
	vol := tpl.Spec.Volumes[0]
	assert.Equal(t, "model-storage", vol.Name)
	require.NotNil(t, vol.VolumeSource.PersistentVolumeClaim)
	assert.Equal(t, "test-oss", vol.VolumeSource.PersistentVolumeClaim.ClaimName)

	require.Len(t, tpl.Spec.Containers[0].VolumeMounts, 1)
	vm := tpl.Spec.Containers[0].VolumeMounts[0]
	assert.Equal(t, "model-storage", vm.Name)
	assert.Equal(t, "/models", vm.MountPath)
}

func TestOSSStorage_MountStorage_MultipleContainers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c1"},
				{Name: "c2"},
			},
		},
	}

	require.NoError(t, p.MountStorage(tpl, MountOptions{Client: fakeClient, StorageName: "test-oss", Namespace: "default", MountPath: DefaultMountPath}))
	for _, c := range tpl.Spec.Containers {
		require.Len(t, c.VolumeMounts, 1)
		assert.Equal(t, "model-storage", c.VolumeMounts[0].Name)
	}
}

func TestOSSStorage_MountStorage_InitContainers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init"},
			},
			Containers: []corev1.Container{
				{Name: "main"},
			},
		},
	}

	require.NoError(t, p.MountStorage(tpl, MountOptions{Client: fakeClient, StorageName: "test-oss", Namespace: "default", MountPath: DefaultMountPath}))
	require.Len(t, tpl.Spec.InitContainers[0].VolumeMounts, 1)
	assert.Equal(t, "/models", tpl.Spec.InitContainers[0].VolumeMounts[0].MountPath)
}

func TestOSSStorage_MountStorage_CreatesResources(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create the secret that PreAdd would have created
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.NoError(t, err)

	// Verify PV was created
	pv := &corev1.PersistentVolume{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-oss", Namespace: "default"}, pv)
	require.NoError(t, err)
	assert.Equal(t, "ossplugin.csi.alibabacloud.com", pv.Spec.CSI.Driver)
	assert.Equal(t, "test-oss-oss-secret", pv.Spec.CSI.NodePublishSecretRef.Name)
	assert.Equal(t, "default", pv.Spec.CSI.NodePublishSecretRef.Namespace)
	assert.Equal(t, "test-bucket", pv.Spec.CSI.VolumeAttributes["bucket"])
	assert.Equal(t, "models", pv.Spec.CSI.VolumeAttributes["path"])
	assert.Equal(t, "oss-cn-hangzhou.aliyuncs.com", pv.Spec.CSI.VolumeAttributes["url"])

	// Verify PVC was created
	pvc := &corev1.PersistentVolumeClaim{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-oss", Namespace: "default"}, pvc)
	require.NoError(t, err)
	assert.Equal(t, "test-oss", pvc.Spec.VolumeName)
	assert.Equal(t, "oss", *pvc.Spec.StorageClassName)

	// Verify volume was added to pod template
	require.Len(t, tpl.Spec.Volumes, 1)
	assert.Equal(t, "test-oss", tpl.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName)
}

func TestOSSStorage_MountStorage_VerifiesExistingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create existing secret with matching credentials
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSecret).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.NoError(t, err)
}

func TestOSSStorage_MountStorage_FailsOnMissingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Don't create the secret - it should fail because secret is expected to exist
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestOSSStorage_MountStorage_VerifiesExistingPV(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	storageQuantity := resource.MustParse("100Gi")
	volumeMode := corev1.PersistentVolumeFilesystem

	// Create existing secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}

	// Create existing PV with correct config
	existingPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-oss",
			Labels: map[string]string{
				"alicloud-pvname": "test-oss",
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
					VolumeHandle: "test-oss",
					NodePublishSecretRef: &corev1.SecretReference{
						Name:      "test-oss-oss-secret",
						Namespace: "default",
					},
					VolumeAttributes: map[string]string{
						"bucket":    "test-bucket",
						"otherOpts": "-o max_stat_cache_size=0 -o allow_other",
						"path":      "models",
						"url":       "oss-cn-hangzhou.aliyuncs.com",
					},
				},
			},
			StorageClassName: "oss",
			VolumeMode:       &volumeMode,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, existingPV).Build()

	p := testOSSWithSecretRef()

	// MountStorage should succeed because PV exists with correct config
	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.NoError(t, err)
}

func TestOSSStorage_MountStorage_VerifiesExistingPVC(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	storageQuantity := resource.MustParse("100Gi")
	volumeMode := corev1.PersistentVolumeFilesystem
	storageClassName := "oss"

	// Create all resources with correct config
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-oss",
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "ossplugin.csi.alibabacloud.com",
					VolumeHandle: "test-oss",
					NodePublishSecretRef: &corev1.SecretReference{
						Name:      "test-oss-oss-secret",
						Namespace: "default",
					},
					VolumeAttributes: map[string]string{
						"bucket":    "test-bucket",
						"otherOpts": "-o max_stat_cache_size=0 -o allow_other",
						"path":      "models",
						"url":       "oss-cn-hangzhou.aliyuncs.com",
					},
				},
			},
		},
	}

	// Create existing PVC with correct config
	existingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQuantity,
				},
			},
			VolumeName:       "test-oss",
			StorageClassName: &storageClassName,
			VolumeMode:       &volumeMode,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, pv, existingPVC).Build()

	p := testOSSWithSecretRef()

	// MountStorage should succeed because all resources exist with correct config
	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.NoError(t, err)
}

func TestOSSStorage_MountStorage_FailsOnDifferentPVC(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	storageQuantity := resource.MustParse("100Gi")
	volumeMode := corev1.PersistentVolumeFilesystem
	storageClassName := "oss"

	// Create all required resources but PVC references different PV
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-oss",
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "ossplugin.csi.alibabacloud.com",
					VolumeHandle: "test-oss",
					NodePublishSecretRef: &corev1.SecretReference{
						Name:      "test-oss-oss-secret",
						Namespace: "default",
					},
					VolumeAttributes: map[string]string{
						"bucket":    "test-bucket",
						"otherOpts": "-o max_stat_cache_size=0 -o allow_other",
						"path":      "models",
						"url":       "oss-cn-hangzhou.aliyuncs.com",
					},
				},
			},
		},
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName:       "different-pv",
			StorageClassName: &storageClassName,
			VolumeMode:       &volumeMode,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQuantity,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, pv, pvc).Build()

	p := testOSSWithSecretRef()

	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different PV")
}

func TestGet_OSS_RequiresConfig(t *testing.T) {
	_, err := Get("oss", map[string]interface{}{})
	require.Error(t, err)
}

func TestGet_OSS_OK(t *testing.T) {
	p, err := Get("oss", testOSSConfigWithSecretRef())
	require.NoError(t, err)
	assert.Equal(t, "oss", p.Name())
}

func TestValidateConfig_OSS_MissingRequired(t *testing.T) {
	err := ValidateConfig("oss", map[string]interface{}{})
	require.Error(t, err)
}

func TestValidateConfig_OSS_OK(t *testing.T) {
	err := ValidateConfig("oss", testOSSConfig())
	assert.NoError(t, err)
}

func TestValidateConfig_OSS_OK_WithoutSubpath(t *testing.T) {
	config := testOSSConfig()
	delete(config, "subpath")
	err := ValidateConfig("oss", config)
	assert.NoError(t, err)
}

func TestValidateConfig_OSS_UnknownField(t *testing.T) {
	config := testOSSConfig()
	config["bad"] = "x"
	err := ValidateConfig("oss", config)
	assert.Error(t, err)
}

func TestGetFields_OSS(t *testing.T) {
	fields := GetFields("oss")
	require.NotNil(t, fields)
	assert.Len(t, fields, 5) // storageSize is not a config field
}

func TestRegisteredNames_ContainsOSS(t *testing.T) {
	names := RegisteredNames()
	assert.Contains(t, names, "oss")
}

// Mock client that returns error on Get
type errorClient struct {
	client.Client
	err error
}

func (e *errorClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	return e.err
}

func TestOSSStorage_MountStorage_ClientError(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	p := testOSSWithSecretRef()

	// Use a mock client that returns a non-NotFound error
	mockClient := &errorClient{err: errors.NewInternalError(fmt.Errorf("internal error"))}
	tpl := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	err := p.MountStorage(tpl, MountOptions{
		Client:      mockClient,
		StorageName: "test-oss",
		Namespace:   "default",
		MountPath:   DefaultMountPath,
	})
	require.Error(t, err)
}

// PreAdd tests
func TestOSSStorage_PreAdd_CreatesSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	p := &OSSStorage{}
	config := testOSSConfig()

	opts := PreAddOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		Config:      config,
	}

	modifiedConfig, err := p.PreAdd(opts)
	require.NoError(t, err)

	// Verify secret was created
	secret := &corev1.Secret{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-oss-oss-secret", Namespace: "default"}, secret)
	require.NoError(t, err)
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
	assert.Equal(t, []byte("test-ak-id"), secret.Data["akId"])
	assert.Equal(t, []byte("test-ak-secret"), secret.Data["akSecret"])

	// Verify modified config has secretName/secretNamespace and no credentials
	assert.Nil(t, modifiedConfig["akId"], "akId should be removed from config")
	assert.Nil(t, modifiedConfig["akSecret"], "akSecret should be removed from config")

	assert.Equal(t, "test-oss-oss-secret", modifiedConfig["secretName"], "secretName should be present in modified config")
	assert.Equal(t, "default", modifiedConfig["secretNamespace"], "secretNamespace should be present in modified config")

	// Verify other fields are preserved
	assert.Equal(t, "oss-cn-hangzhou.aliyuncs.com", modifiedConfig["url"])
	assert.Equal(t, "test-bucket", modifiedConfig["bucket"])
	assert.Equal(t, "models", modifiedConfig["subpath"])
}

func TestOSSStorage_PreAdd_VerifiesExistingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create existing secret with matching credentials
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("test-ak-id"),
			"akSecret": []byte("test-ak-secret"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSecret).Build()

	p := &OSSStorage{}
	config := testOSSConfig()

	opts := PreAddOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		Config:      config,
	}

	modifiedConfig, err := p.PreAdd(opts)
	require.NoError(t, err)

	// Verify modified config has secretName/secretNamespace
	assert.Equal(t, "test-oss-oss-secret", modifiedConfig["secretName"])
	assert.Equal(t, "default", modifiedConfig["secretNamespace"])
}

func TestOSSStorage_PreAdd_FailsOnDifferentSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create existing secret with different credentials
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oss-oss-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akId":     []byte("different-ak-id"),
			"akSecret": []byte("different-ak-secret"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSecret).Build()

	p := &OSSStorage{}
	config := testOSSConfig()

	opts := PreAddOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		Config:      config,
	}

	_, err := p.PreAdd(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists with different credentials")
}

func TestOSSStorage_PreAdd_MissingCredentials(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	p := &OSSStorage{}
	config := map[string]interface{}{
		"url":    "oss-cn-hangzhou.aliyuncs.com",
		"bucket": "test-bucket",
		// missing akId and akSecret
	}

	opts := PreAddOptions{
		Client:      fakeClient,
		StorageName: "test-oss",
		Namespace:   "default",
		Config:      config,
	}

	_, err := p.PreAdd(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "akId is required")
}
