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

package svc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
)

// --- newDeleteCmd: command metadata ---

func TestNewDeleteCmd_UseAndShort(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)
	cmd := newDeleteCmd(cf)
	assert.Equal(t, "delete [name...] [flags]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestNewDeleteCmd_Flags(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)
	cmd := newDeleteCmd(cf)
	assert.Nil(t, cmd.Flags().Lookup("yes"), "yes flag should not exist after confirm removal")
}

// --- delete logic via fake client ---

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = workloadsv1alpha2.AddToScheme(scheme)
	return scheme
}

func newFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(objs...).WithStatusSubresource(&workloadsv1alpha2.RoleBasedGroup{}).Build()
}

func makeTestRBG(name, namespace string) *workloadsv1alpha2.RoleBasedGroup {
	return &workloadsv1alpha2.RoleBasedGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"workloads.x-k8s.io/source": "rbgcli",
			},
		},
	}
}

func TestDeleteFakeClient_DeleteSingleRBG(t *testing.T) {
	rbg := makeTestRBG("my-svc", "default")
	cs := newFakeClient(rbg)

	// verify it exists
	list := &workloadsv1alpha2.RoleBasedGroupList{}
	err := cs.List(t.Context(), list, client.InNamespace("default"))
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	// delete it
	obj := &workloadsv1alpha2.RoleBasedGroup{}
	err = cs.Get(t.Context(), types.NamespacedName{Name: "my-svc", Namespace: "default"}, obj)
	require.NoError(t, err)
	err = cs.Delete(t.Context(), obj)
	require.NoError(t, err)

	// verify it's gone
	list = &workloadsv1alpha2.RoleBasedGroupList{}
	err = cs.List(t.Context(), list, client.InNamespace("default"))
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestDeleteFakeClient_DeleteNonExistent_Errors(t *testing.T) {
	cs := newFakeClient()
	obj := &workloadsv1alpha2.RoleBasedGroup{}
	err := cs.Get(t.Context(), types.NamespacedName{Name: "does-not-exist", Namespace: "default"}, obj)
	require.Error(t, err)
}

func TestDeleteFakeClient_ListAndDeleteAll(t *testing.T) {
	rbg1 := makeTestRBG("svc-a", "default")
	rbg2 := makeTestRBG("svc-b", "default")
	cs := newFakeClient(rbg1, rbg2)

	list := &workloadsv1alpha2.RoleBasedGroupList{}
	err := cs.List(t.Context(), list, client.InNamespace("default"))
	require.NoError(t, err)
	require.Len(t, list.Items, 2)

	for i := range list.Items {
		err := cs.Delete(t.Context(), &list.Items[i])
		require.NoError(t, err)
	}

	list = &workloadsv1alpha2.RoleBasedGroupList{}
	err = cs.List(t.Context(), list, client.InNamespace("default"))
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestDeleteFakeClient_DeleteAcrossNamespaces(t *testing.T) {
	rbg1 := makeTestRBG("svc-a", "ns1")
	rbg2 := makeTestRBG("svc-b", "ns2")
	cs := newFakeClient(rbg1, rbg2)

	obj := &workloadsv1alpha2.RoleBasedGroup{}
	err := cs.Get(t.Context(), types.NamespacedName{Name: "svc-a", Namespace: "ns1"}, obj)
	require.NoError(t, err)
	err = cs.Delete(t.Context(), obj)
	require.NoError(t, err)

	remaining := &workloadsv1alpha2.RoleBasedGroupList{}
	err = cs.List(t.Context(), remaining, client.InNamespace("ns2"))
	require.NoError(t, err)
	require.Len(t, remaining.Items, 1)
	assert.Equal(t, "svc-b", remaining.Items[0].Name)
}
