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
    print(f"ci-review-incomplete-gate: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
print(parsed.get("report") or "", end="")
PY
)
fi

status=$(
  printf '%s\n' "$report" |
    sed '/^[[:space:]]*$/d' |
    tail -n 1
)

if [ "$status" = CI_REVIEW_INCOMPLETE ]; then
  printf 'CI review gate: review bot did not complete review; finishing with warning\n'
  exit 1
fi

exit 0
