#!/bin/sh
# Fails, printing the record, when an agent wrote the blocked record because
# only the user can settle a question about the approved definition.
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  blocked_file=$(printf '%s' "$payload" | jq -r '.blocked_file // ""')
else
  blocked_file=$(printf '%s\n' "$payload" | sed -n 's/.*"blocked_file"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

if [ -z "$blocked_file" ]; then
  echo "blocked_file is required" >&2
  exit 1
fi

if [ -e "$blocked_file" ]; then
  echo "the change cannot be implemented compliantly without a user decision:" >&2
  cat "$blocked_file" >&2
  echo "after deciding, update the definition and start a new run; resuming this run would not redo the stopped work" >&2
  exit 1
fi
