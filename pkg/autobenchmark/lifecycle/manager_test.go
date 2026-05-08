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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = workloadsv1alpha2.AddToScheme(s)
	return s
}

func makeRBG(name, namespace string) *workloadsv1alpha2.RoleBasedGroup {
	replicas := int32(1)
	return &workloadsv1alpha2.RoleBasedGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: workloadsv1alpha2.RoleBasedGroupSpec{
			Roles: []workloadsv1alpha2.RoleSpec{
				{
					Name:     "worker",
					Replicas: &replicas,
				},
			},
		},
	}
}

func TestRBGManager_Create(t *testing.T) {
	scheme := newTestScheme()
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := NewRBGManager(fc, "test-ns")

	rbg := makeRBG("my-rbg", "")
	err := mgr.Create(context.Background(), rbg)
	require.NoError(t, err)

	// Verify the object exists in the fake client
	got, err := mgr.Get(context.Background(), "my-rbg")
	require.NoError(t, err)
	assert.Equal(t, "my-rbg", got.Name)
	assert.Equal(t, "test-ns", got.Namespace)
}

func TestRBGManager_Delete(t *testing.T) {
	scheme := newTestScheme()
	rbg := makeRBG("to-delete", "test-ns")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rbg).Build()
	mgr := NewRBGManager(fc, "test-ns")

	err := mgr.Delete(context.Background(), "to-delete")
	require.NoError(t, err)

	// Verify the object is gone
	_, err = mgr.Get(context.Background(), "to-delete")
	require.Error(t, err)
}

func TestRBGManager_Get(t *testing.T) {
	scheme := newTestScheme()
	rbg := makeRBG("existing", "test-ns")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rbg).Build()
	mgr := NewRBGManager(fc, "test-ns")

	got, err := mgr.Get(context.Background(), "existing")
	require.NoError(t, err)
	assert.Equal(t, "existing", got.Name)

	_, err = mgr.Get(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestRBGManager_WaitReady_AlreadyReady(t *testing.T) {
	scheme := newTestScheme()
	rbg := makeRBG("ready-rbg", "test-ns")
	rbg.Status.Conditions = []metav1.Condition{
		{
			Type:   string(workloadsv1alpha2.RoleBasedGroupReady),
			Status: metav1.ConditionTrue,
		},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rbg).WithStatusSubresource(rbg).Build()
	mgr := NewRBGManager(fc, "test-ns")

	err := mgr.WaitReady(context.Background(), "ready-rbg", 5*time.Second)
	require.NoError(t, err)
}

func TestRBGManager_WaitReady_Timeout(t *testing.T) {
	scheme := newTestScheme()
	rbg := makeRBG("not-ready", "test-ns")
	// No Ready condition set → WaitReady should timeout
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rbg).Build()
	mgr := NewRBGManager(fc, "test-ns")

	err := mgr.WaitReady(context.Background(), "not-ready", 1*time.Second)
	require.Error(t, err)
}

func TestIsRBGReady(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       bool
	}{
		{
			name:       "no conditions",
			conditions: nil,
			want:       false,
		},
		{
			name: "ready true",
			conditions: []metav1.Condition{
				{Type: string(workloadsv1alpha2.RoleBasedGroupReady), Status: metav1.ConditionTrue},
			},
			want: true,
		},
		{
			name: "ready false",
			conditions: []metav1.Condition{
				{Type: string(workloadsv1alpha2.RoleBasedGroupReady), Status: metav1.ConditionFalse},
			},
			want: false,
		},
		{
			name: "other condition only",
			conditions: []metav1.Condition{
				{Type: string(workloadsv1alpha2.RoleBasedGroupProgressing), Status: metav1.ConditionTrue},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rbg := &workloadsv1alpha2.RoleBasedGroup{
				Status: workloadsv1alpha2.RoleBasedGroupStatus{
					Conditions: tt.conditions,
				},
			}
			assert.Equal(t, tt.want, isRBGReady(rbg))
		})
	}
}
