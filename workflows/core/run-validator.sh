#!/bin/sh
set -eu

task_file=$("$AGENT_RUNNER_EXECUTABLE" internal json-value task_file)

if [ -n "${AGENT_RUNNER_METRICS_CONSUMER:-}" ] && [ -n "${AGENT_RUNNER_METRICS_CONTEXT:-}" ]; then
  if [ -n "$task_file" ]; then
    exec agent-validator run --report --enable-review task-compliance --context-file "$task_file" --metrics-consumer "$AGENT_RUNNER_METRICS_CONSUMER" --metrics-context "$AGENT_RUNNER_METRICS_CONTEXT"
  fi
  exec agent-validator run --report --metrics-consumer "$AGENT_RUNNER_METRICS_CONSUMER" --metrics-context "$AGENT_RUNNER_METRICS_CONTEXT"
fi

if [ -n "$task_file" ]; then
  exec agent-validator run --report --enable-review task-compliance --context-file "$task_file"
fi
exec agent-validator run --report
