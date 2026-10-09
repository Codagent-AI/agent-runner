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

plain_packages="$("$script_dir/go-packages.sh")"
tagged_packages="$("$script_dir/go-packages.sh" -tags dev_audit)"
# Package import paths contain no whitespace; split the lists into arguments.
# shellcheck disable=SC2086
deadcode -test $plain_packages > "$plain_output"
# shellcheck disable=SC2086
deadcode -test -tags dev_audit $tagged_packages > "$tagged_output"

findings="$(comm -12 <(sort -u "$plain_output") <(sort -u "$tagged_output"))"
if [ -n "$findings" ]; then
  printf '%s\n' "$findings"
  exit 1
fi
