#!/bin/sh
# Archive an OpenSpec change in an external spec root. Never commits there.
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")
spec_root=$(PAYLOAD="$payload" python3 -c 'import json,os; print(json.loads(os.environ["PAYLOAD"]).get("spec_root", ""))')

case "$spec_root" in
  /*) ;;
  *)
    printf 'archive-external: spec_root must be absolute: %s\n' "$spec_root" >&2
    exit 1
    ;;
esac
if [ ! -d "$spec_root/openspec" ]; then
  printf 'archive-external: not an OpenSpec project: %s\n' "$spec_root" >&2
  exit 1
fi
cd "$spec_root"

archive=""
for dir in openspec/changes/archive/????-??-??-"$change_name"; do
  if [ -d "$dir" ]; then archive=$dir; fi
done
active="openspec/changes/$change_name"

if [ -n "$archive" ]; then
  if [ -e "$active" ]; then
    printf 'archive-external: both %s and %s exist in %s\n' "$active" "$archive" "$spec_root" >&2
    exit 1
  fi
  printf 'Change %s is already archived at %s\n' "$change_name" "$archive"
  exit 0
fi

openspec validate --type change "$change_name"
openspec archive "$change_name" --yes
