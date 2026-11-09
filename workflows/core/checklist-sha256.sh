#!/bin/sh
# Print a checked digest for both completion snapshots and stall comparisons.
set -eu
if command -v shasum >/dev/null 2>&1; then
  output=$(shasum -a 256 < "$1") || { echo 'cannot compute checklist SHA-256' >&2; exit 1; }
elif command -v sha256sum >/dev/null 2>&1; then
  output=$(sha256sum < "$1") || { echo 'cannot compute checklist SHA-256' >&2; exit 1; }
else
  echo 'shasum or sha256sum is required' >&2
  exit 1
fi
checksum=${output%% *}
case "$checksum" in
  ''|*[!0-9a-fA-F]*) echo 'empty or invalid SHA-256 from checklist hash tool' >&2; exit 1 ;;
esac
if [ "${#checksum}" -ne 64 ]; then
  echo 'empty or invalid SHA-256 from checklist hash tool' >&2
  exit 1
fi
printf '%s\n' "$checksum"
