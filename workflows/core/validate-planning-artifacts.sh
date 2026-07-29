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
    expected_dir="specs/changes/$change_name"
    ;;
  *)
    printf 'validate-planning-artifacts: unsupported change_kind: %s\n' "$change_kind" >&2
    exit 1
    ;;
esac

if [ "$change_dir" != "$expected_dir" ]; then
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
import posixpath
import re
import sys
from pathlib import Path
from urllib.parse import unquote

change_dir = Path(os.environ["VALIDATION_CHANGE_DIR"])
change_kind = os.environ["VALIDATION_CHANGE_KIND"]
require_tasks = os.environ["VALIDATION_REQUIRE_TASKS"] == "true"

failures = []

for required in ("proposal.md", "design.md", "test-plan.md"):
    path = change_dir / required
    if not path.is_file() or path.stat().st_size == 0:
        failures.append(f"missing or empty {path}")

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

if require_tasks:
    task_index = change_dir / "tasks.md"
    if not task_index.is_file() or task_index.stat().st_size == 0:
        failures.append(f"missing or empty {task_index}")
    task_paths = sorted(
        path for path in (change_dir / "tasks").glob("*.md")
        if path.is_file() and path.stat().st_size > 0
    )
    if not task_paths:
        failures.append(f"no non-empty task file under {change_dir}/tasks/")
    elif task_index.is_file():
        index_text = task_index.read_text(encoding="utf-8")
        destinations = set()
        for raw in re.findall(r"(?<!!)\[[^\]]+\]\(([^)]+)\)", index_text):
            destination = raw.strip().strip("<>").split("#", 1)[0].split("?", 1)[0]
            destinations.add(posixpath.normpath(unquote(destination)))
        for task_path in task_paths:
            relative = task_path.relative_to(change_dir).as_posix()
            if relative not in destinations:
                failures.append(f"{task_index} does not link to {relative}")

if failures:
    for failure in failures:
        print(f"validate-planning-artifacts: {failure}", file=sys.stderr)
    sys.exit(1)

print(f"validated planning artifacts in {change_dir}")
PY
