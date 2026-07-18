#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")

change_dir="openspec/changes/$change_name"

git add -A -- "$change_dir"
if git diff --cached --quiet -- "$change_dir"; then
  printf 'commit-change-plan: no staged OpenSpec changes found for %s\n' "$change_name" >&2
  exit 1
fi

git commit -m "[commit-openspec] chore: add openspec documents for $change_name"
agent-validator skip
