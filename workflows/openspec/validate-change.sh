#!/bin/sh
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
change_name=$(printf '%s' "$payload" | "$script_dir/validate-change-name.sh")

spec_root=$(PAYLOAD="$payload" python3 -c 'import json,os; print(json.loads(os.environ["PAYLOAD"]).get("spec_root", ""))')
if [ -n "$spec_root" ]; then cd "$spec_root"; fi

openspec validate --type change "$change_name"
