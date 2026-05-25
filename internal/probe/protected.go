// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package probe

// probeProtectedNamespaces mirrors the canonical no-touch list in
// internal/fix/protected.go. Kept in sync by convention; consolidation
// into a shared pkg-level helper is tracked under the Sprint 5 operator
// port (see docs/design/2026-05-hardening-plan.md).
//
// In probe-land, these namespaces aren't "untouched" — they are
// "always-critical." A CrashLoopBackOff in kube-system is more important
// than the same loop in user-ns, not less.
var probeProtectedNamespaces = map[string]struct{}{
	"kube-system":      {},
	"kube-public":      {},
	"kube-node-lease":  {},
	"rook-ceph":        {},
	"vault":            {},
	"external-secrets": {},
	"cnpg-system":      {},
	"cert-manager":     {},
}

// IsProtectedNamespace reports whether the given namespace contains
// platform-critical workloads. Used by probes to escalate severity.
func IsProtectedNamespace(ns string) bool {
	if ns == "" {
		return false
	}
	_, ok := probeProtectedNamespaces[ns]
	return ok
}
