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
    print(f"plan-review-loop-gate: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
print(parsed.get("report") or "", end="")
PY
)
fi

if printf '%s\n' "$report" | grep -Fxq REVIEW_RESOLVED; then
  printf 'Plan review loop: resolved\n'
  exit 0
fi

if printf '%s\n' "$report" | grep -Eq '^PLANNING_BLOCKED:'; then
  printf 'Plan review loop: semantic blocker found\n'
  exit 0
fi

printf 'Plan review loop: another discussion round is required\n' >&2
exit 1
