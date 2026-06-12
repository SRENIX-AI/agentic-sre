// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	pkgsnapshot "github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/snapshot"
	"k8s.io/client-go/rest"
)

// SuppressEndpointsDeprecationWarnings re-exports the pkg/snapshot warning
// filter: drops the core/v1 Endpoints deprecation warning (CHA keeps a few
// deliberate legacy-Endpoints fallback reads) and forwards every other
// server warning to a caller-installed handler when one is set (wrapped, not
// replaced), else to a deduplicating stderr writer. See
// pkg/snapshot/warnings.go for the canonical implementation + rationale.
func SuppressEndpointsDeprecationWarnings(cfg *rest.Config) *rest.Config {
	return pkgsnapshot.SuppressEndpointsDeprecationWarnings(cfg)
}
