#!/bin/sh
set -eu

payload=$(cat)
target_path=$(printf '%s' "$payload" | sed -n 's/.*"target_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "$target_path" ] || [ ! -f "$target_path" ]; then
  printf '[]\n'
  exit 0
fi

first=1
printf '['
for name in interactive_base headless_base planner implementor; do
  if sed -n '/^[[:space:]]*agents:[[:space:]]*$/,/^[^[:space:]]/p' "$target_path" 2>/dev/null | grep -Eq "^[[:space:]]+${name}:[[:space:]]*$"; then
    if [ "$first" -eq 0 ]; then
      printf ','
    fi
    first=0
    printf '"%s"' "$name"
  fi
done
printf ']\n'
exit 0
