#!/usr/bin/env bash
set -euo pipefail

jscpd_version=4.3.0
if ! command -v jscpd >/dev/null 2>&1 || [ "$(jscpd --version)" != "$jscpd_version" ]; then
  echo "jscpd $jscpd_version is required: npm install -g jscpd@$jscpd_version" >&2
  exit 1
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."
jscpd .
