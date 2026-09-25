#!/bin/sh
# Verifies that the current branch has exactly one open pull request, that it
# is a draft with a non-empty base branch, and that its head equals local
# HEAD. Prints the pull request URL only when every check passes.
#
# Optional inputs: attempts (default 1) and interval_seconds (default 3) let a
# caller wait for GitHub to report a just-pushed head.
set -eu

payload=$(cat)
if [ -z "$payload" ]; then
  payload='{}'
fi

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to check the draft pull request\n' >&2
  exit 1
fi
attempts=$(printf '%s' "$payload" | jq -r '.attempts // "1"')
interval=$(printf '%s' "$payload" | jq -r '.interval_seconds // "3"')
case "$attempts" in
  ''|*[!0-9]*|0)
    printf 'attempts must be a positive integer, got: %s\n' "$attempts" >&2
    exit 1
    ;;
esac
case "$interval" in
  ''|*[!0-9]*)
    printf 'interval_seconds must be a non-negative integer, got: %s\n' "$interval" >&2
    exit 1
    ;;
esac

branch=$(git branch --show-current)
local_head=$(git rev-parse HEAD)
attempt=1
while :; do
  if pr_json=$(gh pr list --head "$branch" --state open --json number,url,state,isDraft,baseRefName,headRefOid --limit 2); then
    count=$(printf '%s' "$pr_json" | jq 'length')
    if [ "$count" -ne 1 ]; then
      detail="expected exactly one open pull request for branch '$branch', found $count"
    else
      state=$(printf '%s' "$pr_json" | jq -r '.[0].state')
      is_draft=$(printf '%s' "$pr_json" | jq -r '.[0].isDraft')
      base=$(printf '%s' "$pr_json" | jq -r '.[0].baseRefName')
      pr_head=$(printf '%s' "$pr_json" | jq -r '.[0].headRefOid')
      if [ "$state" = "OPEN" ] && [ "$is_draft" = "true" ] && [ -n "$base" ] && [ "$local_head" = "$pr_head" ]; then
        printf '%s\n' "$(printf '%s' "$pr_json" | jq -r '.[0].url')"
        exit 0
      fi
      detail="open pull request does not match required draft state or local HEAD (state $state, draft $is_draft, base '$base', head $pr_head, local HEAD $local_head)"
    fi
  else
    detail="could not query the open pull request for branch '$branch'"
  fi
  if [ "$attempt" -ge "$attempts" ]; then
    printf '%s\n' "$detail" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  sleep "$interval"
done
