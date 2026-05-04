#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  report=$(printf '%s' "$payload" | jq -r '.report // ""')
else
  report=$(printf '%s' "$payload" | sed -n 's/.*"report"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
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
