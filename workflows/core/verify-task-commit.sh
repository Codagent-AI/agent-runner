#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  starting_head=$(
    printf '%s' "$payload" |
      jq -er '
        if type != "object" then
          error("input must be a JSON object")
        elif (.starting_head | type) != "string" then
          error("starting_head must be a string")
        else
          .starting_head
        end
      '
  )
else
  starting_head=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"verify-task-commit: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
if not isinstance(parsed, dict):
    print("verify-task-commit: input must be a JSON object", file=sys.stderr)
    sys.exit(2)
starting_head = parsed.get("starting_head")
if not isinstance(starting_head, str):
    print("verify-task-commit: starting_head must be a string", file=sys.stderr)
    sys.exit(2)
print(starting_head, end="")
PY
  )
fi

case "$starting_head" in
  ""|*[!0-9a-f]*)
    printf 'verify-task-commit: invalid starting commit: %s\n' "$starting_head" >&2
    exit 2
    ;;
esac

if ! git rev-parse --verify "$starting_head^{commit}" >/dev/null 2>&1; then
  printf 'verify-task-commit: invalid starting commit: %s\n' "$starting_head" >&2
  exit 2
fi

current_head=$(git rev-parse --verify HEAD)

if [ "$current_head" = "$starting_head" ]; then
  echo "implementation task did not produce a commit; refusing to advance to the next task" >&2
  exit 1
fi

if ! git merge-base --is-ancestor "$starting_head" "$current_head"; then
  echo "implementation task ended at a commit that is not a descendant of its starting HEAD" >&2
  exit 1
fi

if git diff --quiet "$starting_head" "$current_head"; then
  echo "implementation task produced commits but no tracked implementation changes" >&2
  exit 1
fi

commit_count=$(git rev-list --count "$starting_head..$current_head")
printf 'implementation task produced %s commit(s)\n' "$commit_count"
