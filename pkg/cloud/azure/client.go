// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

// Package azure is the Azure sub-client surface. Scaffold only — the
// M1 release ships AWS; Azure probes land in M2 (see
// docs/design/2026-05-cloud-probe-framework.md).
//
// Mirrors the shape of pkg/cloud/aws so probes share a mental model
// across providers.
package azure

import "context"

// Client is the Azure sub-client surface. Scaffold only — extended in
// M2 with per-resource methods (Azure SQL DB, Disks, AKS control plane,
// AKS node pool, Managed identity drift, App Gateway backend, certs,
// Storage public-access, Key Vault state, VNet/subnet capacity).
type Client interface {
	// SubscriptionID returns the Azure subscription this client is bound to.
	SubscriptionID() string
}

var _ = context.Background
