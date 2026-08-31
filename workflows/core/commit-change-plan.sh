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
    print(f"commit-change-plan: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
change_name = parsed.get("change_name") if isinstance(parsed, dict) else None
change_dir = parsed.get("change_dir") if isinstance(parsed, dict) else None
if not isinstance(change_name, str) or not isinstance(change_dir, str):
    print("commit-change-plan: change_name and change_dir must be strings", file=sys.stderr)
    sys.exit(2)
print(change_name)
print(change_dir)
PY
)
  change_name=$(printf '%s\n' "$parsed" | sed -n '1p')
  change_dir=$(printf '%s\n' "$parsed" | sed -n '2p')
fi

case "$change_name" in
  ""|[!a-z0-9]*|*[!a-z0-9-]*)
    printf 'commit-change-plan: change_name must use lowercase letters, digits, and hyphens: %s\n' "$change_name" >&2
    exit 1
    ;;
esac

case "$change_dir" in
  ""|.|./*|*/./*|*/.|/*|..|../*|*/../*|*/..)
    printf 'commit-change-plan: change_dir must be a confined relative path: %s\n' "$change_dir" >&2
    exit 1
    ;;
esac

git add -A -- "$change_dir"
if git diff --cached --quiet -- "$change_dir"; then
  printf 'commit-change-plan: no staged changes found for %s\n' "$change_name" >&2
  exit 1
fi

git commit -m "[commit-plan] chore: add change documents for $change_name" -- "$change_dir"
agent-validator skip
