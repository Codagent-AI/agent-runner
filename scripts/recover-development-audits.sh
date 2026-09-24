#!/usr/bin/env bash
# Temporary two-machine recovery tool for durable development-audit attempts.
set -euo pipefail

execute=false
data_root="${HOME}/.agent-runner"
timeout_seconds=3600
include_temp_projects=false
explicit_sessions=()

usage() {
  echo "Usage: $0 [--execute] [--data-root <dir>] [--timeout-seconds <seconds>] [--include-temp-projects] [--session <session-dir>:<execution-session-id>[:<project-dir>]]..." >&2
  echo "  --session recovers only the named sessions, including runs started with --session-dir (for example Agent Factory attempts), instead of scanning the data root." >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --execute) execute=true ;;
    --data-root) shift; data_root="${1:-}" ;;
    --timeout-seconds) shift; timeout_seconds="${1:-}" ;;
    --include-temp-projects) include_temp_projects=true ;;
    --session) shift; explicit_sessions+=("${1:-}") ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
  shift
done

case "$timeout_seconds" in *[!0-9]*|'') echo "timeout must be a positive integer" >&2; exit 2;; esac
if [ "$timeout_seconds" -eq 0 ]; then echo "timeout must be positive" >&2; exit 2; fi
for command in git go jq; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 2; }; done
if [ "${#explicit_sessions[@]}" -eq 0 ]; then
  [ -d "$data_root/projects" ] || { echo "data root has no projects directory: $data_root" >&2; exit 2; }
fi

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
  # Projects under temp directories are normally throwaway test checkouts.
  [ "$include_temp_projects" = true ] && return 0
  case "$(cd "$project_root" && pwd -P)" in /tmp/*|/private/tmp/*|*/tmp/*) return 1;; esac
}

actionable=0
failed=0
runner=""
if [ "$execute" = true ]; then build_runner; fi

delivery_state() {
  local report="$1/local-report.json"
  if [ -f "$report" ]; then jq -r '.delivery_state // "missing"' "$report" 2>/dev/null || echo missing; else echo missing; fi
}

# wait_for_delivery <audit-dir> <label>: poll one audit until its report is
# delivered, the audit run finishes without delivering, or the timeout passes.
wait_for_delivery() {
  local audit_dir="$1" label="$2" state_file="$1/state.json" deadline reason
  deadline=$(( $(date +%s) + timeout_seconds ))
  while [ "$(delivery_state "$audit_dir")" != delivered ]; do
    # An audit run that already finished never delivers later, so report it
    # instead of waiting out the whole timeout. The recheck covers a report
    # written just after the run recorded completion.
    if [ -f "$state_file" ] && [ "$(jq -r '.completed // false' "$state_file" 2>/dev/null || true)" = true ]; then
      sleep 2
      if [ "$(delivery_state "$audit_dir")" = delivered ]; then break; fi
      if [ "$(delivery_state "$audit_dir")" = pending ] && "$runner" audit retry "$audit_dir" \
        && [ "$(delivery_state "$audit_dir")" = delivered ]; then break; fi
      reason=$(jq -r '.failureReason // "audit finished without delivering"' "$state_file" 2>/dev/null | tr '\n' ' ' | cut -c1-200)
      echo "FAILED  $label  $(basename "$audit_dir")  ${reason:-audit finished without delivering}" >&2
      failed=$((failed + 1))
      return
    fi
    if [ "$(date +%s)" -ge "$deadline" ]; then echo "TIMEOUT $label $(basename "$audit_dir")" >&2; failed=$((failed + 1)); return; fi
    sleep 2
  done
  echo "DELIVERED  $label  $(basename "$audit_dir")"
}

# audit_live <audit-dir>: the audit run's lock names a live process.
audit_live() {
  local lock="$1/lock" pid
  [ -f "$lock" ] || return 1
  pid=$(tr -d '[:space:]' < "$lock")
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$pid" 2>/dev/null
}

# recover_session <session-dir> <execution-session-id> <project-dir>: recover
# one explicitly named session whose runs directory may not be recorded.
recover_session() {
  local source_dir="$1" session="$2" project="$3" lifecycle="$1/audit-lifecycle.json" label audit_id pending=""
  label=$(basename "$(dirname "$source_dir")")/$(basename "$source_dir")
  if [ ! -f "$source_dir/run-metrics.json" ] || ! jq -e --arg session "$session" '.sessions[]? | select(.execution_session_id == $session)' "$source_dir/run-metrics.json" >/dev/null; then
    echo "SKIP unavailable  $label  $session"
    return
  fi
  if [ -f "$lifecycle" ]; then
    while IFS=$'\t' read -r linked_id linked_session linked_state; do
      [ "$linked_session" = "$session" ] || continue
      case "$(delivery_state "$(dirname "$source_dir")/$linked_id")" in
        delivered) echo "SKIP delivered-session  $label  $session"; return ;;
        pending) pending="$linked_id" ;;
        *)
          # A running audit may still deliver; a second replay would duplicate its
          # rows. An audit whose process is gone is recovered like any other.
          case "$linked_state" in
            launching|started)
              if audit_live "$(dirname "$source_dir")/$linked_id"; then
                echo "SKIP active  $label  $linked_id"
                return
              fi
              ;;
          esac
          ;;
      esac
    done <<EOF_LINKS
$(jq -r '.links[]? | [.audit_run_id, .execution_session_id, .state] | @tsv' "$lifecycle")
EOF_LINKS
  fi
  actionable=$((actionable + 1))
  if [ -n "$pending" ]; then
    echo "RETRY  $label  $pending"
    [ "$execute" = true ] || return 0
    if ! "$runner" audit retry "$(dirname "$source_dir")/$pending"; then failed=$((failed + 1)); return; fi
    audit_id="$pending"
  else
    echo "REPLAY  $label  $session"
    [ "$execute" = true ] || return 0
    local project_args=()
    if [ -n "$project" ]; then project_args=(--project "$project"); fi
    if ! audit_id=$("$runner" audit replay "$source_dir" --session "$session" ${project_args[@]+"${project_args[@]}"}); then failed=$((failed + 1)); return; fi
  fi
  wait_for_delivery "$(dirname "$source_dir")/$audit_id" "$label"
}

if [ "${#explicit_sessions[@]}" -gt 0 ]; then
  for spec in "${explicit_sessions[@]}"; do
    IFS=: read -r spec_dir spec_session spec_project <<<"$spec"
    if [ -z "$spec_dir" ] || [ -z "$spec_session" ] || [ ! -d "$spec_dir" ]; then
      echo "invalid --session value: $spec" >&2
      exit 2
    fi
    recover_session "$(cd "$spec_dir" && pwd -P)" "$spec_session" "$spec_project"
  done
  echo "Recovery inventory: actionable=$actionable failed=$failed execute=$execute"
  [ "$failed" -eq 0 ]
  exit
fi

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
      wait_for_delivery "$project_dir/runs/$audit_id" "$source_name"
    done <<EOF
$(jq -r '.links[]? | [.audit_run_id, .execution_session_id, .trigger, .state] | @tsv' "$lifecycle")
EOF
  done
done

echo "Recovery inventory: actionable=$actionable failed=$failed execute=$execute"
[ "$failed" -eq 0 ]
