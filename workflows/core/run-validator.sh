#!/bin/sh
set -eu

validator=${AGENT_RUNNER_VALIDATOR_EXECUTABLE:-agent-validator}

task_file=$("$AGENT_RUNNER_EXECUTABLE" internal json-value task_file)

if [ -n "${AGENT_RUNNER_METRICS_CONSUMER:-}" ] && [ -n "${AGENT_RUNNER_METRICS_CONTEXT:-}" ]; then
  if [ -n "$task_file" ]; then
    exec "$validator" run --report --enable-review task-compliance --context-file "$task_file" --metrics-consumer "$AGENT_RUNNER_METRICS_CONSUMER" --metrics-context "$AGENT_RUNNER_METRICS_CONTEXT"
  fi
  exec "$validator" run --report --metrics-consumer "$AGENT_RUNNER_METRICS_CONSUMER" --metrics-context "$AGENT_RUNNER_METRICS_CONTEXT"
fi

if [ -n "$task_file" ]; then
  exec "$validator" run --report --enable-review task-compliance --context-file "$task_file"
fi
exec "$validator" run --report
