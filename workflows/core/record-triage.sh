#!/bin/sh
set -eu

# Nested JSON construction here is more involved than the flat single-field
# extraction the other core/*.sh gate scripts do with a jq/python3 fallback,
# so this script requires python3 rather than duplicating the logic in jq.

payload=$(cat)

PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"record-triage: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)

if not isinstance(parsed, dict):
    print("record-triage: input must be a JSON object", file=sys.stderr)
    sys.exit(2)

decision_raw = parsed.get("decision")
if not isinstance(decision_raw, str):
    print("record-triage: decision must be a string", file=sys.stderr)
    sys.exit(2)

try:
    decision = json.loads(decision_raw)
except json.JSONDecodeError as exc:
    print(f"record-triage: triage decision is not valid JSON: {exc}", file=sys.stderr)
    sys.exit(2)

if not isinstance(decision, dict):
    print("record-triage: triage decision must be a JSON object", file=sys.stderr)
    sys.exit(2)

fixable = decision.get("fixable")
if not isinstance(fixable, bool):
    print("record-triage: triage decision is missing a boolean 'fixable' field", file=sys.stderr)
    sys.exit(2)

reasons = decision.get("reasons") or []
if not isinstance(reasons, list):
    print("record-triage: triage decision 'reasons' must be a list", file=sys.stderr)
    sys.exit(2)
reasons = [str(r) for r in reasons]

if not fixable:
    outcome_path = parsed.get("outcome_path") or "/artifacts/fix-outcome.json"
    outcome = {
        "contract": "factory-fix/1",
        "outcome": "needs-input",
        "reasons": reasons,
        "validator": {"status": "skipped"},
    }
    out_dir = os.path.dirname(outcome_path)
    if out_dir:
        os.makedirs(out_dir, exist_ok=True)
    with open(outcome_path, "w") as f:
        json.dump(outcome, f)
        f.write("\n")

print("true" if fixable else "false")
PY
