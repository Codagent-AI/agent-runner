#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  task_compliance=$(printf '%s' "$payload" | jq -r '.task_compliance // ""')
  task_file=$(printf '%s' "$payload" | jq -r '.task_file // ""')
else
  task_compliance=$(printf '%s\n' "$payload" | sed -n 's/.*"task_compliance"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  task_file=$(printf '%s\n' "$payload" | sed -n 's/.*"task_file"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

case "$task_compliance" in
  true|false) ;;
  *)
    printf 'task_compliance must be true or false, got: %s\n' "$task_compliance" >&2
    exit 1
    ;;
esac

if [ "$task_compliance" = true ] && [ -z "$task_file" ]; then
  printf 'task_compliance is true but task_file is empty\n' >&2
  exit 1
fi
