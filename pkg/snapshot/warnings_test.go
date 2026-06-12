// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"bytes"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

// recordingWarningHandler captures every warning forwarded to it.
type recordingWarningHandler struct {
	got []string
}

func (r *recordingWarningHandler) HandleWarningHeader(_ int, _ string, text string) {
	r.got = append(r.got, text)
}

// The exact text the live API server sends today (plus the version drift
// variant a future server might send).
const (
	endpointsWarning       = "v1 Endpoints is deprecated in v1.33+; use discovery.k8s.io/v1 EndpointSlice"
	endpointsWarningFuture = "v1 Endpoints is deprecated in v1.39+; use discovery.k8s.io/v1 EndpointSlice"
)

func TestSuppressingWarningHandler_DropsEndpointsDeprecation(t *testing.T) {
	rec := &recordingWarningHandler{}
	h := newSuppressingWarningHandler(rec)

	h.HandleWarningHeader(299, "-", endpointsWarning)
	h.HandleWarningHeader(299, "-", endpointsWarningFuture)

	if len(rec.got) != 0 {
		t.Errorf("Endpoints deprecation warnings must be suppressed, but %d leaked: %v", len(rec.got), rec.got)
	}
}

func TestSuppressingWarningHandler_PassesOtherWarningsThrough(t *testing.T) {
	rec := &recordingWarningHandler{}
	h := newSuppressingWarningHandler(rec)

	other := "batch/v1beta1 CronJob is deprecated in v1.21+, unavailable in v1.25+; use batch/v1 CronJob"
	h.HandleWarningHeader(299, "-", endpointsWarning)
	h.HandleWarningHeader(299, "-", other)

	if len(rec.got) != 1 || rec.got[0] != other {
		t.Errorf("non-Endpoints warnings must pass through untouched; got %v", rec.got)
	}
}

// TestSuppressingWarningHandler_DefaultNextDeduplicates pins the production
// wiring contract: warnings that DO pass the filter print once per unique
// message, not once per call (rest.NewWarningWriter with Deduplicate). The
// writer is constructed here exactly as newSuppressingWarningHandler(nil)
// does, but pointed at a buffer so the output is observable.
func TestSuppressingWarningHandler_DefaultNextDeduplicates(t *testing.T) {
	var buf bytes.Buffer
	h := suppressingWarningHandler{
		next: rest.NewWarningWriter(&buf, rest.WarningWriterOptions{Deduplicate: true}),
	}

	other := "policy/v1beta1 PodSecurityPolicy is deprecated in v1.21+, unavailable in v1.25+"
	for i := 0; i < 5; i++ {
		h.HandleWarningHeader(299, "-", other)
		h.HandleWarningHeader(299, "-", endpointsWarning)
	}

	out := buf.String()
	if strings.Contains(out, "Endpoints is deprecated") {
		t.Errorf("Endpoints deprecation leaked to the writer: %q", out)
	}
	if n := strings.Count(out, "PodSecurityPolicy"); n != 1 {
		t.Errorf("expected the passthrough warning to print exactly once (dedup), got %d in %q", n, out)
	}
}

func TestSuppressEndpointsDeprecationWarnings_InstallsHandler(t *testing.T) {
	cfg := &rest.Config{}
	got := SuppressEndpointsDeprecationWarnings(cfg)
	if got != cfg {
		t.Fatalf("expected the same *rest.Config back for chaining")
	}
	if cfg.WarningHandler == nil {
		t.Fatal("WarningHandler not installed on the config")
	}
	if _, ok := cfg.WarningHandler.(suppressingWarningHandler); !ok {
		t.Errorf("installed handler is %T, want suppressingWarningHandler", cfg.WarningHandler)
	}
}

func TestSuppressEndpointsDeprecationWarnings_NilConfigNoop(t *testing.T) {
	if got := SuppressEndpointsDeprecationWarnings(nil); got != nil {
		t.Errorf("nil config must stay nil, got %v", got)
	}
}

// TestNewLiveSource_RespectsCallerWarningHandler — NewLiveSource only
// installs the filter when the caller hasn't wired any handler, and never
// mutates the caller's config either way.
func TestNewLiveSource_RespectsCallerWarningHandler(t *testing.T) {
	rec := &recordingWarningHandler{}
	cfg := &rest.Config{Host: "https://127.0.0.1:6443", WarningHandler: rec}
	if _, err := NewLiveSource(cfg); err != nil {
		t.Fatalf("NewLiveSource: %v", err)
	}
	if cfg.WarningHandler != rest.WarningHandler(rec) {
		t.Errorf("caller-provided WarningHandler was replaced")
	}

	plain := &rest.Config{Host: "https://127.0.0.1:6443"}
	if _, err := NewLiveSource(plain); err != nil {
		t.Fatalf("NewLiveSource: %v", err)
	}
	if plain.WarningHandler != nil {
		t.Errorf("NewLiveSource must not mutate the caller's config (filter goes on an internal copy)")
	}
}
