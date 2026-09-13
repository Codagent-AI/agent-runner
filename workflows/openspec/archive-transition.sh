#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")
session_dir=$(printf '%s' "$payload" | jq -er '.session_dir | select(type == "string" and length > 0)')

change_dir="openspec/changes/$change_name"
exclude_change=":(exclude)$change_dir :(exclude)$change_dir/*"
archive_root="openspec/changes/archive"
specs_dir="openspec/specs"

snapshot_dir="$session_dir/output/archive-transition"
snapshot_file="$snapshot_dir/$change_name.json"
mkdir -p "$snapshot_dir"

find_archive_dirs() {
  if [ -d "$archive_root" ]; then
    find "$archive_root" -mindepth 1 -maxdepth 1 -type d \
      -name "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-$change_name" | sort
  fi
}

status_lines() {
  # Emits "STATUS<TAB>PATH" for tracked worktree changes and "A<TAB>PATH"
  # for untracked files, restricted to the given paths.
  git diff --no-renames --name-status -- "$@"
  git ls-files --others --exclude-standard -- "$@" | sed 's/^/A\t/'
}

# The baseline covers the whole repository except the active change
# directory: archiving fully owns whatever state that directory holds
# (clean or dirty), so any pre-existing edit inside it becomes part of the
# owned move rather than state that must survive unchanged. Every other path
# in the repository, including specs and any pre-existing archive directory,
# is preserved-as-is state that verification must find untouched outside the
# owned delta.
repo_index_lines() {
  # shellcheck disable=SC2086
  git diff --no-renames --cached --name-status -- . $exclude_change
}

repo_worktree_lines() {
  # shellcheck disable=SC2086
  status_lines . $exclude_change
}

to_json_lines() {
  jq -R -s 'split("\n") | map(select(length > 0))'
}

# Emits "PATH<TAB>BLOB" for every path in the given "STATUS<TAB>PATH" lines
# that still exists in the worktree, so verification can prove pre-existing
# edits kept their content, not just their status.
content_lines() {
  while IFS="$(printf '\t')" read -r _status path; do
    [ -n "$path" ] || continue
    [ -f "$path" ] || continue
    printf '%s\t%s\n' "$path" "$(git hash-object -- "$path")"
  done
}

if [ -f "$snapshot_file" ]; then
  start_head=$(jq -r '.start_head' "$snapshot_file")
  prior_index_json=$(jq -c '.prior_index' "$snapshot_file")
  prior_worktree_json=$(jq -c '.prior_worktree' "$snapshot_file")
else
  start_head=$(git rev-parse --verify HEAD)
  prior_index_json=$(repo_index_lines | to_json_lines)
  prior_worktree_json=$(repo_worktree_lines | to_json_lines)
  prior_content_json=$(repo_worktree_lines | content_lines | to_json_lines)
  jq -n \
    --arg start_head "$start_head" \
    --argjson prior_index "$prior_index_json" \
    --argjson prior_worktree "$prior_worktree_json" \
    --argjson prior_content "$prior_content_json" \
    '{start_head: $start_head, prior_index: $prior_index, prior_worktree: $prior_worktree, prior_content: $prior_content}' \
    > "$snapshot_file"
fi

# stdout is the captured archive_state that verify-archive-commit parses as
# JSON, so the CLI's own progress output must go to stderr.
if [ -d "$change_dir" ]; then
  openspec validate --type change "$change_name" >&2
  openspec archive "$change_name" --yes >&2
fi

archive_candidates=$(find_archive_dirs)
archive_dir=$(printf '%s\n' "$archive_candidates" | sed -n '1p')
extra_archive=$(printf '%s\n' "$archive_candidates" | sed -n '2p')
if [ -z "$archive_dir" ] || [ -n "$extra_archive" ] || [ ! -d "$archive_dir" ]; then
  printf 'archive-transition: expected exactly one archive directory for %s, found:\n%s\n' "$change_name" "$archive_candidates" >&2
  exit 1
fi

current_worktree_json=$(status_lines "$change_dir" "$archive_dir" "$specs_dir" | to_json_lines)

# prior_worktree never contains a change_dir entry (excluded from the
# baseline) and never contains an archive_dir entry (it did not exist yet),
# so subtracting it here only ever removes pre-existing specs_dir noise from
# the narrowly-scoped current set above.
owned_delta_json=$(jq -n \
  --argjson current "$current_worktree_json" \
  --argjson prior "$prior_worktree_json" \
  '$current - $prior')

jq -n \
  --arg archive_dir "$archive_dir" \
  --arg change_dir "$change_dir" \
  --arg start_head "$start_head" \
  --argjson owned_delta "$owned_delta_json" \
  --argjson prior_index "$prior_index_json" \
  '{archive_dir: $archive_dir, change_dir: $change_dir, start_head: $start_head, owned_delta: $owned_delta, prior_index: $prior_index}'
