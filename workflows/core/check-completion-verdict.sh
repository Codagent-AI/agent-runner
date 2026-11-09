#!/bin/sh
# Validate the judge output and enforce durable per-verdict budgets.
set -eu
# Script captures are decoded even on non-zero exit. Keep failures decodable
# so the runner reports the script diagnostic rather than a JSON parse error.
trap 'status=$?; if [ "$status" -ne 0 ]; then printf "{}\n"; fi' 0
command -v jq >/dev/null 2>&1 || { echo 'jq is required to read the completion verdict input' >&2; exit 1; }
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
payload=$(cat)
verdict_file=$(printf '%s' "$payload" | jq -r '.verdict_file')
check_file=$(printf '%s' "$payload" | jq -r '.check_file')
ledger_file=$(printf '%s' "$payload" | jq -r '.ledger_file')
blocked_file=$(printf '%s' "$payload" | jq -r '.blocked_file')
remaining_file=$(printf '%s' "$payload" | jq -r '.remaining_file')
snapshot_file=$(printf '%s' "$payload" | jq -r '.snapshot_file')
checklist_file=$(printf '%s' "$payload" | jq -r '.checklist_file')
max_continue=$(printf '%s' "$payload" | jq -r '.max_continue // "3"')
max_fix_record=$(printf '%s' "$payload" | jq -r '.max_fix_record // "2"')
invalid() { printf 'completion judge returned no valid verdict: %s\n' "$1" >&2; exit 1; }
# A recorded verdict is authoritative on a resumed rerun. Keep its original
# remaining list and snapshot instead of counting or refreshing the pass.
[ -s "$ledger_file" ] || invalid 'no open judgment'
recorded=$(jq -s 'last | has("verdict")' "$ledger_file")
if [ "$recorded" = true ]; then
  verdict=$(jq -sc 'last' "$ledger_file")
else
  [ -s "$verdict_file" ] || invalid 'missing verdict file'
  verdict=$(jq -cse 'if length == 1 and (.[0] | type == "object") then .[0] else error("expected one object") end' "$verdict_file" 2>/dev/null) || invalid 'malformed JSON'
fi
# Match the exact JSON string: command substitution would strip a trailing
# newline from "continue\n" and let it dispatch as continue uncounted.
printf '%s' "$verdict" | jq -e '.verdict | IN("blocked", "continue", "fix-record", "complete")' >/dev/null 2>&1 || invalid 'unknown verdict'
word=$(printf '%s' "$verdict" | jq -r '.verdict')
case "$word" in
  continue)
    printf '%s' "$verdict" | jq -e '.remaining | type == "array" and length > 0 and all(.[]; type == "string" and test("\\S"))' >/dev/null 2>&1 || invalid 'continue needs a non-empty remaining-work list'
    ;;
  complete)
    [ -s "$check_file" ] && jq -e '.only_unverified_with_reason == true' "$check_file" >/dev/null 2>&1 || invalid 'complete requires only unverified rows with reasons'
    ;;
  blocked) [ -s "$blocked_file" ] || invalid 'blocked requires a blocked record' ;;
  fix-record) ;;
  *) invalid 'unknown verdict' ;;
esac
if [ "$recorded" != true ]; then
  jq -sc --argjson verdict "$verdict" '.[-1] += ($verdict | {verdict, remaining: (.remaining // [])}) | .[-1].state = "closed" | .[]' "$ledger_file" > "$ledger_file.tmp"
  mv "$ledger_file.tmp" "$ledger_file"
fi
# The list is interpolated inside <remaining-work>; neutralize angle brackets so
# the judge's text cannot forge that block's closing tag.
remaining=$(printf '%s' "$verdict" | jq -r 'if .verdict == "continue" then [.remaining[] | "- " + (gsub("<"; "‹") | gsub(">"; "›"))] | join("\n") else "" end')
if [ "$word" = continue ]; then
  printf '%s\n' "$remaining" > "$remaining_file"
  count=$(jq -s '[.[] | select(.verdict == "continue")] | length' "$ledger_file")
  if [ "$count" -gt "$max_continue" ]; then
    items=$(printf '%s' "$verdict" | jq '.remaining | length')
    printf 'implementation incomplete: %s items remain after %s continue passes\n%s\n' "$items" "$max_continue" "$remaining" >&2
    exit 1
  fi
  if [ ! -f "$snapshot_file" ]; then
    head=$(git rev-parse HEAD)
    checksum=$("$script_dir/checklist-sha256.sh" "$checklist_file")
    jq -n --arg head "$head" --arg sum "$checksum" '{head:$head,checklist_sha256:$sum}' > "$snapshot_file.tmp"
    mv "$snapshot_file.tmp" "$snapshot_file"
  fi
fi
if [ "$word" = fix-record ]; then
  count=$(jq -s 'reverse | map(.verdict != "fix-record") | index(true) // length' "$ledger_file")
  if [ "$count" -gt "$max_fix_record" ]; then
    printf 'implementation record could not be fixed after %s fix-record verdicts\n' "$max_fix_record" >&2
    exit 1
  fi
fi
jq -cn --arg verdict "$word" --arg remaining "$remaining" '{verdict:$verdict,remaining:$remaining}'
