// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"strings"
	"testing"
)

const podsAndEventsAllHealthy = `{
  "apiVersion": "v1", "kind": "PodList",
  "items": [
    {"apiVersion": "v1", "kind": "Pod",
     "metadata": {"name": "running", "namespace": "demo",
                  "creationTimestamp": "2026-05-22T10:00:00Z"},
     "status": {"phase": "Running"}}
  ]
}`

const podStuckOnFailedMount = `{
  "apiVersion": "v1", "kind": "PodList",
  "items": [
    {"apiVersion": "v1", "kind": "Pod",
     "metadata": {"name": "stuck-storage", "namespace": "data",
                  "creationTimestamp": "2026-05-22T10:00:00Z"},
     "status": {"phase": "Pending",
                "containerStatuses": [
                  {"state": {"waiting": {"reason": "ContainerCreating"}}}
                ]}}
  ]
}`

const failedMountEvent = `{
  "apiVersion": "v1", "kind": "EventList",
  "items": [
    {"apiVersion": "v1", "kind": "Event",
     "metadata": {"name": "stuck-storage.evt", "namespace": "data"},
     "reason": "FailedMount",
     "message": "Unable to attach or mount volumes: unmounted volumes=[data]: timed out waiting for the condition",
     "involvedObject": {"kind": "Pod", "namespace": "data", "name": "stuck-storage"}}
  ]
}`

const podYoungContainerCreating = `{
  "apiVersion": "v1", "kind": "PodList",
  "items": [
    {"apiVersion": "v1", "kind": "Pod",
     "metadata": {"name": "fresh-pod", "namespace": "data",
                  "creationTimestamp": "2026-05-22T11:59:30Z"},
     "status": {"phase": "Pending",
                "containerStatuses": [
                  {"state": {"waiting": {"reason": "ContainerCreating"}}}
                ]}}
  ]
}`

const podCCNoMountEvent = `{
  "apiVersion": "v1", "kind": "PodList",
  "items": [
    {"apiVersion": "v1", "kind": "Pod",
     "metadata": {"name": "image-pulling", "namespace": "demo",
                  "creationTimestamp": "2026-05-22T10:00:00Z"},
     "status": {"phase": "Pending",
                "containerStatuses": [
                  {"state": {"waiting": {"reason": "ContainerCreating"}}}
                ]}}
  ]
}`

const provisioningFailedEvent = `{
  "apiVersion": "v1", "kind": "EventList",
  "items": [
    {"apiVersion": "v1", "kind": "Event",
     "metadata": {"name": "pvc-evt", "namespace": "data"},
     "reason": "ProvisioningFailed",
     "message": "Failed to provision volume with StorageClass \"rook-ceph-block\": rpc error: Insufficient capacity",
     "involvedObject": {"kind": "Pod", "namespace": "data", "name": "stuck-storage"}}
  ]
}`

func TestFailedMounts_AllHealthy(t *testing.T) {
	src := loadProbeSrc(t, map[string]string{"pods.json": podsAndEventsAllHealthy})
	r := FailedMounts{Now: fixedNow("2026-05-22T12:00:00Z")}.Run(context.Background(), src)
	if r.Component.Status != "HEALTHY" {
		t.Errorf("Status = %q, want HEALTHY", r.Component.Status)
	}
}

func TestFailedMounts_FailedMountEvent_Critical(t *testing.T) {
	src := loadProbeSrc(t, map[string]string{
		"pods.json":   podStuckOnFailedMount,
		"events.json": failedMountEvent,
	})
	r := FailedMounts{Now: fixedNow("2026-05-22T12:00:00Z")}.Run(context.Background(), src)
	if r.Component.Status != "CRITICAL" {
		t.Errorf("FailedMount past grace must be CRITICAL, got %q (detail=%q)",
			r.Component.Status, r.Component.Detail)
	}
	if !strings.Contains(r.Findings[0].Message, "FailedMount") {
		t.Errorf("finding message should name FailedMount: %q", r.Findings[0].Message)
	}
}

func TestFailedMounts_YoungPodSkipped(t *testing.T) {
	src := loadProbeSrc(t, map[string]string{
		"pods.json":   podYoungContainerCreating,
		"events.json": failedMountEvent,
	})
	r := FailedMounts{Now: fixedNow("2026-05-22T12:00:00Z")}.Run(context.Background(), src)
	if r.Component.Status != "HEALTHY" {
		t.Errorf("young pod should be inside grace period; got %q", r.Component.Status)
	}
}

func TestFailedMounts_ContainerCreatingWithoutMountEvent_Ignored(t *testing.T) {
	// Pod stuck ContainerCreating but no mount event → could be image pull
	// or other class. PendingPods / ImagePullAuth own those.
	src := loadProbeSrc(t, map[string]string{"pods.json": podCCNoMountEvent})
	r := FailedMounts{Now: fixedNow("2026-05-22T12:00:00Z")}.Run(context.Background(), src)
	if r.Component.Status != "HEALTHY" {
		t.Errorf("ContainerCreating without mount event should not be flagged; got %q",
			r.Component.Status)
	}
}

func TestFailedMounts_ProvisioningFailed(t *testing.T) {
	src := loadProbeSrc(t, map[string]string{
		"pods.json":   podStuckOnFailedMount,
		"events.json": provisioningFailedEvent,
	})
	r := FailedMounts{Now: fixedNow("2026-05-22T12:00:00Z")}.Run(context.Background(), src)
	if r.Component.Status != "CRITICAL" {
		t.Errorf("ProvisioningFailed should be CRITICAL, got %q", r.Component.Status)
	}
	if !strings.Contains(strings.ToLower(r.Findings[0].Remediation), "storageclass") {
		t.Errorf("remediation should mention StorageClass: %q", r.Findings[0].Remediation)
	}
}
