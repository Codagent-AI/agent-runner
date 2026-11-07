#!/bin/sh
# Check whole-pass progress and open one durable judgment, also on resume.
set -eu
command -v jq >/dev/null 2>&1 || { echo 'jq is required to read the completion progress input' >&2; exit 1; }
payload=$(cat)
checklist_file=$(printf '%s' "$payload" | jq -r '.checklist_file')
snapshot_file=$(printf '%s' "$payload" | jq -r '.snapshot_file')
ledger_file=$(printf '%s' "$payload" | jq -r '.ledger_file')
verdict_file=$(printf '%s' "$payload" | jq -r '.verdict_file')
if [ -f "$snapshot_file" ]; then
  head=$(git rev-parse HEAD)
  checksum=$(shasum -a 256 "$checklist_file" | cut -d ' ' -f 1)
  if jq -e --arg head "$head" --arg sum "$checksum" '.head == $head and .checklist_sha256 == $sum' "$snapshot_file" >/dev/null; then
    n=$(jq -s '[.[] | select(.verdict == "continue")] | length' "$ledger_file")
    printf 'implementation stalled: continue pass %s added no commit and changed no checklist row\n' "$n" >&2
    exit 1
  fi
  rm -f "$snapshot_file"
fi
rm -f "$verdict_file"
touch "$ledger_file"
if ! jq -se 'length > 0 and (last.state == "open") and (last | has("verdict") | not)' "$ledger_file" >/dev/null; then
  n=$(jq -s 'length + 1' "$ledger_file")
  jq -cn --argjson n "$n" '{judgment:$n,state:"open"}' >> "$ledger_file"
fi
