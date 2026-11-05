#!/bin/sh
# Advances the Agent Validator baseline only when the run's validation workflow
# runs Agent Validator.
set -eu

payload=$(cat)
script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
. "$script_dir/validator-bookkeeping.sh"

if [ "$(validator_bookkeeping "$(payload_validation_workflow "$payload")")" = run ]; then
  agent-validator skip
fi
