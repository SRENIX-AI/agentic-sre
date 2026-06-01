// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package netpol

import (
	"context"
	"testing"

	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/internal/snapshot"
	pkgsnapshot "github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/snapshot"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// memSourceNet is a deterministic in-memory snapshot.Source for CNI
// detection tests. Keyed by GVR.Resource (matches the convention used
// in internal/diagnose tests).
type memSourceNet struct {
	byResource map[string][]unstructured.Unstructured
}

func (m *memSourceNet) List(_ context.Context, gvr schema.GroupVersionResource, _ string) (*unstructured.UnstructuredList, error) {
	out := &unstructured.UnstructuredList{}
	out.Items = append(out.Items, m.byResource[gvr.Resource]...)
	return out, nil
}
func (m *memSourceNet) Get(_ context.Context, gvr schema.GroupVersionResource, _, name string) (*unstructured.Unstructured, error) {
	for _, u := range m.byResource[gvr.Resource] {
		if u.GetName() == name {
			c := u.DeepCopy()
			return c, nil
		}
	}
	return nil, nil
}
func (m *memSourceNet) Mode() pkgsnapshot.Mode { return pkgsnapshot.ModeLive }

func makeDS(ns, name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("apps/v1")
	u.SetKind("DaemonSet")
	u.SetNamespace(ns)
	u.SetName(name)
	return u
}

func TestDetectCNI_FlannelOnly(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {makeDS("kube-flannel", "kube-flannel-ds")},
	}}
	d := DetectCNI(context.Background(), src)
	if d.Enforces {
		t.Errorf("Flannel-only must NOT enforce; got %+v", d)
	}
	if d.CNIName != "flannel-only" {
		t.Errorf("CNIName=%q want flannel-only", d.CNIName)
	}
}

func TestDetectCNI_Calico(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {
			makeDS("kube-flannel", "kube-flannel-ds"), // Flannel + Calico = Calico wins
			makeDS("calico-system", "calico-node"),
		},
	}}
	d := DetectCNI(context.Background(), src)
	if !d.Enforces {
		t.Errorf("Calico must enforce; got %+v", d)
	}
	if d.CNIName != "calico" {
		t.Errorf("CNIName=%q want calico", d.CNIName)
	}
}

func TestDetectCNI_Cilium(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {makeDS("kube-system", "cilium")},
	}}
	d := DetectCNI(context.Background(), src)
	if !d.Enforces {
		t.Errorf("Cilium must enforce; got %+v", d)
	}
	if d.CNIName != "cilium" {
		t.Errorf("CNIName=%q want cilium", d.CNIName)
	}
}

func TestDetectCNI_AWSVPCCNI(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {makeDS("kube-system", "aws-node")},
	}}
	d := DetectCNI(context.Background(), src)
	if !d.Enforces {
		t.Errorf("AWS VPC CNI must enforce (caveat in evidence); got %+v", d)
	}
	if d.CNIName != "aws-vpc-cni" {
		t.Errorf("CNIName=%q want aws-vpc-cni", d.CNIName)
	}
}

func TestDetectCNI_GKEDataplaneV2(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {makeDS("kube-system", "anetd")},
	}}
	d := DetectCNI(context.Background(), src)
	if !d.Enforces || d.CNIName != "gke-dataplane-v2" {
		t.Errorf("expected gke-dataplane-v2; got %+v", d)
	}
}

func TestDetectCNI_AzureCNI(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {makeDS("kube-system", "azure-npm")},
	}}
	d := DetectCNI(context.Background(), src)
	if !d.Enforces || d.CNIName != "azure-cni" {
		t.Errorf("expected azure-cni; got %+v", d)
	}
}

func TestDetectCNI_Unknown(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{
		"daemonsets": {makeDS("kube-system", "kube-proxy")}, // not a CNI signal
	}}
	d := DetectCNI(context.Background(), src)
	if d.Enforces {
		t.Errorf("unknown CNI must NOT enforce; got %+v", d)
	}
	if d.CNIName != "unknown" {
		t.Errorf("CNIName=%q want unknown", d.CNIName)
	}
}

func TestDetectCNI_EmptyCluster(t *testing.T) {
	src := &memSourceNet{byResource: map[string][]unstructured.Unstructured{}}
	d := DetectCNI(context.Background(), src)
	if d.Enforces {
		t.Errorf("empty cluster must NOT enforce")
	}
}

// Make sure snapshot.GVRDaemonSet matches our memSourceNet key.
func TestGVRDaemonSetResourceKey(t *testing.T) {
	if snapshot.GVRDaemonSet.Resource != "daemonsets" {
		t.Errorf("GVRDaemonSet.Resource = %q, tests expect 'daemonsets'", snapshot.GVRDaemonSet.Resource)
	}
}
