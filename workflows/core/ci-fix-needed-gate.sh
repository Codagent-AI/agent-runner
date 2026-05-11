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

summary=$(printf '%s\n' "$report" | sed '/^### PR Comments/,$d')

comments_are_informational() {
  printf '%s\n' "$summary" | grep -Eiq 'CI is green|CI is passing|all checks (are )?(terminal and )?(passing|green)' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no failed checks|failed checks:[[:space:]]*`?0`?|failed checks:[[:space:]]*none' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no pending checks|pending checks:[[:space:]]*`?0`?|still running:[[:space:]]*none' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no blocking reviews|blocking reviews:[[:space:]]*`?0`?|blocking reviews:[[:space:]]*none' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no unresolved inline review threads|unresolved inline review threads:[[:space:]]*`?0`?|unresolved review threads:[[:space:]]*0' || return 1
}

case "$status" in
  failed)
    printf 'CI fix gate: %s; fixes required\n' "$status"
    exit 1
    ;;
  comments)
    if comments_are_informational; then
      printf 'CI fix gate: comments are informational; no fix cycle this iteration\n'
      exit 0
    fi
    printf 'CI fix gate: comments; fixes required\n'
    exit 1
    ;;
  *)
    printf 'CI fix gate: %s; no fix cycle this iteration\n' "${status:-unknown}"
    exit 0
    ;;
esac
