// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/internal/diagnose"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/internal/fix"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/internal/probe"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/internal/snapshot"
)

// fakeMutator records calls and lets the test inspect the create/patch/delete diff.
type fakeMutator struct {
	calls []string
}

func (f *fakeMutator) Delete(_ context.Context, gvr schema.GroupVersionResource, ns, name string) error {
	f.calls = append(f.calls, fmt.Sprintf("Delete %s/%s/%s", gvr.Resource, ns, name))
	return nil
}
func (f *fakeMutator) Patch(_ context.Context, gvr schema.GroupVersionResource, ns, name string, _ types.PatchType, _ []byte) error {
	f.calls = append(f.calls, fmt.Sprintf("Patch %s/%s/%s", gvr.Resource, ns, name))
	return nil
}
func (f *fakeMutator) Create(_ context.Context, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured) error {
	f.calls = append(f.calls, fmt.Sprintf("Create %s/%s/%s", gvr.Resource, ns, obj.GetName()))
	return nil
}

// fakeSrc is a minimal Source that returns a fixed list for GVRDriftReport.
type fakeSrc struct {
	existing []unstructured.Unstructured
}

func (f *fakeSrc) List(_ context.Context, gvr schema.GroupVersionResource, _ string) (*unstructured.UnstructuredList, error) {
	if gvr == snapshot.GVRDriftReport {
		return &unstructured.UnstructuredList{Items: f.existing}, nil
	}
	return &unstructured.UnstructuredList{}, nil
}
func (f *fakeSrc) Get(_ context.Context, _ schema.GroupVersionResource, _, _ string) (*unstructured.Unstructured, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeSrc) Mode() snapshot.Mode { return snapshot.ModeLive }

func driftCR(subject string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "cha.bionicaisolutions.com/v1alpha1",
			"kind":       "DriftReport",
			"metadata":   map[string]any{"name": nameForSubject(subject)},
			"spec":       map[string]any{"subject": subject},
		},
	}
}

func TestAssembleEntries(t *testing.T) {
	results := []probe.Result{{
		Component: probe.ComponentResult{Component: "Postgres", Status: "CRITICAL"},
		Findings: []probe.Finding{{
			Component: "Postgres (pg/main)",
			Severity:  probe.SeverityCritical,
			Message:   "phase=Failover",
		}},
	}}
	diag := []diagnose.Diagnostic{{Subject: "Secret/x/y/k", Message: "missing key"}}
	fixR := []fix.Result{{Fixer: "StaleErrorPods", Actions: []fix.Action{{Description: "deleted", Object: "Pod/ns/name"}}}}

	entries := AssembleEntries(results, diag, fixR)
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	cats := []string{}
	for _, e := range entries {
		cats = append(cats, e.Category)
	}
	sort.Strings(cats)
	want := []string{"analyzer", "fixer-action", "probe"}
	for i, w := range want {
		if cats[i] != w {
			t.Errorf("entry[%d] category=%q, want %q", i, cats[i], w)
		}
	}
}

func TestReconcile_CreateNew(t *testing.T) {
	src := &fakeSrc{}
	m := &fakeMutator{}
	entries := []DriftReportEntry{{Subject: "Secret/x/y/k", Severity: "warning", Source: "an", Category: "analyzer", Message: "msg"}}
	c, u, d, err := Reconcile(context.Background(), src, m, entries, "run-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c != 1 || u != 0 || d != 0 {
		t.Errorf("want c=1 u=0 d=0; got c=%d u=%d d=%d", c, u, d)
	}
	// Should have one Create + one Patch (status stamp).
	if len(m.calls) != 2 {
		t.Fatalf("want 2 calls (create+status patch), got %d: %v", len(m.calls), m.calls)
	}
	if m.calls[0][:6] != "Create" {
		t.Errorf("first call should be Create, got: %s", m.calls[0])
	}
}

func TestReconcile_UpdateExisting(t *testing.T) {
	subj := "Secret/x/y/k"
	src := &fakeSrc{existing: []unstructured.Unstructured{driftCR(subj)}}
	m := &fakeMutator{}
	entries := []DriftReportEntry{{Subject: subj, Severity: "warning", Source: "an", Category: "analyzer", Message: "msg"}}
	c, u, d, err := Reconcile(context.Background(), src, m, entries, "run-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c != 0 || u != 1 || d != 0 {
		t.Errorf("want c=0 u=1 d=0; got c=%d u=%d d=%d", c, u, d)
	}
	// Should be one Patch (status update only — no Create).
	if len(m.calls) != 1 {
		t.Errorf("want 1 patch, got %d: %v", len(m.calls), m.calls)
	}
}

func TestReconcile_DeleteResolved(t *testing.T) {
	subj := "Secret/x/y/k"
	src := &fakeSrc{existing: []unstructured.Unstructured{driftCR(subj)}}
	m := &fakeMutator{}
	c, u, d, err := Reconcile(context.Background(), src, m, nil, "run-3")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c != 0 || u != 0 || d != 1 {
		t.Errorf("want c=0 u=0 d=1; got c=%d u=%d d=%d", c, u, d)
	}
	if len(m.calls) != 1 || m.calls[0][:6] != "Delete" {
		t.Errorf("want 1 delete, got %v", m.calls)
	}
}

func TestReconcile_RefusesWithoutMutator(t *testing.T) {
	_, _, _, err := Reconcile(context.Background(), &fakeSrc{}, nil, nil, "x")
	if err == nil {
		t.Errorf("expected error when Mutator is nil")
	}
}

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"Postgres":             "postgres",
		"Service: LiveKit SIP": "service--livekit-sip",
		"":                     "unknown",
		"AAA-bbb_ccc":          "aaa-bbb-ccc",
	}
	for in, want := range cases {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
