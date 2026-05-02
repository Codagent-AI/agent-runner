#!/bin/bash
set -euo pipefail
jq -rj '.items | join(", ")' </dev/stdin
