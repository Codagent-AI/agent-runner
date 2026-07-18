#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")

change_dir="openspec/changes/$change_name"

git add -- "$change_dir/"
git commit -m "[commit-openspec] chore: add openspec documents for $change_name"
agent-validator skip
