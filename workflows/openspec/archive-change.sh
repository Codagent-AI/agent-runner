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
    printf 'archive-change: invalid change_name %s\n' "$change_name" >&2
    exit 1
    ;;
esac

change_dir="openspec/changes/$change_name"
archive_root="openspec/changes/archive"
before_archives=$(mktemp)
after_archives=$(mktemp)
trap 'rm -f "$before_archives" "$after_archives"' EXIT

find "$archive_root" -mindepth 1 -maxdepth 1 -type d -name "*-$change_name" -print | sort > "$before_archives"

openspec validate --type change "$change_name"
openspec archive "$change_name" --yes

find "$archive_root" -mindepth 1 -maxdepth 1 -type d -name "*-$change_name" -print | sort > "$after_archives"
new_archives=$(comm -13 "$before_archives" "$after_archives")
archive_dir=$(printf '%s\n' "$new_archives" | sed -n '1p')
extra_archive=$(printf '%s\n' "$new_archives" | sed -n '2p')

if [ -z "$archive_dir" ] || [ -n "$extra_archive" ] || [ ! -d "$archive_dir" ]; then
  printf 'archive-change: expected exactly one newly created archive directory, found:\n%s\n' "$new_archives" >&2
  exit 1
fi

git add -A -- "$change_dir" "$archive_dir" openspec/specs
git commit -m "[archive] chore: archive openspec documents for $change_name"
agent-validator skip
