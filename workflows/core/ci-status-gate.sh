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
    sed '/^[[:space:]]*$/d' |
    tail -n 1
)

if [ -z "$status" ]; then
  printf "CI status gate: CI wait failed; no CI report was produced (see the CI wait step's diagnostic)\n"
  exit 1
fi

case "$status" in
  CI_PASSED|CI_REVIEW_INCOMPLETE)
    printf 'CI status gate: passed\n'
    exit 0
    ;;
  CI_COMMENTS)
    printf 'CI status gate: comments; fixes required\n'
    exit 1
    ;;
  CI_FAILED)
    printf 'CI status gate: failed; fixes required\n'
    exit 1
    ;;
  CI_PENDING)
    printf 'CI status gate: pending; waiting for another poll\n'
    exit 1
    ;;
  *)
    printf 'CI status gate: missing or unknown status in CI report\n'
    exit 1
    ;;
esac
