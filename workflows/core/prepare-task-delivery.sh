#!/bin/sh
set -eu

# Input: {"session_dir": "<abs path>", "task_file": "<path>", "starting_head": "<sha>"}.
# Prints {"record_path": "...", "started_at": "..."} naming the external
# delivery record for this task execution, and removes any stale record there.
payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  if ! inputs=$(
    printf '%s' "$payload" |
      jq -r '
        def ctl: explode | any(. < 32 or . == 127);
        if type != "object" then
          error("input must be a JSON object")
        elif [.session_dir, .task_file, .starting_head] | any(type != "string") then
          error("session_dir, task_file, and starting_head must be strings")
        else
          # A captured HEAD keeps its trailing newline.
          .starting_head |= sub("\n+$"; "") |
          if [.session_dir, .task_file, .starting_head] | any(ctl) then
            error("inputs must not contain control characters")
          else
            .session_dir, .task_file, .starting_head
          end
        end
      '
  ); then
    echo "prepare-task-delivery: invalid input" >&2
    exit 2
  fi
else
  inputs=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

def fail(message):
    print(f"prepare-task-delivery: {message}", file=sys.stderr)
    sys.exit(2)

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    fail(f"invalid JSON input: {exc}")
if not isinstance(parsed, dict):
    fail("input must be a JSON object")
values = [parsed.get(key) for key in ("session_dir", "task_file", "starting_head")]
if not all(isinstance(value, str) for value in values):
    fail("session_dir, task_file, and starting_head must be strings")
# A captured HEAD keeps its trailing newline.
values[2] = values[2].rstrip("\n")
if any(ord(ch) < 32 or ord(ch) == 127 for value in values for ch in value):
    fail("inputs must not contain control characters")
for value in values:
    print(value)
PY
  ) || exit 2
fi

session_dir=$(printf '%s\n' "$inputs" | sed -n 1p)
task_file=$(printf '%s\n' "$inputs" | sed -n 2p)
starting_head=$(printf '%s\n' "$inputs" | sed -n 3p)

case "$session_dir" in
  /*) ;;
  *)
    printf 'prepare-task-delivery: session_dir must be absolute: %s\n' "$session_dir" >&2
    exit 2
    ;;
esac
if [ -z "$task_file" ]; then
  echo "prepare-task-delivery: task_file must not be empty" >&2
  exit 2
fi
case "$starting_head" in
  ""|*[!0-9a-f]*)
    printf 'prepare-task-delivery: invalid starting commit: %s\n' "$starting_head" >&2
    exit 2
    ;;
esac

started_at=$(date -u +%s)
task_key=$(basename "$task_file" .md | LC_ALL=C sed 's/[^A-Za-z0-9._-]/_/g')
head12=$(printf '%.12s' "$starting_head")
record_dir="$session_dir/output/task-delivery"
record_path="$record_dir/${task_key}-${started_at}-${head12}.json"

mkdir -p "$record_dir"
rm -f -- "$record_path"

if command -v jq >/dev/null 2>&1; then
  jq -cn --arg record_path "$record_path" --arg started_at "$started_at" \
    '{record_path: $record_path, started_at: $started_at}'
else
  RECORD_PATH="$record_path" STARTED_AT="$started_at" python3 -c '
import json
import os
print(json.dumps({"record_path": os.environ["RECORD_PATH"], "started_at": os.environ["STARTED_AT"]}))
'
fi
