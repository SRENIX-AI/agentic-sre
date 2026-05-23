// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

// Package aws is the AWS sub-client surface that cloud probes call.
// It is intentionally narrow — only the read operations the M1 probe
// set needs (RDS, EBS, EKS, IAM, ALB, ACM, KMS, S3, VPC). Adding a
// new resource type to the surface should be a deliberate decision,
// not an "I needed this from boto" reflex.
//
// The Client interface is implementation-agnostic — Live wraps
// aws-sdk-go-v2, Snapshot replays captured JSON, Fake (in _test.go)
// returns canned responses. Probes never import aws-sdk-go directly.
package aws

import "context"

// Client is the AWS sub-client surface. nil-return semantics:
// individual methods return (nil, nil) when the resource type is
// genuinely empty (e.g., no RDS instances in the account/region);
// they return (nil, err) when the API call failed. Probes distinguish.
//
// This interface is INTENTIONALLY incomplete — only M1 probes are
// represented. M2+ resource types extend the interface here as they
// land. Keep the surface narrow; add deliberately.
type Client interface {
	// Region returns the AWS region this client is bound to. Probes
	// use it to stamp DriftReport subjects like
	// "aws-rds/us-east-1/prod-db-1".
	Region() string

	// (Per-resource methods land here as probes are implemented.
	// See docs/design/2026-05-cloud-probe-framework.md §4 for the
	// M1 probe set: RDS, EBS, EKS, IAM, ALB, ACM, KMS, S3, VPC.
	//
	// Stub:
	//   DescribeDBInstances(ctx context.Context) ([]DBInstance, error)
	//   DescribeVolumes(ctx context.Context) ([]Volume, error)
	//   DescribeCluster(ctx context.Context, name string) (*Cluster, error)
	//   GetRole(ctx context.Context, name string) (*Role, error)
	//   DescribeTargetGroups(ctx context.Context) ([]TargetGroup, error)
	//   DescribeTargetHealth(ctx context.Context, tgArn string) ([]TargetHealth, error)
	//   DescribeCertificates(ctx context.Context) ([]Certificate, error)
	//   ListKMSKeys(ctx context.Context) ([]KMSKey, error)
	//   GetBucketPublicAccessBlock(ctx context.Context, bucket string) (*PABConfig, error)
	//   DescribeSubnets(ctx context.Context) ([]Subnet, error)
	// )
}

// Ensure the context import isn't pruned by tooling while the
// per-resource methods are commented stubs. Removed in M1 commits
// that add the first real method.
var _ = context.Background
