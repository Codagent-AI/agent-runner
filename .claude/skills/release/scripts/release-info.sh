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
    BRANCH_PR=$(gh pr view --json number,title,labels 2>&1) || {
      jq -n '{"error": "no_pr", "message": "Current branch has changes not on main but no PR exists. Create a PR first."}'
      exit 1
    }
  fi

  if ! git merge origin/main --no-edit --quiet 2>/dev/null; then
    jq -n '{"error": "merge_conflict", "message": "Merge conflicts when merging origin/main. Resolve conflicts first."}'
    exit 1
  fi
fi

LAST_TAG=$(git tag --list 'v*' --sort=-v:refname | head -1)
if [ -z "$LAST_TAG" ]; then
  TAG_DATE="1970-01-01T00:00:00Z"
else
  TAG_DATE=$(git log -1 --format=%cI "$LAST_TAG")
fi

CURRENT_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
if [[ ! "$CURRENT_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  jq -n --arg v "$CURRENT_VERSION" '{"error": "invalid_version", "message": "Invalid version in VERSION: \($v)"}'
  exit 1
fi

PRS=$(gh pr list --repo "$REPO" --state merged --base main \
  --search "merged:>$TAG_DATE" \
  --json number,title,mergedAt,labels --limit 100)

PR_COUNT=$(echo "$PRS" | jq 'length')
if [ "$PR_COUNT" -ge 100 ]; then
  jq -n '{"error": "too_many_prs", "message": "PR query returned 100 results. Results may be truncated."}'
  exit 1
fi

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
