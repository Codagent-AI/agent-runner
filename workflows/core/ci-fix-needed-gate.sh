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
    print(f"ci-fix-needed-gate: invalid JSON input: {exc}", file=sys.stderr)
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

case "$status" in
  CI_FAILED)
    printf 'CI fix gate: failed; fixes required\n'
    exit 1
    ;;
  CI_COMMENTS)
    printf 'CI fix gate: comments; fixes required\n'
    exit 1
    ;;
  CI_PASSED)
    printf 'CI fix gate: passed; no fix cycle this iteration\n'
    exit 0
    ;;
  CI_PENDING)
    printf 'CI fix gate: pending; no fix cycle this iteration\n'
    exit 0
    ;;
  *)
    printf 'CI fix gate: missing or unknown status; no fix cycle this iteration\n'
    exit 0
    ;;
esac
