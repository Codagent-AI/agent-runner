#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")
session_dir=$(printf '%s' "$payload" | jq -er '.session_dir | select(type == "string" and length > 0)')
# The runner passes captured variables to script_inputs as strings, so the
# transition's JSON usually arrives as text; accept an object too.
archive_state=$(printf '%s' "$payload" | jq -c '.archive_state | if type == "string" then fromjson else . end')

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

# assert_same_set EXPECTED ACTUAL MISSING_MSG EXTRA_MSG: EXPECTED and ACTUAL
# are JSON arrays; fails naming the first element missing from ACTUAL, then
# the first element ACTUAL has beyond EXPECTED.
assert_same_set() {
  missing=$(jq -n --argjson a "$1" --argjson b "$2" '$a - $b | .[0] // empty' -r)
  if [ -n "$missing" ]; then
    printf 'verify-archive-commit: %s: %s\n' "$3" "$missing" >&2
    exit 1
  fi
  extra=$(jq -n --argjson a "$1" --argjson b "$2" '$b - $a | .[0] // empty' -r)
  if [ -n "$extra" ]; then
    printf 'verify-archive-commit: %s: %s\n' "$4" "$extra" >&2
    exit 1
  fi
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
assert_same_set "$owned_delta" "$committed" \
  'owned change not found in start_head..HEAD' \
  'unexpected committed change outside the owned archive delta'

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
assert_same_set "$prior_index" "$current_index" \
  'pre-existing staged change is missing from the index' \
  'unexpected staged change in the index'

# The worktree, repo-wide, must be clean apart from the pre-existing
# unstaged and untracked state captured by the transition snapshot: a repair
# that leaves stray uncommitted changes anywhere must be caught, and every
# pre-existing edit outside the archived paths must remain exactly as it was.
current_worktree=$(status_lines . | to_json_lines)
assert_same_set "$prior_worktree" "$current_worktree" \
  'pre-existing unstaged change is missing from the worktree' \
  'unexpected uncommitted change remains in the worktree'

printf 'archive verified: %s committed to %s\n' "$change_dir" "$archive_dir"
