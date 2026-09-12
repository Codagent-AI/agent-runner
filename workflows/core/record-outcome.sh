#!/bin/sh
set -eu

# See record-triage.sh for why this requires python3 rather than a jq
# fallback: the outcome shape here is conditionally assembled, not a flat
# single-field extraction.

payload=$(cat)

PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"record-outcome: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)

if not isinstance(parsed, dict):
    print("record-outcome: input must be a JSON object", file=sys.stderr)
    sys.exit(2)

outcome_path = parsed.get("outcome_path") or "/artifacts/fix-outcome.json"
validator_status = parsed.get("validator_status") or "failed"
ci_status = parsed.get("ci_status") or ""
branch_name = parsed.get("branch_name") or ""

pr_details_raw = parsed.get("pr_details") or "{}"
try:
    pr_details = json.loads(pr_details_raw)
except json.JSONDecodeError:
    pr_details = {}
if not isinstance(pr_details, dict):
    pr_details = {}

reasons_raw = parsed.get("reasons") or "[]"
try:
    reasons = json.loads(reasons_raw)
except json.JSONDecodeError:
    reasons = []
if not isinstance(reasons, list):
    reasons = []
reasons = [str(r) for r in reasons]

pr_url = pr_details.get("url") or ""


def pr_reference():
    return {
        "url": pr_url,
        "number": pr_details.get("number"),
        "branch": branch_name,
        "head_sha": pr_details.get("headRefOid") or pr_details.get("head_sha") or "",
    }


if validator_status != "passed":
    outcome = {
        "contract": "factory-fix/1",
        "outcome": "failed",
        "reasons": reasons or ["validator did not pass within its repair cycles"],
        "validator": {"status": "failed"},
    }
elif not pr_url:
    outcome = {
        "contract": "factory-fix/1",
        "outcome": "failed",
        "reasons": reasons or ["failed to push the branch or open a pull request"],
        "validator": {"status": "passed"},
    }
elif ci_status == "passed":
    outcome = {
        "contract": "factory-fix/1",
        "outcome": "pull-request",
        "pr": pr_reference(),
        "validator": {"status": "passed"},
        "ci": {"status": "passed"},
    }
else:
    outcome = {
        "contract": "factory-fix/1",
        "outcome": "failed",
        "reasons": reasons or ["CI did not pass within its fix cycle"],
        "pr": pr_reference(),
        "validator": {"status": "passed"},
        "ci": {"status": ci_status or "failed"},
    }

out_dir = os.path.dirname(outcome_path)
if out_dir:
    os.makedirs(out_dir, exist_ok=True)
with open(outcome_path, "w") as f:
    json.dump(outcome, f)
    f.write("\n")
PY
