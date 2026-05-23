// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/cloud"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/probe"
)

// EKSNodeGroups flags node-group health issues + non-ACTIVE statuses.
// Reuses CLOUD_AWS_EKS_CLUSTER from the control-plane probe.
type EKSNodeGroups struct{}

const eksNGName = "aws-eks-nodegroups"

// Name satisfies cloudprobe.Probe.
func (EKSNodeGroups) Name() string { return eksNGName }

// Run satisfies cloudprobe.Probe.
func (EKSNodeGroups) Run(ctx context.Context, src cloud.Source) probe.Result {
	awsClient := src.AWS()
	if awsClient == nil {
		return skipped(eksNGName, "AWS not configured")
	}
	name := os.Getenv(eksClusterEnv)
	if name == "" {
		return skipped(eksNGName, "set "+eksClusterEnv+" to enable EKS node-group probe")
	}
	ngs, err := awsClient.ListEKSNodeGroups(ctx, name)
	if err != nil {
		return probeFailed(eksNGName, "eks.ListNodegroups", err)
	}
	var findings []probe.Finding
	for _, ng := range ngs {
		subject := fmt.Sprintf("aws-eks-nodegroup/%s/%s/%s", awsClient.Region(), name, ng.Name)
		switch ng.Status {
		case "ACTIVE":
			// healthy
		case "CREATING", "UPDATING":
			findings = append(findings, probe.Finding{
				Component: subject, Severity: probe.SeverityWarning,
				Message: fmt.Sprintf("node group %q is %s (transitional)", ng.Name, ng.Status),
			})
		case "DEGRADED":
			findings = append(findings, probe.Finding{
				Component: subject, Severity: probe.SeverityCritical,
				Message:     fmt.Sprintf("node group %q is DEGRADED (issues: %s)", ng.Name, strings.Join(ng.HealthIssues, ", ")),
				Remediation: fmt.Sprintf("aws eks describe-nodegroup --cluster-name %s --nodegroup-name %s", name, ng.Name),
			})
		default:
			findings = append(findings, probe.Finding{
				Component: subject, Severity: probe.SeverityCritical,
				Message: fmt.Sprintf("node group %q status=%q (not ACTIVE; issues: %s)", ng.Name, ng.Status, strings.Join(ng.HealthIssues, ", ")),
			})
		}
		if len(ng.HealthIssues) > 0 && ng.Status == "ACTIVE" {
			findings = append(findings, probe.Finding{
				Component: subject, Severity: probe.SeverityWarning,
				Message: fmt.Sprintf("node group %q has health issues despite ACTIVE: %s", ng.Name, strings.Join(ng.HealthIssues, ", ")),
			})
		}
	}
	return probe.Result{
		Component: probe.ComponentResult{
			Component: eksNGName,
			Status:    rollupStatus(findings),
			Detail:    fmt.Sprintf("%d node group(s) in cluster %s", len(ngs), name),
		},
		Findings: findings,
	}
}
