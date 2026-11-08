#!/bin/sh
# Fails, printing the record, when an agent wrote the blocked record because
# only the user can unblock the work. Completion-loop callers may provide
# resume_hint and snapshot_file to support resuming after the cause is resolved.
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  blocked_file=$(printf '%s' "$payload" | jq -r '.blocked_file // ""')
  resume_hint=$(printf '%s' "$payload" | jq -r '.resume_hint // ""')
  snapshot_file=$(printf '%s' "$payload" | jq -r '.snapshot_file // ""')
else
  blocked_file=$(printf '%s\n' "$payload" | sed -n 's/.*"blocked_file"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  resume_hint=$(printf '%s\n' "$payload" | sed -n 's/.*"resume_hint"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  snapshot_file=$(printf '%s\n' "$payload" | sed -n 's/.*"snapshot_file"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

if [ -z "$blocked_file" ]; then
  echo "blocked_file is required" >&2
  exit 1
fi

if [ -e "$blocked_file" ]; then
  # An empty record still stops the run, but only a non-empty one is a real
  # block; keep the snapshot otherwise so a resumed run can detect the stall.
  if [ -n "$snapshot_file" ] && [ -s "$blocked_file" ]; then
    rm -f "$snapshot_file"
  fi
  if [ -n "$resume_hint" ]; then
    printf 'implementation blocked: see %s\n' "$blocked_file" >&2
    cat "$blocked_file" >&2
    printf '\nresolve the cause, remove %s, and run --resume\n' "$blocked_file" >&2
    exit 1
  fi
  echo "the change cannot be implemented compliantly without a user decision:" >&2
  cat "$blocked_file" >&2
  echo "after deciding, update the definition and start a new run; resuming this run would not redo the stopped work" >&2
  exit 1
fi
