#!/bin/sh
# Pushes the current branch after an acceptance fix round and verifies that
# its single open pull request is a draft whose head equals local HEAD.
#
# Optional inputs: attempts (default 10) and interval_seconds (default 3)
# bound how long to wait for GitHub to report the pushed head.
set -eu

payload=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to run the acceptance push\n' >&2
  exit 1
fi
attempts=$(printf '%s' "$payload" | jq -r '.attempts // "10"')
interval=$(printf '%s' "$payload" | jq -r '.interval_seconds // "3"')
case "$attempts" in
  ''|*[!0-9]*|0)
    printf 'acceptance push: attempts must be a positive integer, got: %s\n' "$attempts" >&2
    exit 1
    ;;
esac
case "$interval" in
  ''|*[!0-9]*)
    printf 'acceptance push: interval_seconds must be a non-negative integer, got: %s\n' "$interval" >&2
    exit 1
    ;;
esac

branch=$(git branch --show-current)
if [ -z "$branch" ]; then
  printf 'acceptance push: HEAD is detached; check out the change branch\n' >&2
  exit 1
fi
tracked_changes=$(git status --porcelain --untracked-files=no)
if [ -n "$tracked_changes" ]; then
  printf 'acceptance push: tracked changes are uncommitted; commit them before pushing\n' >&2
  git status --short --untracked-files=no >&2
  exit 1
fi

remote=$(git config "branch.$branch.remote" || true)
merge_ref=$(git config "branch.$branch.merge" || true)
if [ -n "$remote" ] && [ -n "$merge_ref" ]; then
  git push "$remote" "HEAD:$merge_ref"
else
  git push --set-upstream origin "$branch"
fi

local_head=$(git rev-parse HEAD)
attempt=1
while :; do
  detail=""
  if pr_json=$(gh pr list --head "$branch" --state open --json url,isDraft,headRefOid --limit 2); then
    count=$(printf '%s' "$pr_json" | jq 'length')
    if [ "$count" -ne 1 ]; then
      detail="expected exactly one open pull request for branch '$branch', found $count"
    else
      is_draft=$(printf '%s' "$pr_json" | jq -r '.[0].isDraft')
      pr_head=$(printf '%s' "$pr_json" | jq -r '.[0].headRefOid')
      if [ "$pr_head" != "$local_head" ]; then
        detail="pull request head $pr_head does not equal local HEAD $local_head"
      elif [ "$is_draft" != "true" ]; then
        detail="pull request for branch '$branch' is not a draft"
      else
        printf 'pushed %s; draft pull request %s is at local HEAD\n' "$local_head" "$(printf '%s' "$pr_json" | jq -r '.[0].url')"
        exit 0
      fi
    fi
  else
    detail="could not query the pull request for branch '$branch'"
  fi
  if [ "$attempt" -ge "$attempts" ]; then
    printf 'acceptance push: %s\n' "$detail" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  sleep "$interval"
done
