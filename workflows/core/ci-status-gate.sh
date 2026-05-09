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

summary=$(printf '%s\n' "$report" | sed '/^### PR Comments/,$d')

comments_are_informational() {
  printf '%s\n' "$summary" | grep -Eiq 'CI is green|CI is passing|all checks (are )?(terminal and )?(passing|green)' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no failed checks|failed checks:[[:space:]]*`?0`?|failed checks:[[:space:]]*none' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no pending checks|pending checks:[[:space:]]*`?0`?|still running:[[:space:]]*none' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no blocking reviews|blocking reviews:[[:space:]]*`?0`?|blocking reviews:[[:space:]]*none' || return 1
  printf '%s\n' "$summary" | grep -Eiq 'no unresolved inline review threads|unresolved inline review threads:[[:space:]]*`?0`?|unresolved review threads:[[:space:]]*0' || return 1
}

case "$status" in
  passed)
    printf 'CI status gate: passed\n'
    exit 0
    ;;
  comments)
    if comments_are_informational; then
      printf 'CI status gate: comments are informational; treating as passed\n'
      exit 0
    fi
    printf 'CI status gate: comments; fixes required\n'
    exit 1
    ;;
  failed)
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
