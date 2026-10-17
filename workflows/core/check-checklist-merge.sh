#!/bin/sh
# Checks that the merged checklist kept every behavior from both checklists:
# each lead ID (L-n) and implementor ID (I-n) must appear in it, as a row or
# cited in a row that combines several. It also rejects a checklist that cites
# an acceptance (AT-n) or human-only (HT-n) test: manual testing happens once,
# in acceptance testing, and is never a checklist check.
#
# This checks that no ID was dropped, not that a combined row keeps the
# stricter check.
set -eu

payload=$(cat)

CHECKLIST_MERGE_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import re
import sys

try:
    inputs = json.loads(os.environ["CHECKLIST_MERGE_PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"checklist merge check: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
keys = ("lead_checklist", "implementor_checklist_file", "checklist_file")
if not isinstance(inputs, dict) or not all(isinstance(inputs.get(k), str) and inputs.get(k) for k in keys):
    print("checklist merge check: lead_checklist, implementor_checklist_file, and checklist_file must be non-empty strings", file=sys.stderr)
    sys.exit(2)
lead_text = inputs["lead_checklist"]


def read(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            return f.read()
    except OSError as exc:
        print(f"checklist merge check: cannot read {path}: {exc.strerror}", file=sys.stderr)
        sys.exit(1)


implementor_text = read(inputs["implementor_checklist_file"])
merged_path = inputs["checklist_file"]
merged_text = read(merged_path)


def ids(prefix, text):
    # Whole IDs only: L-1 must not match inside L-10 or XL-1.
    found = re.findall(r"(?<![A-Za-z0-9-])" + prefix + r"-(\d+)(?!\d)", text)
    return sorted({int(n) for n in found})


problems = []
lead_ids = ids("L", lead_text)
implementor_ids = ids("I", implementor_text)
if not lead_ids:
    problems.append("the lead's checklist has no L- IDs")
if not implementor_ids:
    problems.append("the implementor's checklist has no I- IDs")
for prefix, numbers, owner in (("L", lead_ids, "lead's"), ("I", implementor_ids, "implementor's")):
    present = set(ids(prefix, merged_text))
    missing = [f"{prefix}-{n}" for n in numbers if n not in present]
    if missing:
        problems.append(f"missing from the {owner} checklist: " + ", ".join(missing))
manual = sorted(set(re.findall(r"(?<![A-Za-z0-9-])((?:AT|HT)-\d+)(?!\d)", merged_text)))
if manual:
    problems.append("cites manual tests, which are not checklist checks: " + ", ".join(manual))

if problems:
    print(f"checklist merge check: {merged_path} is not a complete merge:", file=sys.stderr)
    for p in problems:
        print(f"- {p}", file=sys.stderr)
    print("Keep each ID as its own row or cite it in the row that covers it. Verify each behavior with an automated test, a scripted command, or inspection, never an acceptance or human-only test.", file=sys.stderr)
    sys.exit(1)

print(f"checklist merge check: kept {len(lead_ids)} lead and {len(implementor_ids)} implementor IDs")
PY
