#!/bin/sh
# Checks that the lead decision record accounts for the run's calls to the
# lead session, as audit.log records them. With no calls, it writes the
# no-escalation statement into a missing or empty record. With calls, it fails
# when the record is empty, still claims no escalation, has fewer "## " ruling
# sections than answered calls, or does not name by call id a call that ended
# without a successful response.
set -eu

payload=$(cat)

LEAD_DECISIONS_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import re
import sys

NO_ESCALATION = "No questions were escalated to the lead; no lead decisions are recorded."
MAX_QUESTION = 2000

try:
    inputs = json.loads(os.environ["LEAD_DECISIONS_PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"check-lead-decisions: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)
keys = ("audit_log", "record_file", "session")
if not isinstance(inputs, dict) or not all(isinstance(inputs.get(k), str) and inputs.get(k) for k in keys):
    print("check-lead-decisions: audit_log, record_file, and session must be non-empty strings", file=sys.stderr)
    sys.exit(2)
audit_log, record_file, session = (inputs[k] for k in keys)

# An audit line is "<timestamp> [<prefix>] <type> <json>"; prefixes never
# contain "]", and the call id is the prefix's last segment.
line_pattern = re.compile(r"^\S+ \[([^\]]*)\] (agent_call_start|agent_call_end) (\{.*\})$")
calls = {}
try:
    with open(audit_log, encoding="utf-8", errors="replace") as log:
        for line in log:
            match = line_pattern.match(line.rstrip("\n"))
            if not match:
                continue
            try:
                data = json.loads(match.group(3))
            except json.JSONDecodeError:
                continue
            call_id = data.get("call_id")
            if not isinstance(call_id, str) or not call_id:
                continue
            if match.group(2) == "agent_call_start":
                if data.get("target_kind") == "session" and data.get("target_name") == session:
                    segments = [s.strip() for s in match.group(1).split(",")]
                    calls[call_id] = {
                        "caller": ", ".join(segments[:-1]),
                        "prompt": data.get("prompt") or "",
                        "outcome": None,
                    }
            elif call_id in calls:
                calls[call_id]["outcome"] = data.get("outcome") or "unknown"
except OSError as exc:
    # The lead cannot rebuild a missing log, so tell the repair to stop.
    print(f"check-lead-decisions: cannot read audit log {audit_log}: {exc}; only a human can resolve this (REPAIR_BLOCKED)", file=sys.stderr)
    sys.exit(1)

try:
    with open(record_file, encoding="utf-8", errors="replace") as handle:
        record = handle.read()
except FileNotFoundError:
    record = ""

if not calls:
    if record.strip():
        print(f"no calls to {session}; keeping the existing record")
    else:
        os.makedirs(os.path.dirname(record_file) or ".", exist_ok=True)
        with open(record_file, "w", encoding="utf-8") as handle:
            handle.write(NO_ESCALATION + "\n")
        print(f"no calls to {session}; recorded that no questions were escalated")
    sys.exit(0)

problems = []
if not record.strip():
    problems.append(f"{len(calls)} call(s) reached {session}, but {record_file} is missing or empty")
elif NO_ESCALATION in record:
    problems.append(f"{len(calls)} call(s) reached {session}, but {record_file} states that none were escalated")
else:
    # The lead cannot see call ids, so each answered call must at least have
    # its own "## " ruling section.
    answered = sum(1 for call in calls.values() if call["outcome"] == "success")
    sections = len(re.findall(r"^## ", record, re.MULTILINE))
    if sections < answered:
        problems.append(f"{answered} answered call(s) reached {session}, but {record_file} has only {sections} ruling section(s)")

def names(call_id):
    # A whole-token match, so a short id cannot match inside a longer token.
    return re.search(r"(?<![A-Za-z0-9_-])" + re.escape(call_id) + r"(?![A-Za-z0-9_-])", record) is not None


unanswered = [(call_id, call) for call_id, call in calls.items() if call["outcome"] != "success" and not names(call_id)]
for call_id, call in unanswered:
    outcome = call["outcome"] or "no recorded end"
    problems.append(f"call {call_id} from [{call['caller']}] ended before a ruling ({outcome}) and is not recorded")

if not problems:
    print(f"{record_file} accounts for {len(calls)} call(s) to {session}")
    sys.exit(0)

for problem in problems:
    print(problem, file=sys.stderr)
for call_id, call in unanswered:
    question = call["prompt"]
    if len(question) > MAX_QUESTION:
        question = question[:MAX_QUESTION] + "...[truncated]"
    print(f"--- question of call {call_id} ---\n{question}")
sys.exit(1)
PY
