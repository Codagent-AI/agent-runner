#!/usr/bin/env bash
set -euo pipefail

if ! command -v deadcode >/dev/null 2>&1; then
  echo "deadcode is required: go install golang.org/x/tools/cmd/deadcode@v0.50.0" >&2
  exit 1
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

plain_output="$(mktemp)"
tagged_output="$(mktemp)"
trap 'rm -f "$plain_output" "$tagged_output"' EXIT

# shellcheck disable=SC2046 # package paths contain no spaces
deadcode -test $(./.validator/go-packages.sh) > "$plain_output"
# shellcheck disable=SC2046
deadcode -test -tags dev_audit $(./.validator/go-packages.sh -tags dev_audit) > "$tagged_output"

findings="$(comm -12 <(sort -u "$plain_output") <(sort -u "$tagged_output"))"
if [ -n "$findings" ]; then
  printf '%s\n' "$findings"
  exit 1
fi
