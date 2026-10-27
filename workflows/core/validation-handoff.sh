#!/bin/sh
# Appends a "## Validation" section to acceptance-handoff.md: the validation
# workflow's recorded result and the commits made after the tested revision.
# Safe to rerun: an earlier section is replaced.
#
# Inputs: evidence_dir, result_file (optional).
# A missing or malformed result file is reported as "recorded no result", never
# as a failure by itself: a step's own outcome is the authoritative result and
# surfaces in the run's warnings.
set -eu

payload=$(cat)
if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to read the validation handoff input\n' >&2
  exit 1
fi
evidence_dir=$(printf '%s' "$payload" | jq -r '.evidence_dir // ""')
result_file=$(printf '%s' "$payload" | jq -r '.result_file // ""')
if [ -z "$evidence_dir" ]; then
  printf 'validation handoff: evidence_dir is required\n' >&2
  exit 1
fi

handoff="$evidence_dir/acceptance-handoff.md"
tested_file="$evidence_dir/acceptance-tested-revision.txt"
touch "$handoff"

tested=""
if [ -f "$tested_file" ]; then
  tested=$(sed '/^[[:space:]]*$/d' "$tested_file" | tail -n 1 | tr -d '[:space:]')
fi

status=""
details=""
if [ -n "$result_file" ] && [ -s "$result_file" ]; then
  status=$(sed -n '1p' "$result_file" | tr -d '[:space:]')
  details=$(sed '1d' "$result_file")
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
awk '$0 == "## Validation" { skip = 1; next } /^## / { skip = 0 } !skip' "$handoff" >"$tmp"

{
  if [ -s "$tmp" ]; then
    printf '\n'
  fi
  printf '## Validation\n\n'
  case "$status" in
    PASS) printf 'Validation passed.\n' ;;
    SKIPPED) printf 'Validation was skipped.%s\n' "${details:+ $(printf '%s' "$details" | sed -n '1p')}" ;;
    FAIL)
      printf 'Validation failed and its findings are unresolved after bounded repair:\n\n'
      printf '%s\n' "$details" | sed 's/^/    /'
      ;;
    *) printf 'The validation workflow recorded no result; see the run'\''s warnings for its outcome.\n' ;;
  esac
  printf '\n'
  if [ -n "$tested" ] && git cat-file -e "$tested^{commit}" 2>/dev/null; then
    commits=$(git log --oneline "$tested..HEAD")
    if [ -n "$commits" ]; then
      printf 'Commits made after the tested revision %s (validation fixes; not exploratory-tested):\n\n' "$tested"
      printf '%s\n' "$commits" | sed 's/^/- /'
    else
      printf 'No commits were made after the tested revision %s.\n' "$tested"
    fi
  else
    printf 'The tested revision is unknown, so commits made after it cannot be listed.\n'
  fi
} >>"$tmp"

cat "$tmp" >"$handoff"
