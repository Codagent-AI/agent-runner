#!/bin/bash
set -euo pipefail
jq -r '.items | length' </dev/stdin
