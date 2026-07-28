#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  change_name=$(
    printf '%s' "$payload" |
      jq -er '
        if type != "object" then
          error("input must be a JSON object")
        elif (.change_name | type) != "string" then
          error("change_name must be a string")
        else
          .change_name
        end
      '
  )
else
  change_name=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"validate-change-name: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
if not isinstance(parsed, dict):
    print("validate-change-name: input must be a JSON object", file=sys.stderr)
    sys.exit(2)
change_name = parsed.get("change_name")
if not isinstance(change_name, str):
    print("validate-change-name: change_name must be a string", file=sys.stderr)
    sys.exit(2)
print(change_name, end="")
PY
)
fi

case "$change_name" in
  ""|[!a-z0-9]*|*[!a-z0-9-]*)
    printf 'validate-change-name: change_name must use lowercase letters, digits, and hyphens: %s\n' "$change_name" >&2
    exit 1
    ;;
esac

printf '%s\n' "$change_name"
