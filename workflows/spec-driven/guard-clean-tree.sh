#!/bin/sh
# Refuses to start planning while Agent Validator reports unvalidated changes,
# when the run's validation workflow runs Agent Validator.
# agent-validator detect: exit 0 = unvalidated changes present, exit 2 = clean.
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
. "$script_dir/validator-bookkeeping.sh"

if [ "$(validator_bookkeeping "$(payload_validation_workflow "$payload")")" != run ]; then
  exit 0
fi

set +e
agent-validator detect
status=$?
set -e

if [ "$status" -eq 0 ]; then
  echo 'Unvalidated changes detected. Run agent-validator before planning.' >&2
  exit 1
elif [ "$status" -ne 2 ]; then
  exit "$status"
fi
