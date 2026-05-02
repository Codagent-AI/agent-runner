#!/bin/sh
set -eu

payload=$(cat)
adapter=$(printf '%s' "$payload" | sed -n 's/.*"adapter"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

case "$adapter" in
  claude)
    if command -v claude >/dev/null 2>&1; then
      if claude models list >/dev/null 2>&1; then
        claude models list 2>/dev/null | awk 'NF { gsub(/"/, "\\\""); printf "%s\"%s\"", sep, $1; sep="," } END { print "" }' | sed 's/^/[/' | sed 's/$/]/'
        exit 0
      fi
    fi
    ;;
esac

printf '[]\n'
