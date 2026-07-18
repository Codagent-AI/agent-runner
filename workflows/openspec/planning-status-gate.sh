#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  report=$(printf '%s' "$payload" | jq -r '.report // ""')
else
  report=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"planning-status-gate: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
print(parsed.get("report") or "", end="")
PY
)
fi

last_line=$(printf '%s\n' "$report" | sed '/^[[:space:]]*$/d' | tail -n 1)

case "$last_line" in
  PLANNING_READY)
    printf 'Planning status gate: ready\n'
    exit 0
    ;;
  PLANNING_BLOCKED:*)
    reason=${last_line#PLANNING_BLOCKED:}
    reason=$(printf '%s' "$reason" | sed 's/^[[:space:]]*//')
    if [ -z "$reason" ]; then
      reason="no reason provided"
    fi
    printf 'Planning status gate: blocked: %s\n' "$reason" >&2
    exit 1
    ;;
  *)
    printf 'Planning status gate: final non-empty line must be PLANNING_READY or PLANNING_BLOCKED:<reason>; got: %s\n' "$last_line" >&2
    exit 1
    ;;
esac
