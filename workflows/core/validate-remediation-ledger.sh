#!/bin/sh
set -eu

payload=$(cat)

REMEDIATION_LEDGER_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys
from pathlib import Path

try:
    inputs = json.loads(os.environ["REMEDIATION_LEDGER_PAYLOAD"])
except json.JSONDecodeError as exc:
    raise SystemExit(f"validate-remediation-ledger: invalid JSON input: {exc}")

if not isinstance(inputs, dict):
    raise SystemExit("validate-remediation-ledger: input must be a JSON object")

ledger_path = inputs.get("ledger")
repositories = inputs.get("repositories")
if not isinstance(ledger_path, str) or not ledger_path:
    raise SystemExit("validate-remediation-ledger: ledger must be a non-empty string")
if not isinstance(repositories, str):
    raise SystemExit("validate-remediation-ledger: repositories must be a string")

path = Path(ledger_path)
if not path.is_file():
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text('{"workspace":[],"repositories":{}}\n', encoding="utf-8")

with path.open(encoding="utf-8") as ledger_file:
    ledger = json.load(ledger_file)
entries = ledger.get("repositories")
if not isinstance(entries, dict):
    raise SystemExit("acceptance remediation ledger must contain a repositories object")

unknown = set(entries) - set(repositories.split(","))
if unknown:
    raise SystemExit(
        "acceptance remediation ledger names unselected repositories: "
        + ", ".join(sorted(unknown))
    )
PY
