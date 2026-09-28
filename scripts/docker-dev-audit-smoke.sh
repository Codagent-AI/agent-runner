#!/usr/bin/env bash
# Runs the product-owned detached-audit smoke in the supported Docker sandbox.
#
# The smoke releases only what it owns. Without IMAGE it builds a run-unique
# agent-runner-dev-audit-smoke:<run-id> tag and removes it (unforced) on exit.
# Without ARTIFACT_DIR it creates a temporary directory, removes it after
# success, and keeps it after failure or interrupt. Caller-supplied IMAGE and
# ARTIFACT_DIR are never removed. Must stay compatible with bash 3.2.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
TIMEOUT_SECONDS="${AUDIT_SMOKE_TIMEOUT_SECONDS:-45}"

run_id="$(date -u +%Y%m%d%H%M%S)-$$-$RANDOM"
container="agent-runner-dev-audit-smoke-$run_id"
owns_image=0
owns_artifacts=0
cleaned_up=0
child=""
launching=0
pending_signal=""

if [[ -z "${IMAGE:-}" ]]; then
  IMAGE="agent-runner-dev-audit-smoke:$run_id"
  owns_image=1
fi
if [[ -z "${ARTIFACT_DIR:-}" ]]; then
  ARTIFACT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/agent-runner-dev-audit-smoke.XXXXXX")"
  owns_artifacts=1
fi

cleanup() {
  final_status="${final_status:-$?}"
  set +e
  if [[ "$cleaned_up" == 1 ]]; then
    exit "$final_status"
  fi
  cleaned_up=1

  if [[ "$owns_image" == 1 ]]; then
    local inspect_output rm_output
    if inspect_output="$(docker image inspect "$IMAGE" 2>&1 >/dev/null)"; then
      if ! rm_output="$(docker image rm "$IMAGE" 2>&1)"; then
        echo "smoke: could not remove image $IMAGE: $rm_output" >&2
      fi
    elif [[ "$inspect_output" != *"No such image"* ]]; then
      # Only a never-built image stays silent; anything else may leave it behind.
      echo "smoke: could not inspect image $IMAGE: $inspect_output" >&2
    fi
  fi

  if [[ "$final_status" != 0 ]]; then
    echo "smoke: evidence retained in $ARTIFACT_DIR" >&2
  elif [[ "$owns_artifacts" == 1 ]]; then
    # Files written under confinement may have restrictive modes; restore
    # directory search (X) too so rm can descend into read-only directories.
    chmod -R u+rwX -- "$ARTIFACT_DIR" 2>/dev/null
    rm -rf -- "$ARTIFACT_DIR"
    if [[ -e "$ARTIFACT_DIR" ]]; then
      echo "smoke: could not remove artifact directory $ARTIFACT_DIR" >&2
    else
      echo "smoke: removed artifact directory $ARTIFACT_DIR" >&2
    fi
  fi
  exit "$final_status"
}

on_signal() {
  if [[ -z "$child" && "$launching" == 1 ]]; then
    # The runner is starting but its PID is not known yet; handle the signal
    # once it is, so shutdown can stop the runner instead of orphaning it.
    pending_signal="$1"
    return
  fi
  final_status="$1"
  set +e
  if [[ -n "$child" ]]; then
    pkill -TERM -P "$child" 2>/dev/null
    kill -TERM "$child" 2>/dev/null
  fi
  docker rm -f "$container" >/dev/null 2>&1
  if [[ -n "$child" ]]; then
    local waited=0
    while kill -0 "$child" 2>/dev/null && ((waited < 150)); do
      sleep 0.2
      waited=$((waited + 1))
    done
    if kill -0 "$child" 2>/dev/null; then
      pkill -KILL -P "$child" 2>/dev/null
      kill -KILL "$child" 2>/dev/null
      sleep 0.2
    fi
    # Reap only a child that has exited, so a stuck one cannot hang shutdown.
    if ! kill -0 "$child" 2>/dev/null; then
      wait "$child" 2>/dev/null || true
    fi
  fi
  # A container created while shutting down would otherwise pin the image.
  docker rm -f "$container" >/dev/null 2>&1
  exit "$final_status"
}

trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

mkdir -p "$ARTIFACT_DIR"
export AUDIT_SMOKE_TIMEOUT_SECONDS="$TIMEOUT_SECONDS"
launching=1
ARTIFACT_DIR="$ARTIFACT_DIR" "$RUNNER_ROOT/scripts/sandbox-run.sh" \
  --dev-audit \
  --dev-audit-smoke \
  --no-default-secrets \
  --image "$IMAGE" \
  --env AUDIT_SMOKE_TIMEOUT_SECONDS \
  --artifact-dir "$ARTIFACT_DIR" \
  --docker-run-arg "--network=none" \
  --docker-run-arg "--name=$container" \
  -- "bash /agent-runner-source/scripts/docker-dev-audit-smoke-container.sh" &
child=$!
if [[ -n "$pending_signal" ]]; then
  on_signal "$pending_signal"
fi

status=0
wait "$child" || status=$?
final_status="$status"
exit "$status"
