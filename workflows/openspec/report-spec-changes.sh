#!/bin/sh
# Report spec-root changes the user must commit separately. Read-only.
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")
spec_root=$(PAYLOAD="$payload" python3 -c 'import json,os; print(json.loads(os.environ["PAYLOAD"]).get("spec_root", ""))')
cd "$spec_root"

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'Uncommitted spec-root files (commit these separately):\n'
  git --no-optional-locks -c status.relativePaths=true -c color.status=false \
    status --short --untracked-files=all -- .
  exit 0
fi

printf 'Spec root is not under version control; commit these changes separately:\n'
for dir in "openspec/changes/$change_name" openspec/changes/archive/????-??-??-"$change_name"; do
  if [ -d "$dir" ]; then printf '%s\n' "$dir"; fi
done
