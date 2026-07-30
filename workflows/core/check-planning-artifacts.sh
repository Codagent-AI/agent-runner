#!/bin/sh
set -eu

payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  change_dir=$(printf '%s' "$payload" | jq -er '.change_dir | select(type == "string")')
  required_files=$(printf '%s' "$payload" | jq -er '.required_files | select(type == "string")')
  require_specs=$(printf '%s' "$payload" | jq -er '.require_specs | select(type == "string")')
else
  parsed=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"check-planning-artifacts: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
if not isinstance(parsed, dict):
    print("check-planning-artifacts: input must be a JSON object", file=sys.stderr)
    sys.exit(2)
values = [parsed.get(key) for key in ("change_dir", "required_files", "require_specs")]
if not all(isinstance(value, str) for value in values):
    print(
        "check-planning-artifacts: change_dir, required_files, and require_specs must be strings",
        file=sys.stderr,
    )
    sys.exit(2)
print(*values, sep="\n")
PY
)
  change_dir=$(printf '%s\n' "$parsed" | sed -n '1p')
  required_files=$(printf '%s\n' "$parsed" | sed -n '2p')
  require_specs=$(printf '%s\n' "$parsed" | sed -n '3p')
fi

if [ -z "$change_dir" ]; then
  printf 'check-planning-artifacts: change_dir must not be empty\n' >&2
  exit 1
fi

case "$require_specs" in
  true|false)
    ;;
  *)
    printf 'check-planning-artifacts: require_specs must be true or false: %s\n' "$require_specs" >&2
    exit 1
    ;;
esac

if [ -z "$required_files" ]; then
  printf 'check-planning-artifacts: required_files must not be empty\n' >&2
  exit 1
fi

case "$required_files" in
  *[!A-Za-z0-9_.,/-]*)
    printf 'check-planning-artifacts: required_files contains unsupported characters: %s\n' "$required_files" >&2
    exit 1
    ;;
esac

missing=""
old_ifs=$IFS
IFS=','
for file in $required_files; do
  case "$file" in
    ""|/*|..|../*|*/../*|*/..)
      printf 'check-planning-artifacts: required file must be a confined relative path: %s\n' "$file" >&2
      exit 1
      ;;
  esac
  if [ ! -s "$change_dir/$file" ]; then
    missing="$missing $file"
  fi
done
IFS=$old_ifs

if [ -n "$missing" ]; then
  printf 'planning did not produce required artifacts in %s:%s\n' "$change_dir" "$missing" >&2
  exit 1
fi

if [ "$require_specs" = "true" ]; then
  if [ ! -d "$change_dir/specs" ] ||
    ! find "$change_dir/specs" -type f -name spec.md -size +0c -print -quit | grep -q .; then
    printf 'planning did not produce a non-empty specification under %s/specs/\n' "$change_dir" >&2
    exit 1
  fi
fi
