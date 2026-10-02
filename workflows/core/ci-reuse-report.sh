#!/bin/sh
set -eu

exec python3 "$(dirname "$0")/ci_wait.py" --verify-reuse
