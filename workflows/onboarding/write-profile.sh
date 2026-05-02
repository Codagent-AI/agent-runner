#!/bin/sh
set -eu

payload=$(cat)
printf '%s' "$payload" | agent-runner internal write-profile
