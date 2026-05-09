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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
)

// RBGManager handles RBG lifecycle operations (Create, WaitReady, Delete).
type RBGManager struct {
	client    client.Client
	namespace string
}

// NewRBGManager creates a new RBGManager.
func NewRBGManager(c client.Client, namespace string) *RBGManager {
	return &RBGManager{client: c, namespace: namespace}
}

// Create creates an RBG resource in the cluster.
func (m *RBGManager) Create(ctx context.Context, rbg *workloadsv1alpha2.RoleBasedGroup) error {
	rbg.Namespace = m.namespace
	if err := m.client.Create(ctx, rbg); err != nil {
		return fmt.Errorf("creating RBG %q: %w", rbg.Name, err)
	}
	return nil
}

// Delete removes an RBG resource from the cluster.
func (m *RBGManager) Delete(ctx context.Context, name string) error {
	rbg := &workloadsv1alpha2.RoleBasedGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.namespace,
		},
	}
	if err := m.client.Delete(ctx, rbg); err != nil {
		return fmt.Errorf("deleting RBG %q: %w", name, err)
	}
	return nil
}

// Get retrieves an RBG resource by name.
func (m *RBGManager) Get(ctx context.Context, name string) (*workloadsv1alpha2.RoleBasedGroup, error) {
	rbg := &workloadsv1alpha2.RoleBasedGroup{}
	key := types.NamespacedName{Name: name, Namespace: m.namespace}
	if err := m.client.Get(ctx, key, rbg); err != nil {
		return nil, fmt.Errorf("getting RBG %q: %w", name, err)
	}
	return rbg, nil
}

// WaitReady polls until the RBG has a Ready=True condition or the timeout is exceeded.
func (m *RBGManager) WaitReady(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		rbg, err := m.Get(ctx, name)
		if err != nil {
			return false, nil // retry on transient errors
		}
		return isRBGReady(rbg), nil
	})
}

// isRBGReady checks if the RBG has a Ready=True condition.
func isRBGReady(rbg *workloadsv1alpha2.RoleBasedGroup) bool {
	for _, c := range rbg.Status.Conditions {
		if c.Type == string(workloadsv1alpha2.RoleBasedGroupReady) && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
