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
    sed -n 's/^## CI Status:[[:space:]]*\([[:alnum:]_-]*\).*/\1/p' |
    head -n 1
)

case "$status" in
  failed | comments)
    printf 'CI fix gate: %s; fixes required\n' "$status"
    exit 1
    ;;
  *)
    printf 'CI fix gate: %s; no fix cycle this iteration\n' "${status:-unknown}"
    exit 0
    ;;
esac
