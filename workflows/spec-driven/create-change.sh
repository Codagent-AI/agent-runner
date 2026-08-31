#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  change_name=$(printf '%s' "$payload" | jq -er '.change_name | select(type == "string")')
  change_dir=$(printf '%s' "$payload" | jq -er '.change_dir | select(type == "string")')
else
  parsed=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"create-change: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
values = [parsed.get(key) for key in ("change_name", "change_dir")] if isinstance(parsed, dict) else []
if len(values) != 2 or not all(isinstance(value, str) for value in values):
    print("create-change: change_name and change_dir must be strings", file=sys.stderr)
    sys.exit(2)
print(*values, sep="\n")
PY
)
  change_name=$(printf '%s\n' "$parsed" | sed -n '1p')
  change_dir=$(printf '%s\n' "$parsed" | sed -n '2p')
fi

case "$change_name" in
  ""|[!a-z0-9]*|*[!a-z0-9-]*)
    printf 'create-change: change_name must use lowercase letters, digits, and hyphens: %s\n' "$change_name" >&2
    exit 1
    ;;
esac

if ! command -v agent-validator >/dev/null 2>&1; then
  printf 'create-change: agent-validator is not installed; install it before creating a change\n' >&2
  exit 127
fi

set +e
agent-validator detect
status=$?
set -e

if [ "$status" -eq 0 ]; then
  printf 'Unvalidated changes detected. Run agent-validator before planning.\n' >&2
  exit 1
fi
if [ "$status" -ne 2 ]; then
  exit "$status"
fi

case "$change_dir" in
  ""|/|.|..)
    printf 'create-change: change_dir must identify a dedicated artifact directory: %s\n' "$change_dir" >&2
    exit 1
    ;;
esac

if [ -e "$change_dir" ]; then
  printf "Spec-driven change '%s' already exists at %s\n" "$change_name" "$change_dir" >&2
  exit 1
fi

mkdir -p "$change_dir/specs"
