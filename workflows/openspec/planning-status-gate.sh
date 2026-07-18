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

if printf '%s\n' "$report" | grep -Fxq PLANNING_READY; then
  printf 'Planning status gate: ready\n'
  exit 0
fi

printf 'Planning status gate: blocked; return to define-change before continuing\n' >&2
exit 1
