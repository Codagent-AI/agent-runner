#!/bin/sh
# Pushes the current branch after an acceptance fix round without
# force-pushing. The workflow's check-draft-pr step then verifies the draft
# pull request's head.
set -eu

branch=$(git branch --show-current)
if [ -z "$branch" ]; then
  printf 'acceptance push: HEAD is detached; check out the change branch\n' >&2
  exit 1
fi
tracked_changes=$(git status --porcelain --untracked-files=no)
if [ -n "$tracked_changes" ]; then
  printf 'acceptance push: tracked changes are uncommitted; commit them before pushing\n' >&2
  printf '%s\n' "$tracked_changes" >&2
  exit 1
fi

remote=$(git config "branch.$branch.remote" || true)
merge_ref=$(git config "branch.$branch.merge" || true)
if [ -n "$remote" ] && [ -n "$merge_ref" ]; then
  git push "$remote" "HEAD:$merge_ref"
else
  git push --set-upstream origin "$branch"
fi
