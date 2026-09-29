#!/bin/sh
set -eu

payload=$(cat)
if command -v jq >/dev/null 2>&1; then
  report=$(printf '%s' "$payload" | jq -r '.report // ""')
else
  report=$(printf '%s' "$payload" | sed -n 's/.*"report"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

status=$(printf '%s\n' "$report" | sed '/^[[:space:]]*$/d' | tail -n 1)
case "$status" in
  CI_PASSED|CI_REVIEW_INCOMPLETE) ;;
  *) echo 'ci-reuse-report: last report did not pass' >&2; exit 1 ;;
esac

head=$(printf '%s\n' "$report" | sed -n 's/^\*\*Head:\*\* \([0-9a-f]\{40\}\)$/\1/p' | head -n 1)
if [ -z "$head" ] || ! command -v gh >/dev/null 2>&1; then
  echo 'ci-reuse-report: head or gh unavailable' >&2
  exit 1
fi
current=$(gh pr view --json headRefOid -q .headRefOid) || {
  echo 'ci-reuse-report: PR head lookup failed' >&2
  exit 1
}
if [ "$head" != "$current" ]; then
  echo 'ci-reuse-report: PR head changed' >&2
  exit 1
fi
printf '%s\n' "$report"
