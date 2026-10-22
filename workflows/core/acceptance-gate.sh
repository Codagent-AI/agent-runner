#!/bin/sh
# Decides whether acceptance testing has converged for the current revision.
#
# Convergence requires all of:
#   - the tester's acceptance-round-status.txt ends with `READY <HEAD>`, where
#     <HEAD> is the full SHA of local HEAD;
#   - acceptance-tested-revision.txt names <HEAD>, the revision the tester
#     last explored;
#   - exploration-log.md and acceptance-handoff.md exist and are non-empty;
#   - no tracked file has uncommitted changes;
#   - when acceptance-crosscheck-required.txt exists, acceptance-crosscheck.md
#     is a well-formed review of the tester's write-up that flags no
#     `## DEFECT` section.
#
# action=check exits 0 on convergence and 1 otherwise, printing the reasons.
# action=finalize writes acceptance-preparation-status.txt with
# ACCEPTANCE_COMPLETE or ACCEPTANCE_FAILED. On failure it also writes a short
# acceptance-handoff.md, keeping any tester-written handoff as
# acceptance-handoff-tester.md. Finalize is safe to rerun.
# action=crosscheck-scope takes round (a zero-based index) and rounds. It
# writes acceptance-crosscheck-required.txt when the round would otherwise
# end: the tester wrote READY, or this is the final round.
# action=crosscheck-verify takes round and rounds and checks the shape of
# acceptance-crosscheck.md. A `## DEFECT` flag before the final round
# overwrites the round status with NOT_READY, so the lead fixes it. Any other
# flag writes acceptance-crosscheck-amend.txt, which asks the tester to amend
# its write-up.
set -eu

payload=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to read the acceptance gate input\n' >&2
  exit 1
fi
evidence_dir=$(printf '%s' "$payload" | jq -r '.evidence_dir // ""')
action=$(printf '%s' "$payload" | jq -r '.action // "check"')
rounds=$(printf '%s' "$payload" | jq -r '.rounds // ""')
round=$(printf '%s' "$payload" | jq -r '.round // ""')

if [ -z "$evidence_dir" ]; then
  printf 'acceptance gate: evidence_dir is required\n' >&2
  exit 1
fi
case "$action" in
  check|finalize|crosscheck-scope|crosscheck-verify) ;;
  *)
    printf 'acceptance gate: unknown action: %s\n' "$action" >&2
    exit 1
    ;;
esac

round_status_file="$evidence_dir/acceptance-round-status.txt"
tested_revision_file="$evidence_dir/acceptance-tested-revision.txt"
handoff_file="$evidence_dir/acceptance-handoff.md"
tester_handoff_file="$evidence_dir/acceptance-handoff-tester.md"
status_file="$evidence_dir/acceptance-preparation-status.txt"
crosscheck_file="$evidence_dir/acceptance-crosscheck.md"
crosscheck_required_file="$evidence_dir/acceptance-crosscheck-required.txt"
crosscheck_amend_file="$evidence_dir/acceptance-crosscheck-amend.txt"

last_line() {
  sed '/^[[:space:]]*$/d' "$1" 2>/dev/null | tail -n 1 | sed 's/[[:space:]]*$//'
}

# Sets crosscheck_defects, crosscheck_flags, and crosscheck_problem. The
# problem is empty when the review is well formed: exactly one verdict line,
# last in the file, that agrees with its `## DEFECT` and `## OMISSION` flags.
read_crosscheck() {
  crosscheck_defects=0
  crosscheck_flags=0
  crosscheck_problem=""
  if [ ! -s "$crosscheck_file" ]; then
    crosscheck_problem="acceptance-crosscheck.md is missing or empty"
    return
  fi
  crosscheck_defects=$(grep -c '^## DEFECT' "$crosscheck_file" || true)
  omissions=$(grep -c '^## OMISSION' "$crosscheck_file" || true)
  crosscheck_flags=$((crosscheck_defects + omissions))
  verdicts=$(grep -Ec '^CROSSCHECK (CLEAN|FLAGGED)[[:space:]]*$' "$crosscheck_file" || true)
  verdict=$(last_line "$crosscheck_file")
  if [ "$verdicts" != 1 ] || { [ "$verdict" != "CROSSCHECK CLEAN" ] && [ "$verdict" != "CROSSCHECK FLAGGED" ]; }; then
    crosscheck_problem="acceptance-crosscheck.md must end with exactly one verdict line, CROSSCHECK CLEAN or CROSSCHECK FLAGGED (found $verdicts verdict line(s); last line '$verdict')"
  elif [ "$verdict" = "CROSSCHECK CLEAN" ] && [ "$crosscheck_flags" -ne 0 ]; then
    crosscheck_problem="acceptance-crosscheck.md says CROSSCHECK CLEAN but has $crosscheck_flags flag section(s)"
  elif [ "$verdict" = "CROSSCHECK FLAGGED" ] && [ "$crosscheck_flags" -eq 0 ]; then
    crosscheck_problem="acceptance-crosscheck.md says CROSSCHECK FLAGGED but has no '## DEFECT' or '## OMISSION' flag section"
  fi
}

# Succeeds when the zero-based round index is the last allowed round.
is_final_round() {
  case "$round:$rounds" in
    :*|*:|*[!0-9:]*)
      printf 'acceptance gate: round and rounds must be numbers, got round=%s rounds=%s\n' "$round" "$rounds" >&2
      exit 1
      ;;
  esac
  [ $((round + 1)) -ge "$rounds" ]
}

if [ "$action" = crosscheck-scope ]; then
  mkdir -p "$evidence_dir"
  rm -f "$crosscheck_required_file"
  if is_final_round; then
    reason="this is the final round"
  elif last_line "$round_status_file" | grep -q '^READY'; then
    reason="the tester reported READY"
  else
    printf 'acceptance crosscheck not needed: the round does not end here\n'
    exit 0
  fi
  printf 'required: %s\n' "$reason" >"$crosscheck_required_file"
  printf 'acceptance crosscheck needed: %s\n' "$reason"
  exit 0
fi

if [ "$action" = crosscheck-verify ]; then
  rm -f "$crosscheck_amend_file"
  read_crosscheck
  if [ -n "$crosscheck_problem" ]; then
    printf 'acceptance crosscheck: %s\n' "$crosscheck_problem" >&2
    exit 1
  fi
  if [ "$crosscheck_flags" -eq 0 ]; then
    printf 'acceptance crosscheck: clean\n'
    exit 0
  fi
  if [ "$crosscheck_defects" -gt 0 ] && ! is_final_round; then
    printf 'NOT_READY\n' >"$round_status_file"
    printf 'acceptance crosscheck: flagged %s reproduced defect(s); the round is NOT_READY and goes to the lead\n' "$crosscheck_defects"
    exit 0
  fi
  printf 'amend\n' >"$crosscheck_amend_file"
  printf 'acceptance crosscheck: flagged %s item(s); the tester amends its write-up\n' "$crosscheck_flags"
  exit 0
fi

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
    round_status=$(last_line "$round_status_file")
  fi
  if [ -z "$round_status" ]; then
    add_reason "the tester recorded no round status in acceptance-round-status.txt"
  elif [ "$round_status" != "READY $head" ]; then
    add_reason "the tester's round status is '$round_status', not 'READY $head'"
  fi
  tested_revision=""
  if [ -f "$tested_revision_file" ]; then
    tested_revision=$(last_line "$tested_revision_file")
  fi
  if [ "$tested_revision" != "$head" ]; then
    add_reason "acceptance-tested-revision.txt names '${tested_revision}', not '$head'"
  fi
  for name in exploration-log.md acceptance-handoff.md; do
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

if [ -f "$crosscheck_required_file" ]; then
  read_crosscheck
  if [ -n "$crosscheck_problem" ]; then
    add_reason "the crosscheck review of the tester's write-up is required but incomplete: $crosscheck_problem"
  elif [ "$crosscheck_defects" -gt 0 ]; then
    add_reason "the crosscheck flagged $crosscheck_defects reproduced defect(s) the tester did not treat as defects; see acceptance-crosscheck.md"
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

# A rerun of finalize must not move its own earlier handoff over the tester's,
# so only a handoff without the generated heading is kept as the tester's.
generated_heading='# Acceptance did not converge within'
if [ -s "$handoff_file" ]; then
  case "$(head -n 1 "$handoff_file")" in
    "$generated_heading"*) ;;
    *) mv "$handoff_file" "$tester_handoff_file" ;;
  esac
fi

{
  printf '%s %s rounds\n\n' "$generated_heading" "${rounds:-the allowed}"
  printf 'Local HEAD: %s\n\n' "${head:-unknown}"
  printf 'Reasons:\n\n%s\n' "$reasons"
  printf 'Open findings: %s/acceptance-findings.md\n' "$evidence_dir"
  printf 'Unresolved assumptions: %s/acceptance-assumptions.md\n' "$evidence_dir"
  if [ -s "$crosscheck_file" ]; then
    printf 'Crosscheck review of the tester: %s\n' "$crosscheck_file"
  fi
  if [ -s "$tester_handoff_file" ]; then
    printf 'Tester handoff: %s\n' "$tester_handoff_file"
  fi
} >"$handoff_file"

printf 'ACCEPTANCE_FAILED\n' >"$status_file"
printf 'acceptance preparation failed; wrote %s\n%s' "$handoff_file" "$reasons"
