#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")

set +e
agent-validator detect
status=$?
set -e

if [ "$status" -eq 0 ]; then
  printf 'Unvalidated changes detected. Run agent-validator before planning.\n' >&2
  exit 1
fi
if [ "$status" -ne 2 ]; then
  exit "$status"
fi

change_dir="openspec/changes/$change_name"
if [ -e "$change_dir" ]; then
  printf "OpenSpec change '%s' already exists at %s/%s\n" "$change_name" "$(pwd -P)" "$change_dir" >&2
  exit 1
fi

openspec new change "$change_name"
