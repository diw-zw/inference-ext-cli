package util

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func GetRbgGVK() schema.GroupVersionKind {
	return schema.FromAPIVersionAndKind("workloads.x-k8s.io/v1alpha2", "RoleBasedGroup")
}
