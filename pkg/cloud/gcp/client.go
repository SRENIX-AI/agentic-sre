// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

// Package gcp is the GCP sub-client surface. Scaffold only — the M1
// release ships AWS; GCP probes land in M2 (see
// docs/design/2026-05-cloud-probe-framework.md).
//
// Mirrors the shape of pkg/cloud/aws so probes share a mental model
// across providers.
package gcp

import "context"

// Client is the GCP sub-client surface. Scaffold only — extended in
// M2 with per-resource methods (Cloud SQL, Persistent Disk, GKE
// control plane, GKE node pool, IAM service-account bindings, LB
// backend health, Google-managed certs, GCS public-access, KMS state,
// subnet capacity).
type Client interface {
	// Project returns the GCP project this client is bound to.
	Project() string
}

var _ = context.Background
