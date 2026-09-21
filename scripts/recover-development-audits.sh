#!/usr/bin/env bash
# Temporary two-machine recovery tool for durable development-audit attempts.
set -euo pipefail

execute=false
data_root="${HOME}/.agent-runner"
timeout_seconds=3600

usage() {
  echo "Usage: $0 [--execute] [--data-root <dir>] [--timeout-seconds <seconds>]" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --execute) execute=true ;;
    --data-root) shift; data_root="${1:-}" ;;
    --timeout-seconds) shift; timeout_seconds="${1:-}" ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
  shift
done

case "$timeout_seconds" in *[!0-9]*|'') echo "timeout must be a positive integer" >&2; exit 2;; esac
if [ "$timeout_seconds" -eq 0 ]; then echo "timeout must be positive" >&2; exit 2; fi
for command in git go jq; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 2; }; done
[ -d "$data_root/projects" ] || { echo "data root has no projects directory: $data_root" >&2; exit 2; }

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
tmp_dir=""
cleanup() { [ -z "$tmp_dir" ] || rm -rf "$tmp_dir"; }
trap cleanup EXIT INT TERM

build_runner() {
  tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/agent-runner-audit-recovery.XXXXXX")
  local_root_encoded=$(printf '%s' "$repo_root" | base64 | tr -d '\n')
  local_revision=$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || true)
  local_dirty=""
  if [ -n "$(git -C "$repo_root" status --porcelain 2>/dev/null)" ]; then local_dirty=true; fi
  local_flags="-X github.com/codagent/agent-runner/internal/devaudit.BuildRootEncoded=${local_root_encoded} -X github.com/codagent/agent-runner/internal/devaudit.BuildRevision=${local_revision} -X github.com/codagent/agent-runner/internal/devaudit.BuildDirty=${local_dirty}"
  (cd "$repo_root" && go build -tags dev_audit -ldflags "$local_flags" -o "$tmp_dir/agent-runner" ./cmd/agent-runner)
  runner="$tmp_dir/agent-runner"
}

is_real_project() {
  local project_root="$1"
  [ -d "$project_root" ] && git -C "$project_root" rev-parse --is-inside-work-tree >/dev/null 2>&1 || return 1
  case "$(cd "$project_root" && pwd -P)" in /tmp/*|/private/tmp/*|*/tmp/*) return 1;; esac
}

actionable=0
failed=0
runner=""
if [ "$execute" = true ]; then build_runner; fi

for meta in "$data_root"/projects/*/meta.json; do
  [ -f "$meta" ] || continue
  project_root=$(jq -r '.path // empty' "$meta")
  is_real_project "$project_root" || continue
  project_dir=$(dirname "$meta")
  for lifecycle in "$project_dir"/runs/*/audit-lifecycle.json; do
    [ -f "$lifecycle" ] || continue
    source_dir=$(dirname "$lifecycle")
    source_name=$(basename "$source_dir")
    while IFS=$'\t' read -r audit_id session trigger state; do
      report="$project_dir/runs/$audit_id/local-report.json"
      delivery="missing"
      if [ -f "$report" ]; then delivery=$(jq -r '.delivery_state // "missing"' "$report"); fi
      [ "$delivery" = delivered ] && continue
      session_delivered=false
      while IFS=$'\t' read -r sibling_id sibling_session; do
        [ "$sibling_session" = "$session" ] || continue
        sibling_report="$project_dir/runs/$sibling_id/local-report.json"
        if [ -f "$sibling_report" ] && [ "$(jq -r '.delivery_state // empty' "$sibling_report")" = delivered ]; then
          session_delivered=true
          break
        fi
      done <<EOF
$(jq -r '.links[]? | [.audit_run_id, .execution_session_id] | @tsv' "$lifecycle")
EOF
      if [ "$session_delivered" = true ]; then
        echo "SKIP delivered-session  $source_name  $session"
        continue
      fi
      action=""
      target=""
      if [ "$delivery" = pending ]; then
        action="retry"
        target="$audit_id"
      else
      case "$state" in
        launching|started) echo "SKIP active  $source_name  $audit_id" ;;
        reserved) if [ "$trigger" = automatic ]; then action="reconcile"; target="$session"; fi ;;
        failed|completed) action="replay"; target="$session" ;;
      esac
      fi
      [ -n "$action" ] || continue
      if [ "$action" = replay ] || [ "$action" = reconcile ]; then
        metrics_file="$source_dir/run-metrics.json"
        if [ ! -f "$metrics_file" ] || ! jq -e --arg session "$target" '.sessions[]? | select(.execution_session_id == $session)' "$metrics_file" >/dev/null; then
          echo "SKIP unavailable  $source_name  $target"
          continue
        fi
      fi
      actionable=$((actionable + 1))
      echo "$(printf '%s' "$action" | tr '[:lower:]' '[:upper:]')  $source_name  $target"
      [ "$execute" = true ] || continue
      case "$action" in
        retry) if ! "$runner" audit retry "$project_dir/runs/$target"; then failed=$((failed + 1)); continue; fi ;;
        reconcile) if ! audit_id=$("$runner" audit reconcile "$source_dir" --session "$target"); then failed=$((failed + 1)); continue; fi ;;
        replay) if ! audit_id=$("$runner" audit replay "$source_dir" --session "$target"); then failed=$((failed + 1)); continue; fi ;;
      esac
      if [ "$action" = retry ]; then audit_id="$target"; fi
      report="$project_dir/runs/$audit_id/local-report.json"
      deadline=$(( $(date +%s) + timeout_seconds ))
      while [ ! -f "$report" ] || [ "$(jq -r '.delivery_state // empty' "$report" 2>/dev/null || true)" != delivered ]; do
        if [ "$(date +%s)" -ge "$deadline" ]; then echo "TIMEOUT $source_name $audit_id" >&2; failed=$((failed + 1)); break; fi
        sleep 2
      done
    done <<EOF
$(jq -r '.links[]? | [.audit_run_id, .execution_session_id, .trigger, .state] | @tsv' "$lifecycle")
EOF
  done
done

echo "Recovery inventory: actionable=$actionable failed=$failed execute=$execute"
[ "$failed" -eq 0 ]
