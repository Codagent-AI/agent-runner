#!/usr/bin/env bash
set -euo pipefail

# release-info.sh — Gather merged PRs, classify by type, calculate version bump.
# Output: JSON with current_version, new_version, and PRs grouped by bump type.
# Works from any branch: includes merged-to-main PRs and the current branch PR.

REPO="${GITHUB_REPOSITORY:-Codagent-AI/agent-runner}"
VERSION_FILE="VERSION"

CURRENT_BRANCH=$(git branch --show-current)
BRANCH_PR=""
AHEAD=0

git fetch origin main --tags --quiet

if [ "$CURRENT_BRANCH" = "main" ]; then
  git pull origin main --quiet
else
  AHEAD=$(git log --oneline origin/main..HEAD | wc -l | tr -d ' ')
  if [ "$AHEAD" -gt 0 ]; then
    BRANCH_PR=$(gh pr view --json number,title,labels,state,url,baseRefName,headRefName,mergedAt 2>&1) || {
      jq -n '{"error": "no_pr", "message": "Current branch has changes not on main but no PR exists. Create a PR first."}'
      exit 1
    }
    BRANCH_PR_STATE=$(echo "$BRANCH_PR" | jq -r '.state')
    BRANCH_PR_BASE=$(echo "$BRANCH_PR" | jq -r '.baseRefName // ""')
    if [ "$BRANCH_PR_STATE" != "OPEN" ]; then
      BRANCH_PR_NUMBER=$(echo "$BRANCH_PR" | jq -r '.number')
      BRANCH_PR_URL=$(echo "$BRANCH_PR" | jq -r '.url')
      jq -n \
        --arg number "$BRANCH_PR_NUMBER" \
        --arg state "$BRANCH_PR_STATE" \
        --arg url "$BRANCH_PR_URL" \
        '{"error": "branch_pr_not_open", "message": "Current branch has changes not on main, but its associated PR #\($number) is \($state). Create a new branch/PR for the unreleased changes before running release.", "url": $url}'
      exit 1
    fi
    if [ "$BRANCH_PR_BASE" != "main" ]; then
      BRANCH_PR_NUMBER=$(echo "$BRANCH_PR" | jq -r '.number')
      BRANCH_PR_URL=$(echo "$BRANCH_PR" | jq -r '.url')
      jq -n \
        --arg number "$BRANCH_PR_NUMBER" \
        --arg base "$BRANCH_PR_BASE" \
        --arg url "$BRANCH_PR_URL" \
        '{"error": "branch_pr_wrong_base", "message": "Current branch PR #\($number) targets \($base), but release candidates must target main.", "url": $url}'
      exit 1
    fi
  fi

  if ! git merge-tree --write-tree HEAD origin/main >/dev/null 2>&1; then
    jq -n '{"error": "merge_conflict", "message": "Merge conflicts when merging origin/main. Resolve conflicts first."}'
    exit 1
  fi
fi

LAST_TAG=$(git tag --list 'v*' --sort=-v:refname | head -1)
if [ -z "$LAST_TAG" ]; then
  TAG_EPOCH=0
  SEARCH_DATE="1970-01-01"
else
  RELEASE_PUBLISHED_AT=$(gh release view "$LAST_TAG" --repo "$REPO" --json publishedAt --jq .publishedAt 2>/dev/null || true)
  if [ -n "$RELEASE_PUBLISHED_AT" ] && [ "$RELEASE_PUBLISHED_AT" != "null" ]; then
    TAG_EPOCH=$(jq -n --arg published_at "$RELEASE_PUBLISHED_AT" '$published_at | fromdateiso8601')
    SEARCH_DATE=${RELEASE_PUBLISHED_AT%%T*}
  else
    TAG_EPOCH=$(git for-each-ref --format='%(creatordate:unix)' "refs/tags/$LAST_TAG")
    SEARCH_DATE=$(git for-each-ref --format='%(creatordate:short)' "refs/tags/$LAST_TAG")
    if [ -z "$TAG_EPOCH" ]; then
      TAG_EPOCH=$(git log -1 --format=%ct "$LAST_TAG")
      SEARCH_DATE=$(git log -1 --format=%cd --date=short "$LAST_TAG")
    fi
  fi
fi

CURRENT_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
if [[ ! "$CURRENT_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  jq -n --arg v "$CURRENT_VERSION" '{"error": "invalid_version", "message": "Invalid version in VERSION: \($v)"}'
  exit 1
fi

PRS=$(gh pr list --repo "$REPO" --state merged --base main \
  --search "merged:>=$SEARCH_DATE" \
  --json number,title,mergedAt,labels --limit 100)

PR_COUNT=$(echo "$PRS" | jq 'length')
if [ "$PR_COUNT" -ge 100 ]; then
  jq -n '{"error": "too_many_prs", "message": "PR query returned 100 results. Results may be truncated."}'
  exit 1
fi

PRS=$(echo "$PRS" | jq --argjson tag_epoch "$TAG_EPOCH" '
  [.[] | select((.mergedAt | fromdateiso8601) > $tag_epoch)]
')

if [ "$CURRENT_BRANCH" != "main" ] && [ "$AHEAD" -gt 0 ] && [ -n "$BRANCH_PR" ]; then
  PRS=$(jq -s '.[0] + [.[1]] | unique_by(.number)' <(echo "$PRS") <(echo "$BRANCH_PR"))
fi

RESULT=$(echo "$PRS" | jq --arg cv "$CURRENT_VERSION" '
  def labels:
    (.labels // []) | map(if type == "object" then .name else . end);

  [.[] | select(
    (.title | test("^chore: (release|version packages)") | not) and
    (.title | test("^chore\\(release\\)") | not)
  )] |
  {
    current_version: $cv,
    major: [.[] | select(
      (.title | test("^\\w+(\\(.*\\))?!:")) or
      (labels | any(. == "breaking"))
    )] | sort_by(.number),
    minor: [.[] | select(
      (.title | test("^feat(\\(.*\\))?:")) and
      (.title | test("^\\w+(\\(.*\\))?!:") | not) and
      ((labels | any(. == "breaking")) | not)
    )] | sort_by(.number),
    patch: [.[] | select(
      (.title | test("^feat(\\(.*\\))?:") | not) and
      (.title | test("^\\w+(\\(.*\\))?!:") | not) and
      ((labels | any(. == "breaking")) | not)
    )] | sort_by(.number)
  } |
  if (.major | length) + (.minor | length) + (.patch | length) == 0 then
    {error: "nothing_to_release", message: "No qualifying PRs found since last release."}
  else
    ($cv | split(".") | map(tonumber)) as [$maj, $min, $pat] |
    if (.major | length) > 0 then
      .new_version = "\($maj + 1).0.0"
    elif (.minor | length) > 0 then
      .new_version = "\($maj).\($min + 1).0"
    else
      .new_version = "\($maj).\($min).\($pat + 1)"
    end
  end
')

echo "$RESULT"
