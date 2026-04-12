#!/usr/bin/env bash
set -euo pipefail

ORIG_DIR="$PWD"
cd "$(dirname "$0")"
go run ./cmd/agent-runner -C "$ORIG_DIR" "$@"
