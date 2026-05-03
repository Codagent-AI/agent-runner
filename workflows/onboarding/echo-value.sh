#!/bin/bash
set -euo pipefail
jq -rj '.value' </dev/stdin
