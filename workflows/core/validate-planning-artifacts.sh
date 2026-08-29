#!/bin/sh
set -eu

payload=$(cat)

parsed=$(VALIDATION_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

try:
    value = json.loads(os.environ["VALIDATION_PAYLOAD"])
except json.JSONDecodeError as exc:
    print(f"validate-planning-artifacts: invalid JSON input: {exc}", file=sys.stderr)
    sys.exit(2)

if not isinstance(value, dict):
    print("validate-planning-artifacts: input must be a JSON object", file=sys.stderr)
    sys.exit(2)

keys = ("change_name", "change_dir", "change_kind", "require_tasks")
items = [value.get(key) for key in keys]
if not all(isinstance(item, str) for item in items):
    print(
        "validate-planning-artifacts: change_name, change_dir, change_kind, and require_tasks must be strings",
        file=sys.stderr,
    )
    sys.exit(2)

print(*items, sep="\n")
PY
)

change_name=$(printf '%s\n' "$parsed" | sed -n '1p')
change_dir=$(printf '%s\n' "$parsed" | sed -n '2p')
change_kind=$(printf '%s\n' "$parsed" | sed -n '3p')
require_tasks=$(printf '%s\n' "$parsed" | sed -n '4p')

case "$change_name" in
  ""|[!a-z0-9]*|*[!a-z0-9-]*)
    printf 'validate-planning-artifacts: change_name must use lowercase letters, digits, and hyphens: %s\n' "$change_name" >&2
    exit 1
    ;;
esac

case "$change_kind" in
  openspec)
    expected_dir="openspec/changes/$change_name"
    ;;
  spec-driven)
    ;;
  *)
    printf 'validate-planning-artifacts: unsupported change_kind: %s\n' "$change_kind" >&2
    exit 1
    ;;
esac

if [ -z "$change_dir" ]; then
  printf 'validate-planning-artifacts: change_dir must not be empty\n' >&2
  exit 1
fi

if [ "$change_kind" = "openspec" ] && [ "$change_dir" != "$expected_dir" ]; then
  printf 'validate-planning-artifacts: change_dir must be %s for %s: %s\n' "$expected_dir" "$change_kind" "$change_dir" >&2
  exit 1
fi

case "$require_tasks" in
  true|false)
    ;;
  *)
    printf 'validate-planning-artifacts: require_tasks must be true or false: %s\n' "$require_tasks" >&2
    exit 1
    ;;
esac

if [ "$change_kind" = "openspec" ]; then
  openspec validate --type change "$change_name"
fi

VALIDATION_CHANGE_DIR="$change_dir" \
  VALIDATION_CHANGE_KIND="$change_kind" \
  VALIDATION_REQUIRE_TASKS="$require_tasks" \
  python3 - <<'PY'
import os
import re
import sys
from pathlib import Path

change_dir = Path(os.environ["VALIDATION_CHANGE_DIR"])
change_kind = os.environ["VALIDATION_CHANGE_KIND"]
require_tasks = os.environ["VALIDATION_REQUIRE_TASKS"] == "true"

failures = []

for required in ("proposal.md", "design.md", "test-plan.md"):
    path = change_dir / required
    if not path.is_file() or path.stat().st_size == 0:
        failures.append(f"missing or empty {path}")

test_plan_path = change_dir / "test-plan.md"
if test_plan_path.is_file() and test_plan_path.stat().st_size > 0:
    test_plan = test_plan_path.read_text(encoding="utf-8")
    section_names = (
        "Coverage Strategy",
        "Integration Tests",
        "End-to-End Tests",
        "Agent Acceptance Tests",
        "Human-Only Testing",
        "Coverage Map",
    )
    section_pattern = re.compile(r"^##\s+(.+?)\s*$", re.MULTILINE)
    section_matches = list(section_pattern.finditer(test_plan))
    sections = {}
    for index, match in enumerate(section_matches):
        name = match.group(1)
        end = section_matches[index + 1].start() if index + 1 < len(section_matches) else len(test_plan)
        if name in section_names:
            if name in sections:
                failures.append(f"{test_plan_path} contains duplicate section '## {name}'")
            else:
                sections[name] = (match.end(), end)

    for name in section_names:
        if name not in sections:
            failures.append(f"{test_plan_path} is missing '## {name}'")

    present_sections = [match.group(1) for match in section_matches if match.group(1) in section_names]
    expected_present_sections = [name for name in section_names if name in sections]
    if present_sections != expected_present_sections:
        failures.append(f"{test_plan_path} test-plan sections are out of order")

    obligation_pattern = re.compile(
        r"^###\s+((?:INT|E2E|AT|HT)-[A-Za-z0-9][A-Za-z0-9._-]*):\s*(.+?)\s*$",
        re.MULTILINE,
    )
    obligation_matches = list(obligation_pattern.finditer(test_plan))
    obligations = {}
    for match in obligation_matches:
        obligation_id = match.group(1)
        if obligation_id in obligations:
            failures.append(f"{test_plan_path} contains duplicate obligation ID {obligation_id}")
        else:
            obligations[obligation_id] = match

    required_fields = {
        "INT": ("Covers", "Boundary", "Setup", "Action", "Assertions", "Execution"),
        "E2E": ("Covers", "Surface", "Setup", "Journey", "Assertions", "Execution"),
        "AT": (
            "Classification",
            "Covers",
            "Actor and surface",
            "Setup",
            "Steps",
            "Expected",
            "Evidence",
            "Effects and cleanup",
            "Permitted substitutes",
        ),
        "HT": ("Reason", "Prerequisites", "Instructions", "Required decision or observation"),
    }
    expected_sections = {
        "INT": "Integration Tests",
        "E2E": "End-to-End Tests",
        "AT": "Agent Acceptance Tests",
        "HT": "Human-Only Testing",
    }
    heading_boundary_pattern = re.compile(r"^#{2,3}\s+", re.MULTILINE)
    for match in obligation_matches:
        obligation_id = match.group(1)
        prefix = obligation_id.split("-", 1)[0]
        next_heading = heading_boundary_pattern.search(test_plan, match.end())
        end = next_heading.start() if next_heading else len(test_plan)
        body = test_plan[match.end():end]
        section_name = expected_sections[prefix]
        section_range = sections.get(section_name)
        if section_range is not None and not (section_range[0] <= match.start() < section_range[1]):
            failures.append(f"{test_plan_path} places {obligation_id} outside '## {section_name}'")
        for field in required_fields[prefix]:
            field_pattern = re.compile(rf"^-\s+{re.escape(field)}:\s*(\S.*?)\s*$", re.MULTILINE)
            if not field_pattern.search(body):
                failures.append(f"{test_plan_path} obligation {obligation_id} is missing non-empty '{field}'")
        if prefix == "AT":
            classification = re.search(r"^-\s+Classification:\s*(.*?)\s*$", body, re.MULTILINE)
            value = classification.group(1).strip() if classification else ""
            if value != "Required" and not (
                value.startswith("Conditional:") and value.removeprefix("Conditional:").strip()
            ):
                failures.append(
                    f"{test_plan_path} obligation {obligation_id} classification must be "
                    "'Required' or 'Conditional: <condition>'"
                )

    human_only_range = sections.get("Human-Only Testing")
    if human_only_range is not None and not any(key.startswith("HT-") for key in obligations):
        human_only = test_plan[human_only_range[0]:human_only_range[1]]
        if not re.search(r"(?im)^\s*None\.\s*$", human_only):
            failures.append(
                f"{test_plan_path} human-only testing must say 'None.' or define at least one HT-* obligation"
            )

    coverage_map_range = sections.get("Coverage Map")
    if coverage_map_range is not None:
        coverage_map = test_plan[coverage_map_range[0]:coverage_map_range[1]]
        referenced_ids = set(
            re.findall(r"\b(?:INT|E2E|AT|HT)-[A-Za-z0-9][A-Za-z0-9._-]*\b", coverage_map)
        )
        for obligation_id in sorted(referenced_ids):
            if obligation_id not in obligations:
                failures.append(f"{test_plan_path} coverage map references undefined {obligation_id}")
        for obligation_id in sorted(set(obligations) - referenced_ids):
            failures.append(f"{test_plan_path} coverage map does not reference {obligation_id}")

spec_paths = sorted(change_dir.glob("specs/*/spec.md"))
spec_paths = [path for path in spec_paths if path.is_file() and path.stat().st_size > 0]
if not spec_paths:
    failures.append(f"no non-empty specification under {change_dir}/specs/")

if change_kind == "spec-driven":
    requirement_pattern = re.compile(r"^### Requirement:\s*(.+?)\s*$", re.MULTILINE)
    scenario_pattern = re.compile(r"^#### Scenario:\s*(.+?)\s*$", re.MULTILINE)
    for path in spec_paths:
        text = path.read_text(encoding="utf-8")
        requirements = list(requirement_pattern.finditer(text))
        if not requirements:
            failures.append(f"{path} contains no '### Requirement:' heading")
            continue
        for index, requirement in enumerate(requirements):
            end = requirements[index + 1].start() if index + 1 < len(requirements) else len(text)
            body = text[requirement.end():end]
            label = f"{path}: requirement {requirement.group(1)!r}"
            if not re.search(r"\b(?:SHALL|MUST)\b", body):
                failures.append(f"{label} lacks SHALL or MUST language")
            scenarios = list(scenario_pattern.finditer(body))
            if not scenarios:
                failures.append(f"{label} has no '#### Scenario:'")
                continue
            for scenario_index, scenario in enumerate(scenarios):
                scenario_end = scenarios[scenario_index + 1].start() if scenario_index + 1 < len(scenarios) else len(body)
                scenario_body = body[scenario.end():scenario_end]
                scenario_label = f"{label}, scenario {scenario.group(1)!r}"
                if not re.search(r"\*\*WHEN\*\*", scenario_body):
                    failures.append(f"{scenario_label} lacks WHEN behavior")
                if not re.search(r"\*\*THEN\*\*", scenario_body):
                    failures.append(f"{scenario_label} lacks THEN behavior")

if failures:
    for failure in failures:
        print(f"validate-planning-artifacts: {failure}", file=sys.stderr)
    sys.exit(1)

print(f"validated planning artifacts in {change_dir}")
PY

if [ "$require_tasks" = "true" ]; then
  workspace_dir=$(pwd -P)
  change_dir_absolute=$(CDPATH= cd -- "$change_dir" && pwd -P)
  "${AGENT_RUNNER_EXECUTABLE:-agent-runner}" internal task-groups \
    --workspace-dir "$workspace_dir" \
    --change-dir "$change_dir_absolute" \
    --plan-kind full \
    --output repositories >/dev/null
fi
