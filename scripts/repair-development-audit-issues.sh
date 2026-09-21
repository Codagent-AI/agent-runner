#!/usr/bin/env bash
# Temporary two-machine repair tool for placeholder auto-audit GitHub issues.
set -euo pipefail

execute=false
data_root="${HOME}/.agent-runner"

usage() {
  echo "Usage: $0 [--execute] [--data-root <dir>]" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --execute) execute=true ;;
    --data-root) shift; data_root="${1:-}" ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
  shift
done

for command in go gh jq rg; do
  command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 2; }
done
[ -d "$data_root/projects" ] || { echo "data root has no projects directory: $data_root" >&2; exit 2; }

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
runner=""
tmp_dir=""
cleanup() { [ -z "$tmp_dir" ] || rm -rf "$tmp_dir"; }
trap cleanup EXIT INT TERM

if [ "$execute" = true ]; then
  tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/agent-runner-audit-issue-repair.XXXXXX")
  (cd "$repo_root" && go build -tags dev_audit -o "$tmp_dir/agent-runner" ./cmd/agent-runner)
  runner="$tmp_dir/agent-runner"
fi

actionable=0
repaired=0
while IFS= read -r report; do
  audit_dir=$(dirname "$report")
  while IFS=$'\t' read -r issue_url title; do
    [ -n "$issue_url" ] || continue
    issue=$(gh issue view "$issue_url" --repo Codagent-AI/agent-runner --json title,body,url)
    if ! jq -e '.title | startswith("[auto-audit] ")' >/dev/null <<<"$issue" || ! jq -e '.body == "-"' >/dev/null <<<"$issue"; then
      echo "SKIP not-placeholder  $(basename "$audit_dir")  $issue_url"
      continue
    fi
    actionable=$((actionable + 1))
    echo "REPAIR  $(basename "$audit_dir")  $issue_url  $title"
  done <<EOF
$(jq -r '.correctness.findings[]? | select(.publication_state == "created" and (.issue_url // "") != "") | [.issue_url, .candidate.title] | @tsv' "$report")
EOF
  [ "$execute" = true ] || continue
  count=$("$runner" audit repair-issues "$audit_dir")
  repaired=$((repaired + count))
done <<EOF
$(rg -l '"issue_url"' "$data_root/projects" 2>/dev/null | rg 'local-report\.json$' || true)
EOF

echo "Issue repair inventory: actionable=$actionable repaired=$repaired execute=$execute"
