#!/bin/sh
# Completion adds record, commit, and verified-status gates to the evidence check.
set -eu
command -v jq >/dev/null 2>&1 || { echo 'jq is required to read the completion check input' >&2; exit 1; }
payload=$(cat)
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
checklist_file=$(printf '%s' "$payload" | jq -r '.checklist_file')
completion_file=$(printf '%s' "$payload" | jq -r '.completion_file')
report_dir=$(printf '%s' "$payload" | jq -r '.report_dir')
start_head=$(printf '%s' "$payload" | jq -r '.start_head')
mkdir -p "$report_dir"
problems="$report_dir/completion-problems.txt"
: > "$problems"
evidence_ok=true
# Preserve check-evidence's problem-file contract and include its diagnostics.
if ! evidence_output=$(printf '%s' "$payload" | "$script_dir/check-evidence.sh" 2>&1); then
  evidence_ok=false
  printf '%s\n' "$evidence_output" | sed 's/^/- /' >> "$problems"
fi
record_ok=true
if [ ! -s "$completion_file" ]; then
  record_ok=false
  printf -- '- completion record %s is missing or empty\n' "$completion_file" >> "$problems"
fi
commits_ok=true
if ! revision=$(git rev-parse --verify "$start_head^{commit}" 2>&1); then
  commits_ok=false
  printf 'invalid implementation start revision %s: %s\n' "$start_head" "$revision" | sed 's/^/- /' >> "$problems"
elif ! subjects=$(git log --format=%s "$revision..HEAD" 2>&1); then
  commits_ok=false
  printf 'cannot inspect implementation commits: %s\n' "$subjects" | sed 's/^/- /' >> "$problems"
elif ! printf '%s\n' "$subjects" | grep -v '^\[judge-completion\]' | grep -q .; then
  commits_ok=false
  printf -- '- no implementation commit since %s (judge-completion commits do not count)\n' "$start_head" >> "$problems"
fi
rows=$(awk -v statuses_only=1 -f "$script_dir/checklist-rows.awk" "$checklist_file" 2>/dev/null) || rows='checklist unreadable'
only_unverified=false
if [ "$evidence_ok" = true ] && [ "$record_ok" = true ] && [ "$commits_ok" = true ] && [ -n "$rows" ] && printf '%s\n' "$rows" | awk -F '\t' '$2 != "not verified" {exit 1}'; then
  only_unverified=true
fi
if [ -n "$rows" ]; then
  printf '%s\n' "$rows" | awk -F '\t' '{printf "- %s has status %s; requires verified\n",$1,$2}' >> "$problems"
fi
passed=false
[ -s "$problems" ] || passed=true
jq -n --argjson passed "$passed" --argjson only "$only_unverified" '{passed:$passed,only_unverified_with_reason:$only}' > "$report_dir/completion-check.json"
if [ "$passed" = false ]; then cat "$problems" >&2; exit 1; fi
