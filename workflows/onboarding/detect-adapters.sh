#!/bin/sh
set -eu

first=1
printf '['
for adapter in claude codex copilot cursor opencode; do
  if command -v "$adapter" >/dev/null 2>&1; then
    if [ "$first" -eq 0 ]; then
      printf ','
    fi
    first=0
    printf '"%s"' "$adapter"
  fi
done
printf ']\n'
