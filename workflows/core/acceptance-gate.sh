#!/bin/sh
# Decides whether acceptance testing has converged for the current revision.
#
# Convergence requires all of:
#   - the tester's acceptance-round-status.txt ends with `READY <HEAD>`, where
#     <HEAD> is the full SHA of local HEAD;
#   - acceptance-test.md and acceptance-handoff.md exist and are non-empty;
#   - no tracked file has uncommitted changes.
#
# action=check exits 0 on convergence and 1 otherwise, printing the reasons.
# action=finalize writes acceptance-preparation-status.txt with
# ACCEPTANCE_COMPLETE or ACCEPTANCE_FAILED. On failure it also writes a short
# acceptance-handoff.md, keeping any tester-written handoff as
# acceptance-handoff-tester.md.
set -eu

payload=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to read the acceptance gate input\n' >&2
  exit 1
fi
evidence_dir=$(printf '%s' "$payload" | jq -r '.evidence_dir // ""')
action=$(printf '%s' "$payload" | jq -r '.action // "check"')
rounds=$(printf '%s' "$payload" | jq -r '.rounds // ""')

if [ -z "$evidence_dir" ]; then
  printf 'acceptance gate: evidence_dir is required\n' >&2
  exit 1
fi
case "$action" in
  check|finalize) ;;
  *)
    printf 'acceptance gate: unknown action: %s\n' "$action" >&2
    exit 1
    ;;
esac

round_status_file="$evidence_dir/acceptance-round-status.txt"
handoff_file="$evidence_dir/acceptance-handoff.md"
tester_handoff_file="$evidence_dir/acceptance-handoff-tester.md"
status_file="$evidence_dir/acceptance-preparation-status.txt"

head=$(git rev-parse HEAD 2>/dev/null || true)
reasons=""

add_reason() {
  reasons="${reasons}- $1
"
}

if [ -z "$head" ]; then
  add_reason "local HEAD could not be resolved"
else
  round_status=""
  if [ -f "$round_status_file" ]; then
    round_status=$(sed '/^[[:space:]]*$/d' "$round_status_file" | tail -n 1 | sed 's/[[:space:]]*$//')
  fi
  if [ -z "$round_status" ]; then
    add_reason "the tester recorded no round status in acceptance-round-status.txt"
  elif [ "$round_status" != "READY $head" ]; then
    add_reason "the tester's round status is '$round_status', not 'READY $head'"
  fi
  for name in acceptance-test.md acceptance-handoff.md; do
    if [ ! -s "$evidence_dir/$name" ]; then
      add_reason "$name is missing or empty"
    fi
  done
  if tracked_changes=$(git status --porcelain --untracked-files=no); then
    if [ -n "$tracked_changes" ]; then
      add_reason "tracked files have uncommitted changes"
    fi
  else
    add_reason "could not inspect tracked changes"
  fi
fi

if [ "$action" = check ]; then
  if [ -z "$reasons" ]; then
    printf 'acceptance gate: converged at %s\n' "$head"
    exit 0
  fi
  printf 'acceptance gate: not converged:\n%s' "$reasons" >&2
  exit 1
fi

mkdir -p "$evidence_dir"

if [ -z "$reasons" ]; then
  printf 'ACCEPTANCE_COMPLETE\n' >"$status_file"
  printf 'acceptance preparation complete at %s\n' "$head"
  exit 0
fi

if [ -s "$handoff_file" ]; then
  mv "$handoff_file" "$tester_handoff_file"
fi

{
  printf '# Acceptance did not converge within %s rounds\n\n' "${rounds:-the allowed}"
  printf 'Local HEAD: %s\n\n' "${head:-unknown}"
  printf 'Reasons:\n\n%s\n' "$reasons"
  printf 'Open findings: %s/acceptance-findings.md\n' "$evidence_dir"
  printf 'Unresolved assumptions: %s/acceptance-assumptions.md\n' "$evidence_dir"
  if [ -s "$tester_handoff_file" ]; then
    printf 'Tester handoff: %s\n' "$tester_handoff_file"
  fi
} >"$handoff_file"

printf 'ACCEPTANCE_FAILED\n' >"$status_file"
printf 'acceptance preparation failed; wrote %s\n%s' "$handoff_file" "$reasons"
