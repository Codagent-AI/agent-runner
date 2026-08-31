#!/bin/sh
set -eu
payload=$(cat)
parsed=$(TASK_GROUP_PAYLOAD="$payload" python3 - <<'PY'
import json, os, sys
try: value = json.loads(os.environ["TASK_GROUP_PAYLOAD"])
except json.JSONDecodeError as exc: print(f"resolve-task-group: invalid JSON input: {exc}", file=sys.stderr); sys.exit(2)
for key in ("workspace_dir", "change_dir", "plan_kind", "output"):
    if not isinstance(value, dict) or not isinstance(value.get(key), str) or not value[key]: print(f"resolve-task-group: {key} must be a non-empty string", file=sys.stderr); sys.exit(2)
repository = value.get("repository_name", "")
if not isinstance(repository, str): print("resolve-task-group: repository_name must be a string", file=sys.stderr); sys.exit(2)
print(value["workspace_dir"], value["change_dir"], value["plan_kind"], value["output"], repository, sep="\n")
PY
)
workspace_dir=$(printf '%s\n' "$parsed" | sed -n '1p'); change_dir=$(printf '%s\n' "$parsed" | sed -n '2p'); plan_kind=$(printf '%s\n' "$parsed" | sed -n '3p'); output=$(printf '%s\n' "$parsed" | sed -n '4p'); repository_name=$(printf '%s\n' "$parsed" | sed -n '5p')
case "$plan_kind" in full|simple) ;; *) echo "resolve-task-group: invalid plan_kind: $plan_kind" >&2; exit 2 ;; esac
case "$output" in repositories|task-pattern) ;; *) echo "resolve-task-group: invalid output: $output" >&2; exit 2 ;; esac
if [ "$output" = repositories ]; then exec "${AGENT_RUNNER_EXECUTABLE:-agent-runner}" internal task-groups --workspace-dir "$workspace_dir" --change-dir "$change_dir" --plan-kind "$plan_kind" --output repositories; fi
test -n "$repository_name" || { echo "resolve-task-group: repository_name is required for task-pattern output" >&2; exit 2; }
exec "${AGENT_RUNNER_EXECUTABLE:-agent-runner}" internal task-groups --workspace-dir "$workspace_dir" --change-dir "$change_dir" --plan-kind "$plan_kind" --repository "$repository_name" --output task-pattern
