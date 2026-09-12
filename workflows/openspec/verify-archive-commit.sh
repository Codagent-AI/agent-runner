#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")
session_dir=$(printf '%s' "$payload" | jq -er '.session_dir | select(type == "string" and length > 0)')
archive_state=$(printf '%s' "$payload" | jq -c '.archive_state')

archive_dir=$(printf '%s' "$archive_state" | jq -er '.archive_dir')
change_dir=$(printf '%s' "$archive_state" | jq -er '.change_dir')
start_head=$(printf '%s' "$archive_state" | jq -er '.start_head')
owned_delta=$(printf '%s' "$archive_state" | jq -c '.owned_delta | sort')
prior_index=$(printf '%s' "$archive_state" | jq -c '.prior_index | sort')

specs_dir="openspec/specs"
snapshot_file="$session_dir/output/archive-transition/$change_name.json"
if [ ! -f "$snapshot_file" ]; then
  printf 'verify-archive-commit: archive transition snapshot not found: %s\n' "$snapshot_file" >&2
  exit 1
fi
prior_worktree=$(jq -c '.prior_worktree | sort' "$snapshot_file")

status_lines() {
  git diff --no-renames --name-status -- "$@"
  git ls-files --others --exclude-standard -- "$@" | sed 's/^/A\t/'
}

to_json_lines() {
  jq -R -s 'split("\n") | map(select(length > 0)) | sort'
}

# Active change directory must be gone from both the worktree and the index.
if [ -e "$change_dir" ]; then
  printf 'verify-archive-commit: active change directory still present in the worktree: %s\n' "$change_dir" >&2
  exit 1
fi
tracked_active=$(git ls-files -- "$change_dir")
if [ -n "$tracked_active" ]; then
  printf 'verify-archive-commit: active change directory still present in the index: %s\n' "$change_dir" >&2
  exit 1
fi

# Exactly the resolved archive directory must exist.
if [ ! -d "$archive_dir" ]; then
  printf 'verify-archive-commit: expected archive directory is missing: %s\n' "$archive_dir" >&2
  exit 1
fi

# start_head must be an ancestor of HEAD.
if ! git merge-base --is-ancestor "$start_head" HEAD; then
  printf 'verify-archive-commit: start commit %s is not an ancestor of HEAD\n' "$start_head" >&2
  exit 1
fi

# The committed delta start_head..HEAD, restricted to the allowed paths,
# must equal the transition-owned delta exactly.
committed=$(git diff --no-renames --name-status "$start_head" HEAD -- "$change_dir" "$archive_dir" "$specs_dir" | to_json_lines)
missing=$(jq -n --argjson owned "$owned_delta" --argjson committed "$committed" '$owned - $committed | .[0] // empty' -r)
if [ -n "$missing" ]; then
  printf 'verify-archive-commit: owned change not found in start_head..HEAD: %s\n' "$missing" >&2
  exit 1
fi
extra=$(jq -n --argjson owned "$owned_delta" --argjson committed "$committed" '$committed - $owned | .[0] // empty' -r)
if [ -n "$extra" ]; then
  printf 'verify-archive-commit: unexpected committed change outside the owned archive delta: %s\n' "$extra" >&2
  exit 1
fi

# The commit range must touch no path outside the allowed paths.
outside=$(git diff --no-renames --name-only "$start_head" HEAD -- . \
  ":(exclude)$change_dir" ":(exclude)$change_dir/*" \
  ":(exclude)$archive_dir" ":(exclude)$archive_dir/*" \
  ":(exclude)$specs_dir" ":(exclude)$specs_dir/*" | sed -n '1p')
if [ -n "$outside" ]; then
  printf 'verify-archive-commit: commit range touched a path outside the allowed archive paths: %s\n' "$outside" >&2
  exit 1
fi

# The index outside the owned delta must equal the pre-existing staged state,
# repo-wide: a repair that stages or unstages anything unrelated, anywhere in
# the repository, must be caught, not just inside the allowed archive paths.
current_index=$(git diff --no-renames --cached --name-status | to_json_lines)
index_missing=$(jq -n --argjson prior "$prior_index" --argjson current "$current_index" '$prior - $current | .[0] // empty' -r)
if [ -n "$index_missing" ]; then
  printf 'verify-archive-commit: pre-existing staged change is missing from the index: %s\n' "$index_missing" >&2
  exit 1
fi
index_extra=$(jq -n --argjson prior "$prior_index" --argjson current "$current_index" '$current - $prior | .[0] // empty' -r)
if [ -n "$index_extra" ]; then
  printf 'verify-archive-commit: unexpected staged change in the index: %s\n' "$index_extra" >&2
  exit 1
fi

# The worktree, repo-wide, must be clean apart from the pre-existing
# unstaged and untracked state captured by the transition snapshot: a repair
# that leaves stray uncommitted changes anywhere must be caught, and every
# pre-existing edit outside the archived paths must remain exactly as it was.
current_worktree=$(status_lines . | to_json_lines)
worktree_missing=$(jq -n --argjson prior "$prior_worktree" --argjson current "$current_worktree" '$prior - $current | .[0] // empty' -r)
if [ -n "$worktree_missing" ]; then
  printf 'verify-archive-commit: pre-existing unstaged change is missing from the worktree: %s\n' "$worktree_missing" >&2
  exit 1
fi
worktree_extra=$(jq -n --argjson prior "$prior_worktree" --argjson current "$current_worktree" '$current - $prior | .[0] // empty' -r)
if [ -n "$worktree_extra" ]; then
  printf 'verify-archive-commit: unexpected uncommitted change remains in the worktree: %s\n' "$worktree_extra" >&2
  exit 1
fi

printf 'archive verified: %s committed to %s\n' "$change_dir" "$archive_dir"
