// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

// Package v1alpha1 contains the API Schema definitions for the
// cha.bionicaisolutions.com/v1alpha1 API group.
//
// The CRDs in this group make CHA an in-cluster operator: the operator
// reconciles a ClusterHealthAutopilot resource into the existing
// watcher Deployment + diagnose CronJob + RBAC. The DriftReport CRD
// remains the read surface for active drift findings (its types live
// in pkg/snapshot for now; future PR may move them here).
//
// Phase 1 (this package) ships ONLY the type definitions — no
// controller-runtime dependency, no reconcile loop. The pure-function
// builders in internal/operator turn a ClusterHealthAutopilotSpec
// into concrete K8s manifests. Phase 2 will add the manager binary
// and the controller wiring.
//
// +kubebuilder:object:generate=true
// +groupName=cha.bionicaisolutions.com
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version for the CHA operator types.
var GroupVersion = schema.GroupVersion{
	Group:   "cha.bionicaisolutions.com",
	Version: "v1alpha1",
}

// Resource takes an unqualified resource name and returns the
// fully-qualified Group/Version/Resource. Used by the controller to
// build informer requests in Phase 2.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}
