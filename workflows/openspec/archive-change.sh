#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")

change_dir="openspec/changes/$change_name"
archive_root="openspec/changes/archive"
before_archives=$(mktemp "${TMPDIR:-/tmp}/openspec-archive-before.XXXXXX")
after_archives=$(mktemp "${TMPDIR:-/tmp}/openspec-archive-after.XXXXXX")
trap 'rm -f "$before_archives" "$after_archives"' EXIT

find_archives() {
  if [ -d "$archive_root" ]; then
    find "$archive_root" -mindepth 1 -maxdepth 1 -type d \
      -name "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-$change_name" -print | sort
  fi
}

if [ -d "$change_dir" ]; then
  include_active_path=true
  find_archives > "$before_archives"

  openspec validate --type change "$change_name"
  openspec archive "$change_name" --yes

  find_archives > "$after_archives"
  archive_candidates=$(comm -13 "$before_archives" "$after_archives")
else
  include_active_path=false
  archive_candidates=$(find_archives)
fi

archive_dir=$(printf '%s\n' "$archive_candidates" | sed -n '1p')
extra_archive=$(printf '%s\n' "$archive_candidates" | sed -n '2p')

if [ -z "$archive_dir" ] || [ -n "$extra_archive" ] || [ ! -d "$archive_dir" ]; then
  printf 'archive-change: expected exactly one archive directory, found:\n%s\n' "$archive_candidates" >&2
  exit 1
fi

if [ "$include_active_path" = false ]; then
  active_deleted=$(git diff --no-renames --name-only --diff-filter=D HEAD -- "$change_dir")
  archive_added=$(
    git diff --no-renames --name-only --diff-filter=A HEAD -- "$archive_dir"
    git ls-files --others --exclude-standard -- "$archive_dir"
  )
  if [ -z "$active_deleted" ] || [ -z "$archive_added" ]; then
    printf 'archive-change: active change not found and no interrupted archive move exists: %s\n' "$change_name" >&2
    exit 1
  fi
fi

if [ "$include_active_path" = true ]; then
  git add -A -- "$change_dir" "$archive_dir" openspec/specs
else
  git add -A -- "$archive_dir" openspec/specs
fi

commit_message="[archive] chore: archive openspec documents for $change_name"
ticket_prefix=${change_name%%-*}
ticket_rest=${change_name#*-}
ticket_number=${ticket_rest%%-*}
case "$ticket_prefix:$ticket_number" in
  "":*|*:*[!0-9]*)
    ;;
  *)
    if [ "$ticket_rest" != "$ticket_number" ]; then
      ticket_prefix=$(printf '%s' "$ticket_prefix" | tr '[:lower:]' '[:upper:]')
      commit_message="$ticket_prefix-$ticket_number: Archive change"
    fi
    ;;
esac

if ! git diff --cached --quiet -- "$change_dir" "$archive_dir" openspec/specs; then
  git commit -m "$commit_message" -- "$change_dir" "$archive_dir" openspec/specs
fi
agent-validator skip
