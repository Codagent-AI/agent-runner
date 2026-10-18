#!/bin/sh
# Checks that the merged checklist is complete before the audit: the first
# Markdown table with Status and Evidence columns must give every row a final
# status, and each claim must point at its evidence.
#
#   - Status is `verified`, `failed`, or `not verified`.
#   - A verified row cites, in backticks, at least one non-empty file in the
#     evidence directory: a path relative to it, or an absolute path inside it.
#   - A failed or not-verified row says why in its Evidence cell.
#   - The checklist cites no acceptance (AT-n) or human-only (HT-n) test:
#     manual testing happens once, in acceptance testing, and is never a
#     checklist check.
#   - When plan_file (the merged checklist as committed before implementation)
#     is given, every planned row is still there with its planned columns
#     unchanged, and no row was added. When findings (the audit's) is also
#     given, rows may be added, and a planned row may change only if a finding
#     names its ID.
#
# When problems_file is given, the problem list is written there on failure
# and the file is removed on success, so a later step can tell whether the
# checklist still needs work.
#
# This checks that the checklist is complete, not that its claims are true;
# the audit judges whether the evidence proves each behavior.
set -eu
# Cited paths are split unquoted below; never glob-expand them.
set -f

payload=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to read the evidence check input\n' >&2
  exit 1
fi
checklist_file=$(printf '%s' "$payload" | jq -r '.checklist_file // ""')
evidence_dir=$(printf '%s' "$payload" | jq -r '.evidence_dir // ""')
evidence_dir=${evidence_dir%/}
problems_file=$(printf '%s' "$payload" | jq -r '.problems_file // ""')
plan_file=$(printf '%s' "$payload" | jq -r '.plan_file // ""')
has_findings=$(printf '%s' "$payload" | jq -r 'if has("findings") then "yes" else "" end')
findings=$(printf '%s' "$payload" | jq -r '.findings // ""')

# Reports a problem that stops the check before the rows are read.
fail() {
  if [ -n "$problems_file" ]; then
    printf -- '- %s\n' "$1" > "$problems_file"
  fi
  printf 'evidence check: %s\n' "$1" >&2
  exit 1
}

if [ -z "$checklist_file" ] || [ -z "$evidence_dir" ]; then
  fail "checklist_file and evidence_dir are required"
fi
if [ ! -s "$checklist_file" ]; then
  fail "$checklist_file is missing or empty"
fi

# Non-whitespace separators, so read keeps empty fields and a cited path may
# contain spaces.
sep=$(printf '\037')
path_sep=$(printf '\036')

# Emits one separator-delimited record per data row: ID, normalized status, the
# backticked paths of its Evidence cell (separated by path_sep, without a `..`
# segment), and whether that cell has any text.
rows=$(awk '
  function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
  function cells(line, out,    n, i) {
    gsub(/\\\|/, "\001", line)
    sub(/^[ \t]*\|/, "", line)
    sub(/\|[ \t]*$/, "", line)
    n = split(line, out, "|")
    for (i = 1; i <= n; i++) { gsub("\001", "|", out[i]); out[i] = trim(out[i]) }
    return n
  }
  done { next }
  !/^[ \t]*\|/ { if (in_table) done = 1; next }
  in_table && /^[ \t]*\|[ \t:|-]*$/ { next }
  in_table {
    rownum++
    cells($0, c)
    id = (id_col && c[id_col] != "") ? c[id_col] : "row " rownum
    status = tolower(c[status_col])
    gsub(/[`*_]/, "", status)
    gsub(/[ \t]+/, " ", status)
    status = trim(status)
    evidence = c[evidence_col]
    tokens = ""
    rest = evidence
    while (match(rest, /`[^`]+`/)) {
      token = substr(rest, RSTART + 1, RLENGTH - 2)
      rest = substr(rest, RSTART + RLENGTH)
      if (token !~ /(^|\/)\.\.(\/|$)/) tokens = tokens (tokens == "" ? "" : "\036") token
    }
    printf "%s\037%s\037%s\037%s\n", id, status, tokens, (evidence == "" ? "empty" : "text")
    next
  }
  {
    n = cells($0, h)
    status_col = evidence_col = id_col = 0
    for (i = 1; i <= n; i++) {
      name = tolower(h[i])
      if (name == "status") status_col = i
      else if (name == "evidence") evidence_col = i
      else if (name == "id") id_col = i
    }
    if (status_col && evidence_col) { in_table = 1; found = 1 }
  }
  END { if (!found) print "NO_TABLE" }
' "$checklist_file")

if [ "$rows" = "NO_TABLE" ]; then
  fail "$checklist_file has no Markdown table with Status and Evidence columns"
fi

problems=""
total=0
verified=0
failed=0
unverified=0

add_problem() {
  problems="${problems}- $1
"
}

# Records the outcome for a later step: the problem list on failure, nothing
# on success.
record_problems() {
  [ -n "$problems_file" ] || return 0
  if [ -n "$problems" ]; then
    printf '%s' "$problems" > "$problems_file"
  else
    rm -f "$problems_file"
  fi
}

while IFS="$sep" read -r id status tokens has_text; do
  [ -n "$id" ] || continue
  total=$((total + 1))
  case "$status" in
    verified)
      verified=$((verified + 1))
      cited=""
      IFS=$path_sep
      for token in $tokens; do
        case "$token" in
          "$evidence_dir"/*) path=$token ;;
          /*) continue ;;
          *) path="$evidence_dir/$token" ;;
        esac
        if [ -f "$path" ] && [ -s "$path" ]; then
          cited=yes
          break
        fi
      done
      unset IFS
      if [ -z "$cited" ]; then
        add_problem "$id is verified but cites no non-empty file in $evidence_dir"
      fi
      ;;
    failed|"not verified")
      if [ "$status" = failed ]; then
        failed=$((failed + 1))
      else
        unverified=$((unverified + 1))
      fi
      if [ "$has_text" != text ]; then
        add_problem "$id is $status but its Evidence cell does not say why"
      fi
      ;;
    *)
      add_problem "$id has status '${status}'; Status holds only verified, failed, or not verified, and evidence file names go in the Evidence column"
      ;;
  esac
done <<EOF
$rows
EOF

if [ "$total" -eq 0 ]; then
  add_problem "the checklist table has no rows"
fi

manual=$(grep -oE '(^|[^A-Za-z0-9-])(AT|HT)-[0-9]+' "$checklist_file" | sed -E 's/^[^AH]//' | sort -u | paste -sd ',' - | sed 's/,/, /g' || true)
if [ -n "$manual" ]; then
  add_problem "the checklist cites manual tests, which are not checklist checks: $manual; verify each behavior with an automated test, a scripted command, or inspection"
fi

if [ -n "$plan_file" ]; then
  plan_problems=$(CHECKLIST_FILE="$checklist_file" PLAN_FILE="$plan_file" HAS_FINDINGS="$has_findings" AUDIT_FINDINGS="$findings" python3 - <<'PY'
import os
import re
from collections import Counter

DONE_COLUMNS = {"code", "evidence", "status"}


def cells(line):
    line = line.strip().replace("\\|", "\x01")
    line = re.sub(r"^\|", "", line)
    line = re.sub(r"\|$", "", line)
    return [" ".join(c.replace("\x01", "|").split()) for c in line.split("|")]


def id_table(path, planned=False):
    """Returns the header and rows of the first table with an ID column."""
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            lines = f.read().splitlines()
    except OSError as exc:
        print(f"cannot read {path}: {exc.strerror}")
        raise SystemExit(0)
    header, rows = None, []
    for line in lines:
        if not line.lstrip().startswith("|"):
            if header is not None and "id" in header:
                break
            header, rows = None, []
            continue
        if header is None:
            header = [c.lower() for c in cells(line)]
        elif not re.fullmatch(r"[\s:|-]*", line):
            rows.append(cells(line))
    if header is None or "id" not in header:
        if planned:
            print(f"the planned checklist {path} has no table with a header cell exactly ID; this is a merge-step defect that the evidence repair cannot fix")
        else:
            print(f"{path} has no Markdown table with an ID column")
        raise SystemExit(0)
    return header, rows


def cell(header, row, name):
    i = header.index(name)
    return row[i] if i < len(row) else ""


def named(id_cell, findings):
    ids = re.findall(r"[A-Za-z]+-\d+", id_cell)
    if not ids:
        return bool(id_cell) and id_cell in findings
    return any(re.search(r"(?<![A-Za-z0-9-])" + re.escape(i) + r"(?!\d)", findings) for i in ids)


plan_path = os.environ["PLAN_FILE"]
plan_header, plan_rows = id_table(plan_path, planned=True)
header, rows = id_table(os.environ["CHECKLIST_FILE"])
columns = [c for c in plan_header if c and c not in DONE_COLUMNS]
lost = [c for c in columns if c not in header]
if lost:
    print(f"the checklist table lost the planned column(s) {', '.join(lost)} of {plan_path}")
    raise SystemExit(0)

after_audit = os.environ["HAS_FINDINGS"] == "yes"
findings = os.environ["AUDIT_FINDINGS"]
current = Counter(tuple(cell(header, r, c) for c in columns) for r in rows)
changed = set()
for r in plan_rows:
    key = tuple(cell(plan_header, r, c) for c in columns)
    if current[key]:
        current[key] -= 1
        continue
    row_id = cell(plan_header, r, "id") or "a row"
    if after_audit and named(row_id, findings):
        continue
    changed.add(row_id)
    rule = "only a row an audit finding names may change them" if after_audit else "implementation fills in only Code, Evidence, and Status"
    print(f"{row_id} was changed or removed: restore its planned cells ({', '.join(columns)}) as they are in {plan_path}; {rule}")
if not after_audit:
    for key, count in current.items():
        if count > 0 and key[columns.index("id")] not in changed:
            print(f"{key[columns.index('id')] or 'a row'} is not in the planned checklist {plan_path}: remove the row, or restore the planned row it changed")
PY
) || plan_problems="the planned checklist comparison failed to run"
  while IFS= read -r line; do
    if [ -n "$line" ]; then
      add_problem "$line"
    fi
  done <<EOF
$plan_problems
EOF
fi

record_problems

if [ -n "$problems" ]; then
  printf 'evidence check: %s is not complete:\n%s' "$checklist_file" "$problems" >&2
  printf 'Cite evidence files in backticks, relative to %s or as absolute paths inside it.\n' "$evidence_dir" >&2
  exit 1
fi

printf 'evidence check: %d rows: %d verified, %d failed, %d not verified\n' "$total" "$verified" "$failed" "$unverified"
