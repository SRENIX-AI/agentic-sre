// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/cloud"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/probe"
)

// EKSControlPlane flags an EKS cluster whose status is not "ACTIVE".
// Configured via CLOUD_AWS_EKS_CLUSTER env var (set by the Helm chart
// from cloud.aws.probes.eksClusterName); SKIPPED when unset.
type EKSControlPlane struct{}

const eksClusterName = "aws-eks-control-plane"

const eksClusterEnv = "CLOUD_AWS_EKS_CLUSTER"

// Name satisfies cloudprobe.Probe.
func (EKSControlPlane) Name() string { return eksClusterName }

// Run satisfies cloudprobe.Probe.
func (EKSControlPlane) Run(ctx context.Context, src cloud.Source) probe.Result {
	awsClient := src.AWS()
	if awsClient == nil {
		return skipped(eksClusterName, "AWS not configured")
	}
	name := os.Getenv(eksClusterEnv)
	if name == "" {
		return skipped(eksClusterName, "set "+eksClusterEnv+" to enable EKS control-plane probe")
	}
	cluster, err := awsClient.DescribeEKSCluster(ctx, name)
	if err != nil {
		return probeFailed(eksClusterName, "eks.DescribeCluster", err)
	}
	subject := fmt.Sprintf("aws-eks/%s/%s", awsClient.Region(), name)
	if cluster == nil {
		return probe.Result{
			Component: probe.ComponentResult{Component: eksClusterName, Status: "CRITICAL", Detail: "cluster not found"},
			Findings: []probe.Finding{{
				Component: subject, Severity: probe.SeverityCritical,
				Message:     fmt.Sprintf("EKS cluster %q not found in region %s — drift between Helm config (cloud.aws.probes.eksClusterName) and reality", name, awsClient.Region()),
				Remediation: "Verify cluster exists: aws eks describe-cluster --name " + name,
			}},
		}
	}
	var findings []probe.Finding
	switch cluster.Status {
	case "ACTIVE":
		// healthy
	case "CREATING", "UPDATING":
		findings = append(findings, probe.Finding{
			Component: subject, Severity: probe.SeverityWarning,
			Message: fmt.Sprintf("EKS cluster %q is %s (transitional)", name, cluster.Status),
		})
	default:
		findings = append(findings, probe.Finding{
			Component: subject, Severity: probe.SeverityCritical,
			Message:     fmt.Sprintf("EKS cluster %q status is %q (not ACTIVE)", name, cluster.Status),
			Remediation: "aws eks describe-cluster --name " + name,
		})
	}
	return probe.Result{
		Component: probe.ComponentResult{
			Component: eksClusterName,
			Status:    rollupStatus(findings),
			Detail:    fmt.Sprintf("cluster %s v%s status=%s", name, cluster.Version, cluster.Status),
		},
		Findings: findings,
	}
}
