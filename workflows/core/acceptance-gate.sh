#!/bin/sh
# Decides whether acceptance testing has converged for the current revision.
#
# Convergence requires all of:
#   - the tester's acceptance-round-status.txt ends with `READY <HEAD>`, where
#     <HEAD> is the full SHA of local HEAD;
#   - acceptance-test.md and acceptance-handoff.md are non-empty and each names
#     local HEAD (its full SHA or an abbreviation of at least 7 characters);
#   - no tracked file has uncommitted changes.
#
# action=check exits 0 on convergence and 1 otherwise, printing the reasons.
# action=finalize writes acceptance-preparation-status.txt with
# ACCEPTANCE_COMPLETE or ACCEPTANCE_FAILED. On failure it also writes an
# acceptance-handoff.md that lists the open findings and available evidence,
# keeping any tester-written handoff as acceptance-handoff-tester.md.
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
test_file="$evidence_dir/acceptance-test.md"
handoff_file="$evidence_dir/acceptance-handoff.md"
findings_file="$evidence_dir/acceptance-findings.md"
status_file="$evidence_dir/acceptance-preparation-status.txt"

head=$(git rev-parse HEAD 2>/dev/null || true)
reasons=""

add_reason() {
  reasons="${reasons}- $1
"
}

# names_head FILE succeeds when FILE contains HEAD's full SHA or an
# abbreviation of it that is at least 7 hexadecimal characters long.
names_head() {
  for token in $(grep -Eoi '[0-9a-f]{7,40}' "$1" 2>/dev/null | tr 'A-F' 'a-f'); do
    case "$head" in
      "$token"*) return 0 ;;
    esac
  done
  return 1
}

if [ -z "$head" ]; then
  add_reason "local HEAD could not be resolved"
else
  if [ -f "$round_status_file" ]; then
    round_status=$(sed '/^[[:space:]]*$/d' "$round_status_file" | tail -n 1 | sed 's/[[:space:]]*$//')
  else
    round_status=""
  fi
  if [ "$round_status" != "READY $head" ]; then
    if [ -z "$round_status" ]; then
      add_reason "the tester recorded no round status in acceptance-round-status.txt"
    else
      add_reason "the tester's round status is '$round_status', not 'READY $head'"
    fi
  fi
  for evidence in "$test_file" "$handoff_file"; do
    name=$(basename "$evidence")
    if [ ! -s "$evidence" ]; then
      add_reason "$name is missing or empty"
    elif ! names_head "$evidence"; then
      add_reason "$name does not name the current revision $head"
    fi
  done
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    add_reason "tracked files have uncommitted changes"
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
  mv "$handoff_file" "$evidence_dir/acceptance-handoff-tester.md"
fi

{
  printf '# Acceptance handoff: testing did not converge\n\n'
  if [ -n "$rounds" ]; then
    printf 'Automated acceptance testing did not converge within %s rounds. ' "$rounds"
  else
    printf 'Automated acceptance testing did not converge. '
  fi
  printf 'The change still needs the open findings below resolved or accepted by a human.\n\n'
  printf '## Revision\n\n'
  printf -- '- Local HEAD: %s\n\n' "${head:-unknown}"
  printf '## Why acceptance is incomplete\n\n%s\n' "$reasons"
  printf '## Open findings\n\n'
  if [ -s "$findings_file" ]; then
    printf 'From `%s`:\n\n' "$findings_file"
    cat "$findings_file"
    printf '\n'
  else
    printf 'The tester recorded no findings file (`%s`).\n' "$findings_file"
  fi
  printf '\n## Available evidence\n\n'
  found_evidence=false
  for evidence in "$evidence_dir"/acceptance-*.md "$evidence_dir"/acceptance-round-status.txt "$evidence_dir"/acceptance-screenshots; do
    if [ -e "$evidence" ] && [ "$evidence" != "$handoff_file" ]; then
      printf -- '- `%s`\n' "$evidence"
      found_evidence=true
    fi
  done
  if [ "$found_evidence" = false ]; then
    printf 'No acceptance evidence files were found in `%s`.\n' "$evidence_dir"
  fi
  printf '\n## Assumptions\n\n'
  printf 'Unresolved assumptions and context gaps are in `%s/acceptance-assumptions.md`.\n' "$evidence_dir"
} >"$handoff_file"

printf 'ACCEPTANCE_FAILED\n' >"$status_file"
printf 'acceptance preparation failed; wrote %s\n' "$handoff_file"
printf '%s' "$reasons"
exit 0
