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
  passed)
    printf 'CI status gate: passed\n'
    exit 0
    ;;
  failed | comments)
    printf 'CI status gate: %s; fixes required\n' "$status"
    exit 1
    ;;
  pending)
    printf 'CI status gate: pending; waiting for another poll\n'
    exit 1
    ;;
  *)
    printf 'CI status gate: missing or unknown status; waiting for another poll\n'
    exit 1
    ;;
esac
