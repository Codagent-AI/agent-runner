#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  change_name=$(printf '%s' "$payload" | jq -er '.change_name | select(type == "string")')
else
  change_name=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"create-change: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
change_name = parsed.get("change_name") if isinstance(parsed, dict) else None
if not isinstance(change_name, str):
    print("create-change: change_name must be a string", file=sys.stderr)
    sys.exit(2)
print(change_name, end="")
PY
)
fi

case "$change_name" in
  ""|[!a-z0-9]*|*[!a-z0-9-]*)
    printf 'create-change: change_name must use lowercase letters, digits, and hyphens: %s\n' "$change_name" >&2
    exit 1
    ;;
esac

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

change_dir="specs/changes/$change_name"
if [ -e "$change_dir" ]; then
  printf "Spec-driven change '%s' already exists at %s/%s\n" "$change_name" "$(pwd -P)" "$change_dir" >&2
  exit 1
fi

mkdir -p "$change_dir/specs"
