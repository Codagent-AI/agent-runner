#!/bin/sh
set -eu

payload=$(cat)
adapter=$(printf '%s' "$payload" | sed -n 's/.*"adapter"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

case "$adapter" in
  claude)
    if command -v claude >/dev/null 2>&1; then
      models_output=$(claude models list 2>/dev/null) && [ -n "$models_output" ] && {
        printf '%s' "$models_output" | awk '
          function clean(s) {
            gsub(/^[|*`"(),:;[:space:]]+/, "", s)
            gsub(/[|*`"(),:;[:space:]]+$/, "", s)
            return s
          }
          function valid_model(s) {
            return s ~ /^[a-z0-9._-]*(opus|sonnet|haiku)[a-z0-9._-]*$/
          }
          function emit(s) {
            gsub(/\\/, "\\\\", s)
            gsub(/"/, "\\\"", s)
            printf "%s\"%s\"", sep, s
            sep=","
          }
          {
            for (i = 1; i <= NF; i++) {
              candidate = clean($i)
              if (valid_model(candidate) && !seen[candidate]++) {
                emit(candidate)
              }
            }
          }
          BEGIN { printf "[" }
          END { print "]" }
        '
        exit 0
      }
    fi
    ;;
esac

printf '[]\n'
