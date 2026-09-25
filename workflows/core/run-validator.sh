#!/bin/sh
set -eu

validator=${AGENT_RUNNER_VALIDATOR_EXECUTABLE:-agent-validator}

IFS= read -r payload || :
task_file=$(printf '%s' "$payload" | "$AGENT_RUNNER_EXECUTABLE" internal json-value task_file)
result_file=$(printf '%s' "$payload" | "$AGENT_RUNNER_EXECUTABLE" internal json-value result_file)

set -- run --report
if [ -n "$task_file" ]; then
  set -- "$@" --enable-review task-compliance --context-file "$task_file"
fi

if [ -n "${AGENT_RUNNER_METRICS_CONSUMER:-}" ] && [ -n "${AGENT_RUNNER_METRICS_CONTEXT:-}" ]; then
  set -- "$@" --metrics-consumer "$AGENT_RUNNER_METRICS_CONSUMER" --metrics-context "$AGENT_RUNNER_METRICS_CONTEXT"
fi

if [ -z "$result_file" ]; then
  exec "$validator" "$@"
fi

stdout_file=$(mktemp)
stderr_file=$(mktemp)
trap 'rm -f "$stdout_file" "$stderr_file"' EXIT
if "$validator" "$@" >"$stdout_file" 2>"$stderr_file"; then
  printf 'PASS\n' >"$result_file"
  cat "$stdout_file"
  cat "$stderr_file" >&2
else
  result=$?
  {
    printf 'FAIL\n'
    cat "$stdout_file" "$stderr_file"
  } >"$result_file"
  cat "$stdout_file"
  cat "$stderr_file" >&2
  exit "$result"
fi
