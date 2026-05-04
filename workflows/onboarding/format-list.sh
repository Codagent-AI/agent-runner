#!/bin/sh
set -eu

: "${AGENT_RUNNER_BIN:=agent-runner}"
"$AGENT_RUNNER_BIN" internal json-list-join items
