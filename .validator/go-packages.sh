#!/usr/bin/env bash
# Prints the module's Go package directories as ./relative paths, excluding
# anything under ./worktrees. Leftover worktree folders without go.mod are
# otherwise picked up by ./... as packages of this module.
#
# usage: go-packages.sh [--build] [go list flags, for example -tags dev_audit]
#   --build  omit test-only packages, which `go build` rejects when named
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

format='{{.Dir}}'
if [ "${1:-}" = "--build" ]; then
  shift
  format='{{if .GoFiles}}{{.Dir}}{{end}}'
fi

root="$(pwd -P)"
go list -e -f "$format" "$@" ./... |
  sed -n "s#^$root\$#.#p; s#^$root/#./#p" |
  grep -v '^\./worktrees\(/\|$\)'
