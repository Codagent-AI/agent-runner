#!/bin/bash
set -euo pipefail
jq -rj '.items | length' </dev/stdin
