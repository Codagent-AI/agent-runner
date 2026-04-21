#!/usr/bin/env bash
set -euo pipefail

# auto-release.sh — Calculate semver bump from conventional commit PR titles,
# generate a changelog section, and update CHANGELOG.md.
#
# Outputs (GitHub Actions):
#   new_version=<version>   — set when there's something to release
#
# Exit codes:
#   0 — success (check new_version output to see if a release was created)

REPO="${GITHUB_REPOSITORY:-Codagent-AI/agent-runner}"

# --- Find last release tag ---
LAST_TAG=$(git tag --list 'v*' --sort=-v:refname | head -1)
if [ -z "$LAST_TAG" ]; then
  CURRENT_VERSION="0.0.0"
  TAG_DATE="1970-01-01T00:00:00Z"
else
  CURRENT_VERSION="${LAST_TAG#v}"
  TAG_DATE=$(git log -1 --format=%cI "$LAST_TAG")
fi

echo "Current version: ${CURRENT_VERSION} (tag: ${LAST_TAG:-none})"

# --- Fetch merged PRs since last tag ---
PRS=$(gh pr list --repo "$REPO" --state merged --base main \
  --search "merged:>$TAG_DATE" \
  --json number,title --limit 100)

PR_COUNT=$(echo "$PRS" | jq 'length')
if [ "$PR_COUNT" -ge 100 ]; then
  echo "Error: PR query returned ${PR_COUNT} results (limit 100). Results may be truncated." >&2
  exit 1
fi

# --- Filter out release PRs and classify by conventional commit prefix ---
CLASSIFIED=$(echo "$PRS" | jq '
  [.[] | select(
    (.title | test("^chore\\(release\\)") | not) and
    (.title | test("^chore: release") | not)
  )] |
  {
    major: [.[] | select(
      .title | test("^\\w+(\\(.*\\))?!:")
    ) | {number, title}] | sort_by(.number),

    minor: [.[] | select(
      (.title | test("^feat(\\(.*\\))?:")) and
      (.title | test("^\\w+(\\(.*\\))?!:") | not)
    ) | {number, title}] | sort_by(.number),

    patch: [.[] | select(
      (.title | test("^feat(\\(.*\\))?:") | not) and
      (.title | test("^\\w+(\\(.*\\))?!:") | not)
    ) | {number, title}] | sort_by(.number)
  }
')

# --- Check if anything to release ---
TOTAL=$(echo "$CLASSIFIED" | jq '(.major | length) + (.minor | length) + (.patch | length)')
if [ "$TOTAL" -eq 0 ]; then
  echo "No qualifying PRs found since ${LAST_TAG:-repo creation}. Skipping release."
  exit 0
fi

# --- Calculate new version ---
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"
MAJOR_COUNT=$(echo "$CLASSIFIED" | jq '.major | length')
MINOR_COUNT=$(echo "$CLASSIFIED" | jq '.minor | length')

if [ "$MAJOR_COUNT" -gt 0 ]; then
  NEW_VERSION="$((MAJOR + 1)).0.0"
elif [ "$MINOR_COUNT" -gt 0 ]; then
  NEW_VERSION="${MAJOR}.$((MINOR + 1)).0"
else
  NEW_VERSION="${MAJOR}.${MINOR}.$((PATCH + 1))"
fi

echo "New version: ${NEW_VERSION} (major=${MAJOR_COUNT}, minor=${MINOR_COUNT}, patch=$((TOTAL - MAJOR_COUNT - MINOR_COUNT)))"

# --- Generate changelog section ---
generate_entries() {
  local category="$1"
  echo "$CLASSIFIED" | jq -r --arg cat "$category" --arg repo "$REPO" '
    .[$cat][] |
    (.title | sub("^\\w+(\\(.*\\))?!?:\\s*"; "") |
     (.[0:1] | ascii_upcase) + .[1:]) as $desc |
    "- [#\(.number)](https://github.com/\($repo)/pull/\(.number)) \($desc)"
  '
}

CHANGELOG_SECTION="## ${NEW_VERSION}"

MAJOR_ENTRIES=$(generate_entries "major")
MINOR_ENTRIES=$(generate_entries "minor")
PATCH_ENTRIES=$(generate_entries "patch")

[ -n "$MAJOR_ENTRIES" ] && CHANGELOG_SECTION="${CHANGELOG_SECTION}

### Major Changes

${MAJOR_ENTRIES}"

[ -n "$MINOR_ENTRIES" ] && CHANGELOG_SECTION="${CHANGELOG_SECTION}

### Minor Changes

${MINOR_ENTRIES}"

[ -n "$PATCH_ENTRIES" ] && CHANGELOG_SECTION="${CHANGELOG_SECTION}

### Patch Changes

${PATCH_ENTRIES}"

# --- Update CHANGELOG.md ---
CHANGELOG="CHANGELOG.md"
if [ -f "$CHANGELOG" ]; then
  {
    head -1 "$CHANGELOG"
    echo ""
    echo "$CHANGELOG_SECTION"
    tail -n +2 "$CHANGELOG"
  } > "${CHANGELOG}.tmp" && mv "${CHANGELOG}.tmp" "$CHANGELOG"
else
  {
    echo "# agent-runner"
    echo ""
    echo "$CHANGELOG_SECTION"
  } > "$CHANGELOG"
fi

echo ""
echo "Changelog updated."

# --- Set GitHub Actions output ---
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "new_version=${NEW_VERSION}" >> "$GITHUB_OUTPUT"
fi
