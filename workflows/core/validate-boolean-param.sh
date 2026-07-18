#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  name=$(printf '%s' "$payload" | jq -r '.name // "parameter"')
  value=$(printf '%s' "$payload" | jq -r '.value // ""')
else
  name=$(printf '%s\n' "$payload" | sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  value=$(printf '%s\n' "$payload" | sed -n 's/.*"value"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

case "$value" in
  true|false)
    exit 0
    ;;
  *)
    printf '%s must be true or false, got: %s\n' "$name" "$value" >&2
    exit 1
    ;;
esac
