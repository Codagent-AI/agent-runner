#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  task_file=$(printf '%s' "$payload" | jq -er '
    if type != "object" then
      error("input must be a JSON object")
    elif (.task_file | type) != "string" then
      error("task_file must be a string")
    else
      .task_file
    end
  ')
else
  task_file=$(printf '%s' "$payload" | python3 -c '
import json
import sys

try:
    value = json.load(sys.stdin)
except json.JSONDecodeError as exc:
    print(f"run-validator: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
if not isinstance(value, dict) or not isinstance(value.get("task_file"), str):
    print("run-validator: task_file must be a string", file=sys.stderr)
    sys.exit(2)
print(value["task_file"], end="")
')
fi

if [ -n "$task_file" ]; then
  exec agent-validator run --report --enable-review task-compliance --context-file "$task_file"
fi
exec agent-validator run --report
