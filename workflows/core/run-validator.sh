#!/bin/sh
set -eu

task_file=$("$AGENT_RUNNER_EXECUTABLE" internal json-value task_file)

if [ -n "$task_file" ]; then
  exec agent-validator run --report --enable-review task-compliance --context-file "$task_file"
fi
exec agent-validator run --report
