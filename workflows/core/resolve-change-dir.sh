#!/bin/sh
set -eu

payload=$(cat)
parsed=$(CHANGE_DIR_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    value = json.loads(os.environ["CHANGE_DIR_PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"resolve-change-dir: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
if not isinstance(value, dict) or not all(isinstance(value.get(k), str) and value[k] for k in ("workspace_dir", "change_dir")):
    print("resolve-change-dir: workspace_dir and change_dir must be non-empty strings", file=sys.stderr)
    sys.exit(2)
print(value["workspace_dir"])
print(value["change_dir"])
PY
)
workspace_dir=$(printf '%s\n' "$parsed" | sed -n '1p')
change_dir=$(printf '%s\n' "$parsed" | sed -n '2p')

case "$change_dir" in
  /*) candidate=$change_dir ;;
  *) candidate=$workspace_dir/$change_dir ;;
esac

if [ ! -d "$candidate" ]; then
  echo "resolve-change-dir: change directory does not exist: $candidate" >&2
  exit 1
fi
CDPATH= cd -- "$candidate" && pwd -P
