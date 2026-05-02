#!/bin/sh
set -eu

payload=$(cat)
adapter=$(printf '%s' "$payload" | sed -n 's/.*"adapter"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

case "$adapter" in
  claude)
    if command -v claude >/dev/null 2>&1; then
      models_output=$(claude models list 2>/dev/null) && [ -n "$models_output" ] && {
        printf '%s' "$models_output" | awk 'NF { gsub(/"/, "\\\""); printf "%s\"%s\"", sep, $1; sep="," } END { print "" }' | sed 's/^/[/' | sed 's/$/]/'
        exit 0
      }
    fi
    ;;
esac

printf '[]\n'
