#!/bin/sh
set -eu

payload=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to read the re-acceptance status gate input\n' >&2
  exit 1
fi
status_file=$(printf '%s' "$payload" | jq -r '.status_file // ""')

if [ -z "$status_file" ] || [ ! -f "$status_file" ]; then
  printf 're-acceptance testing did not record a result\n' >&2
  exit 1
fi

status=$(sed '/^[[:space:]]*$/d' "$status_file" | tail -n 1)
case "$status" in
  REACCEPTANCE_COMPLETE)
    exit 0
    ;;
  REACCEPTANCE_FAILED)
    printf 're-acceptance testing remains incomplete\n' >&2
    exit 1
    ;;
  *)
    printf 'invalid re-acceptance result: %s\n' "$status" >&2
    exit 1
    ;;
esac
