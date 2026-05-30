// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package silence

import (
	"reflect"
	"testing"
	"time"

	chav1alpha1 "github.com/Bionic-AI-Solutions/cluster-health-autopilot/api/v1alpha1"
	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/diagnose"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var fixedNow = time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

func sil(name string, matcher chav1alpha1.SilenceMatcher, until time.Time) chav1alpha1.Silence {
	return chav1alpha1.Silence{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: chav1alpha1.SilenceSpec{
			Matcher: matcher,
			Until:   metav1.NewTime(until),
		},
	}
}

func diag(source, subject, severity string) diagnose.Diagnostic {
	return diagnose.Diagnostic{Source: source, Subject: subject, Severity: severity}
}

func TestMatches_AllFields(t *testing.T) {
	s := sil("s1", chav1alpha1.SilenceMatcher{
		Source: "StaleErrorPods", Subject: "Pod/default/x", Severity: "warning",
	}, fixedNow.Add(time.Hour))

	if !Matches(s, diag("StaleErrorPods", "Pod/default/x", "warning"), fixedNow) {
		t.Errorf("expected exact-field match to silence")
	}
	if Matches(s, diag("StaleErrorPods", "Pod/default/x", "critical"), fixedNow) {
		t.Errorf("severity mismatch should not silence")
	}
	if Matches(s, diag("StaleErrorPods", "Pod/other/y", "warning"), fixedNow) {
		t.Errorf("subject mismatch should not silence")
	}
	if Matches(s, diag("OtherAnalyzer", "Pod/default/x", "warning"), fixedNow) {
		t.Errorf("source mismatch should not silence")
	}
}

func TestMatches_WildcardFields(t *testing.T) {
	// Source-only matcher silences EVERY finding from that analyzer.
	s := sil("s2", chav1alpha1.SilenceMatcher{Source: "StaleErrorPods"}, fixedNow.Add(time.Hour))
	if !Matches(s, diag("StaleErrorPods", "Pod/a/b", "warning"), fixedNow) {
		t.Errorf("source wildcard should silence any subject")
	}
	if !Matches(s, diag("StaleErrorPods", "Pod/c/d", "critical"), fixedNow) {
		t.Errorf("source wildcard should silence any severity")
	}
	if Matches(s, diag("Different", "Pod/a/b", "warning"), fixedNow) {
		t.Errorf("source mismatch — should not silence")
	}
}

func TestMatches_ExpiredNeverMatches(t *testing.T) {
	s := sil("expired", chav1alpha1.SilenceMatcher{Source: "X"}, fixedNow.Add(-time.Minute))
	if Matches(s, diag("X", "Y/Z/W", "warning"), fixedNow) {
		t.Errorf("expired silence must NEVER match")
	}
}

func TestMatches_EmptyMatcherRejected(t *testing.T) {
	// Defense in depth: a fully-empty matcher must not silence everything
	// (the CRD validation should also reject this).
	s := sil("empty", chav1alpha1.SilenceMatcher{}, fixedNow.Add(time.Hour))
	if Matches(s, diag("X", "Y", "warning"), fixedNow) {
		t.Errorf("empty matcher should not silence (would mute everything)")
	}
}

func TestFilter_DropsMatched_KeepsRest(t *testing.T) {
	silences := []chav1alpha1.Silence{
		sil("stale-warn", chav1alpha1.SilenceMatcher{Source: "StaleErrorPods", Severity: "warning"},
			fixedNow.Add(time.Hour)),
	}
	diags := []diagnose.Diagnostic{
		diag("StaleErrorPods", "Pod/a/b", "warning"),  // silenced
		diag("StaleErrorPods", "Pod/c/d", "critical"), // kept (severity differs)
		diag("OtherAnalyzer", "Pod/a/b", "warning"),   // kept (source differs)
	}
	got := Filter(diags, silences, fixedNow)
	if len(got) != 2 {
		t.Fatalf("expected 2 survivors; got %d (%+v)", len(got), got)
	}
	for _, d := range got {
		if d.Source == "StaleErrorPods" && d.Severity == "warning" {
			t.Errorf("a warning-severity StaleErrorPods slipped through: %+v", d)
		}
	}
}

func TestFilter_OrderPreserved(t *testing.T) {
	silences := []chav1alpha1.Silence{
		sil("drop-c", chav1alpha1.SilenceMatcher{Subject: "Pod/c/c"}, fixedNow.Add(time.Hour)),
	}
	in := []diagnose.Diagnostic{
		diag("X", "Pod/a/a", "warning"),
		diag("X", "Pod/b/b", "warning"),
		diag("X", "Pod/c/c", "warning"), // dropped
		diag("X", "Pod/d/d", "warning"),
	}
	wantSubjects := []string{"Pod/a/a", "Pod/b/b", "Pod/d/d"}
	got := Filter(in, silences, fixedNow)
	gotSubjects := make([]string, len(got))
	for i, d := range got {
		gotSubjects[i] = d.Subject
	}
	if !reflect.DeepEqual(gotSubjects, wantSubjects) {
		t.Errorf("order not preserved: got %v want %v", gotSubjects, wantSubjects)
	}
}

func TestFilter_NoSilences_PassesThrough(t *testing.T) {
	diags := []diagnose.Diagnostic{
		diag("X", "Y/Z/W", "warning"),
	}
	got := Filter(diags, nil, fixedNow)
	if len(got) != 1 || got[0].Subject != "Y/Z/W" {
		t.Errorf("nil silences should be a no-op pass-through")
	}
}

func TestFilter_DoesNotMutateInputSlice(t *testing.T) {
	silences := []chav1alpha1.Silence{
		sil("drop-a", chav1alpha1.SilenceMatcher{Subject: "Pod/a/a"}, fixedNow.Add(time.Hour)),
	}
	in := []diagnose.Diagnostic{
		diag("X", "Pod/a/a", "warning"),
		diag("X", "Pod/b/b", "warning"),
	}
	_ = Filter(in, silences, fixedNow)
	// in[0] must remain Pod/a/a — Filter mustn't shuffle the caller's slice.
	if in[0].Subject != "Pod/a/a" {
		t.Errorf("Filter mutated caller's slice; got in[0]=%+v", in[0])
	}
}

func TestCountMatches(t *testing.T) {
	silences := []chav1alpha1.Silence{
		sil("a", chav1alpha1.SilenceMatcher{Source: "X"}, fixedNow.Add(time.Hour)),
		sil("b", chav1alpha1.SilenceMatcher{Source: "Y"}, fixedNow.Add(time.Hour)),
	}
	diags := []diagnose.Diagnostic{
		diag("X", "1", "warning"),
		diag("X", "2", "warning"),
		diag("Y", "3", "warning"),
		diag("Z", "4", "warning"),
	}
	c := CountMatches(diags, silences, fixedNow)
	if c["default/a"] != 2 {
		t.Errorf("silence default/a should match 2 diagnostics; got %d", c["default/a"])
	}
	if c["default/b"] != 1 {
		t.Errorf("silence default/b should match 1 diagnostic; got %d", c["default/b"])
	}
	if _, ok := c["default/missing"]; ok {
		t.Error("unmatched silence should not appear in counts")
	}
}
