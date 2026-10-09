#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

packages="$("$script_dir/go-packages.sh" -tags dev_audit)"
# Package import paths contain no whitespace; split the list into arguments.
# shellcheck disable=SC2086
exec go test -tags dev_audit "$@" $packages
