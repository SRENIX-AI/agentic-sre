#!/usr/bin/env bash
# Copyright 2026 Cluster Health Autopilot contributors
# SPDX-License-Identifier: Apache-2.0
#
# verify-local.sh — the single command a developer runs before opening or
# merging a PR. Per-checkin verification runs LOCALLY (see RELEASING.md);
# the GitHub CI workflow (.github/workflows/ci.yml) is manual-only.
#
# This script mirrors EXACTLY what ci.yml does, in order:
#   1. go mod verify
#   2. go vet ./...
#   3. go build -trimpath -o bin/cha ./cmd/cha   (+ ./bin/cha version)
#   4. go test -race -count=1 -coverprofile=coverage.out ./...
#   5. golangci-lint run --timeout=5m
#   6. changelog-lint.sh
#   7. changelog-tag-check.sh   (+ its selftest)
#   8. helm lint charts/cluster-health-autopilot
#   9. helm unittest charts/cluster-health-autopilot
#  10. helm template smoke (default + full values)
#
# Fails fast on the first failing step.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

step() { echo; echo "==> $*"; }

# --- Go build + test (mirrors ci.yml build-test job) ------------------
step "go mod verify"
go mod verify

step "go vet ./..."
go vet ./...

step "go build -trimpath -o bin/cha ./cmd/cha"
go build -trimpath -o bin/cha ./cmd/cha

step "cha version"
./bin/cha version

step "go test -race -count=1 -coverprofile=coverage.out ./..."
go test -race -count=1 -coverprofile=coverage.out ./...

step "coverage summary"
go tool cover -func=coverage.out | awk '/^total:/ {print "Total coverage: " $3}'

# --- Lint + changelog gates (mirrors ci.yml lint job) -----------------
step "golangci-lint run --timeout=5m"
if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run --timeout=5m
else
  echo "ERROR: golangci-lint not found on PATH (ci.yml pins v2.12.0)." >&2
  echo "Install: https://golangci-lint.run/welcome/install/" >&2
  exit 1
fi

step "changelog lint"
bash scripts/changelog-lint.sh

step "changelog tag check"
# Match ci.yml: ensure tags are present before the parity gate runs.
git fetch --tags --force --quiet origin 2>/dev/null || true
bash scripts/changelog-tag-check.sh

step "changelog tag check selftest"
bash scripts/changelog-tag-check_test.sh

# --- Helm chart gates (mirrors ci.yml chart-test job) -----------------
step "helm lint charts/cluster-health-autopilot"
helm lint charts/cluster-health-autopilot

step "helm unittest charts/cluster-health-autopilot"
helm unittest charts/cluster-health-autopilot

step "helm template smoke (default values)"
helm template foo charts/cluster-health-autopilot >/tmp/cha-rendered.yaml
wc -l /tmp/cha-rendered.yaml

step "helm template smoke (watcher + remediation + leader election)"
helm template foo charts/cluster-health-autopilot \
  --set watcher.enabled=true \
  --set remediation.enabled=true \
  --set watcher.leaderElection.enabled=true \
  >/tmp/cha-rendered-full.yaml
wc -l /tmp/cha-rendered-full.yaml

echo
echo "======================================================================"
echo "  ✅  verify-local: ALL CHECKS PASSED"
echo "======================================================================"
