#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

packages="$("$script_dir/go-packages.sh" "$@")"
# Unlike ./..., an explicit package list makes go build reject test-only
# packages. Keep the same package set, omitting those with nothing to build.
# shellcheck disable=SC2086
build_packages="$(go list -e -f '{{if or .GoFiles .CgoFiles}}{{.ImportPath}}{{end}}' "$@" $packages)"
# shellcheck disable=SC2086
exec go build "$@" $build_packages
