#!/bin/sh
# Prints the input text with < and > replaced by visually similar characters,
# so untrusted agent output interpolated into a prompt cannot forge the closing
# tag of the block that wraps it. Matches escapeEvidenceAngles in
# internal/exec/repair.go.
set -eu
command -v jq >/dev/null 2>&1 || { echo 'jq is required to read the untrusted text input' >&2; exit 1; }
jq -j '.text // "" | gsub("<"; "‹") | gsub(">"; "›")'
