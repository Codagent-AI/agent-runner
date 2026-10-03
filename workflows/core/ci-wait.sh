#!/bin/sh
set -eu
for tool in python3 gh; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ci-wait: required tool $tool is unavailable" >&2
    exit 2
  fi
done
exec python3 "$(dirname "$0")/ci_wait.py"
