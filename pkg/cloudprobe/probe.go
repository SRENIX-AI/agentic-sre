// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

// Package cloudprobe defines the Probe interface cloud-resource
// probes implement. Parallel to pkg/probe.Probe but takes a
// cloud.Source instead of a snapshot.Source.
//
// Reuses probe.ComponentResult and probe.Result so downstream
// rendering (Slack, Alertmanager, DriftReport, ticketing) needs zero
// changes. Subject convention for findings:
//
//	"aws-rds/<region>/<db-id>"
//	"gcp-cloudsql/<project>/<instance>"
//	"azure-sql/<rg>/<server>/<db>"
//
// See docs/design/2026-05-cloud-probe-framework.md for the full
// design.
package cloudprobe

import (
	"context"

	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/cloud"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/probe"
)

// Probe is the cloud-resource counterpart to pkg/probe.Probe.
//
// Implementations:
//   - Inspect a single resource type (e.g., AWS RDS instances, GCP
//     Cloud SQL instances, Azure Disk attachments)
//   - Return probe.Result; findings flow through the existing
//     report.AssembleEntries path unchanged
//   - Refuse cleanly (mark themselves SKIPPED) when their required
//     cloud sub-client is nil — never panic on nil clients
//   - Are SAFE to call concurrently with other probes; per-cycle
//     rate limiting is applied at the catalog level, not per-probe
//   - Must NEVER mutate cloud resources — that lands in a separate
//     design with its own safety envelope (M4)
type Probe interface {
	// Component returns the static metadata (name, description) the
	// renderer uses to label this probe in reports.
	Component() probe.ComponentResult

	// Run inspects the cloud Source and returns zero or more findings.
	// Must not mutate. Must handle nil sub-client by returning a
	// probe.Result with status SKIPPED.
	Run(ctx context.Context, src cloud.Source) probe.Result
}
