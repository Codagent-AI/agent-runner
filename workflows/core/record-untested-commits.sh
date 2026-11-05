#!/bin/sh
# Appends a "## Commits not exploratory-tested" section to acceptance-handoff.md
# listing the commits since the latest recorded tested revision. Covers a
# declined re-acceptance, validation commits that were not retested, and fixes
# made after the last retest. Writes nothing when the tested revision equals
# HEAD. Safe to rerun: an earlier section is replaced.
#
# Input: evidence_dir.
set -eu

payload=$(cat)
if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to read the untested commits input\n' >&2
  exit 1
fi
evidence_dir=$(printf '%s' "$payload" | jq -r '.evidence_dir // ""')
if [ -z "$evidence_dir" ]; then
  printf 'untested commits: evidence_dir is required\n' >&2
  exit 1
fi

handoff="$evidence_dir/acceptance-handoff.md"
tested_file="$evidence_dir/acceptance-tested-revision.txt"
touch "$handoff"

heading="## Commits not exploratory-tested"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
awk -v h="$heading" '$0 == h { skip = 1; next } /^## / { skip = 0 } !skip' "$handoff" >"$tmp"

tested=""
if [ -f "$tested_file" ]; then
  tested=$(sed '/^[[:space:]]*$/d' "$tested_file" | tail -n 1 | tr -d '[:space:]')
fi
head=$(git rev-parse HEAD)

if [ -n "$tested" ] && [ "$tested" = "$head" ]; then
  cat "$tmp" >"$handoff"
  exit 0
fi

{
  if [ -s "$tmp" ]; then
    printf '\n'
  fi
  printf '%s\n\n' "$heading"
  if [ -n "$tested" ] && git cat-file -e "$tested^{commit}" 2>/dev/null; then
    printf 'These commits were made after the last tested revision %s and received no exploratory testing:\n\n' "$tested"
    git log --oneline "$tested..HEAD" | sed 's/^/- /'
  else
    printf 'No tested revision is recorded, so the whole change since its merge base received no recorded exploratory testing at this revision (%s).\n' "$head"
  fi
} >>"$tmp"

cat "$tmp" >"$handoff"
