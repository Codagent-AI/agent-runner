#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  change_name=$(printf '%s' "$payload" | jq -r '.change_name // ""')
else
  change_name=$(printf '%s\n' "$payload" | sed -n 's/.*"change_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

case "$change_name" in
  ""|[!a-z0-9]*|*[!a-z0-9-]*)
    printf 'commit-change-plan: invalid change_name %s\n' "$change_name" >&2
    exit 1
    ;;
esac

change_dir="openspec/changes/$change_name"

git add -- "$change_dir/"
git commit -m "[commit-openspec] chore: add openspec documents for $change_name"
agent-validator skip
