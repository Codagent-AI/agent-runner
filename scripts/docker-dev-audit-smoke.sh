#!/usr/bin/env bash
# Runs the product-owned detached-audit smoke in the supported Docker sandbox.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/agent-runner-dev-audit-smoke.XXXXXX")}"
TIMEOUT_SECONDS="${AUDIT_SMOKE_TIMEOUT_SECONDS:-45}"

mkdir -p "$ARTIFACT_DIR"
export AUDIT_SMOKE_TIMEOUT_SECONDS="$TIMEOUT_SECONDS"
ARTIFACT_DIR="$ARTIFACT_DIR" "$RUNNER_ROOT/scripts/sandbox-run.sh" \
  --dev-audit \
  --dev-audit-smoke \
  --no-default-secrets \
  --env AUDIT_SMOKE_TIMEOUT_SECONDS \
  --artifact-dir "$ARTIFACT_DIR" \
  --docker-run-arg "--network=none" \
  -- "bash /agent-runner-source/scripts/docker-dev-audit-smoke-container.sh"
