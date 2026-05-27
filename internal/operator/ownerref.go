// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	chav1alpha1 "github.com/Bionic-AI-Solutions/cluster-health-autopilot/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// controllerutilHelper installs the CR as the controller owner of the
// child object. Wrapper keeps the controllerutil import scoped to one
// file so the rest of the package can stay free of it.
func controllerutilHelper(cr *chav1alpha1.ClusterHealthAutopilot, child client.Object, scheme *runtime.Scheme) error {
	return controllerutil.SetControllerReference(cr, child, scheme)
}
