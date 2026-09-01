#!/usr/bin/env bash
set -euo pipefail

ORIG_DIR="$PWD"
cd "$(dirname "$0")"
DEV_AUDIT_ROOT="$(pwd -P)"
DEV_AUDIT_REVISION="$(git rev-parse HEAD 2>/dev/null || true)"
DEV_AUDIT_DIRTY=""
if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
  DEV_AUDIT_DIRTY="true"
fi
DEV_AUDIT_LDFLAGS="-X github.com/codagent/agent-runner/internal/devaudit.BuildRoot=${DEV_AUDIT_ROOT} -X github.com/codagent/agent-runner/internal/devaudit.BuildRevision=${DEV_AUDIT_REVISION} -X github.com/codagent/agent-runner/internal/devaudit.BuildDirty=${DEV_AUDIT_DIRTY}"
go run -tags dev_audit -ldflags "$DEV_AUDIT_LDFLAGS" ./cmd/agent-runner -C "$ORIG_DIR" "$@"
