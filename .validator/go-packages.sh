#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

# Capture separately so an empty grep result cannot hide a go list failure.
packages="$(go list -e -f '{{.ImportPath}}' "$@" ./...)"
printf '%s\n' "$packages" | {
  grep -v '^github.com/codagent/agent-runner/worktrees\(/\|$\)' || [ "$?" -eq 1 ]
}
