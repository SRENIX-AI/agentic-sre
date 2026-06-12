// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"os"
	"strings"

	"k8s.io/client-go/rest"
)

// endpointsDeprecationFragment matches the API server's deprecation warning
// for the legacy core/v1 Endpoints API, e.g.
//
//	v1 Endpoints is deprecated in v1.33+; use discovery.k8s.io/v1 EndpointSlice
//
// Matching on the stable prefix (not the exact version string) keeps the
// filter working as the server bumps the "in v1.NN+" part across upgrades.
const endpointsDeprecationFragment = "v1 Endpoints is deprecated"

// suppressingWarningHandler drops the core/v1 Endpoints deprecation warning
// and forwards every other server warning to next. CHA keeps a handful of
// deliberate legacy-Endpoints fallback reads (DNSChainDrift + KongRoutes
// when EndpointSlices are unavailable, snapshot capture); without this
// filter the deployed watcher/aiwatch printed the same deprecation line for
// every such call (~2.5 log lines/sec), drowning real signal.
type suppressingWarningHandler struct {
	next rest.WarningHandler
}

// HandleWarningHeader implements rest.WarningHandler.
func (h suppressingWarningHandler) HandleWarningHeader(code int, agent string, text string) {
	if strings.Contains(text, endpointsDeprecationFragment) {
		return
	}
	if h.next != nil {
		h.next.HandleWarningHeader(code, agent, text)
	}
}

// newSuppressingWarningHandler builds the production handler: Endpoints
// deprecation warnings are dropped entirely; all other server warnings pass
// through to a DEDUPLICATING stderr writer (each unique warning prints once
// per process instead of once per call). The next seam exists so tests can
// inject a recording handler.
func newSuppressingWarningHandler(next rest.WarningHandler) rest.WarningHandler {
	if next == nil {
		next = rest.NewWarningWriter(os.Stderr, rest.WarningWriterOptions{Deduplicate: true})
	}
	return suppressingWarningHandler{next: next}
}

// SuppressEndpointsDeprecationWarnings installs the Endpoints-deprecation
// warning filter on cfg (see suppressingWarningHandler): the core/v1
// Endpoints deprecation line is dropped, every other API-server warning is
// printed to stderr once per unique message. Returns cfg for call-site
// chaining. Safe on a nil config (no-op).
func SuppressEndpointsDeprecationWarnings(cfg *rest.Config) *rest.Config {
	if cfg == nil {
		return nil
	}
	cfg.WarningHandler = newSuppressingWarningHandler(nil)
	return cfg
}
