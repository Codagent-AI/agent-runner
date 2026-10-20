#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  name=$(printf '%s' "$payload" | jq -r '.name // "parameter"')
  # Preserve NUL content across shell command substitution.
  content=$(printf '%s' "$payload" | jq -r '.value // ""' |
    LC_ALL=C tr -d ' \t\n\v\f\r' | tr '\000' x)
else
  name=$(printf '%s\n' "$payload" | sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  # Neutralize literal backslashes before removing JSON whitespace escapes.
  # An escaped quote leaves a backslash at the extraction boundary, which
  # correctly counts as content.
  content=$(printf '%s\n' "$payload" |
    sed -n 's/.*"value"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    sed 's/\\\\/x/g; s/ //g; s/\\[tnrf]//g; s/\\u000[9aAbBcCdD]//g; s/\\u0020//g')
fi

if [ -z "$content" ]; then
  printf '%s must not be empty\n' "$name" >&2
  exit 1
fi
