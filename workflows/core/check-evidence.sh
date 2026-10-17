#!/bin/sh
# Checks that the merged checklist is complete before the audit: the first
# Markdown table with Status and Evidence columns must give every row a final
# status, and each claim must point at its evidence.
#
#   - Status is `verified`, `failed`, or `not verified`.
#   - A verified row cites, in backticks, at least one non-empty file in the
#     evidence directory: a path relative to it, or an absolute path inside it.
#   - A failed or not-verified row says why in its Evidence cell.
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
      add_problem "$id has status '${status}'; use one of: verified, failed, not verified"
      ;;
  esac
done <<EOF
$rows
EOF

if [ "$total" -eq 0 ]; then
  add_problem "the checklist table has no rows"
fi

record_problems

if [ -n "$problems" ]; then
  printf 'evidence check: %s is not complete:\n%s' "$checklist_file" "$problems" >&2
  printf 'Cite evidence files in backticks, relative to %s or as absolute paths inside it.\n' "$evidence_dir" >&2
  exit 1
fi

printf 'evidence check: %d rows: %d verified, %d failed, %d not verified\n' "$total" "$verified" "$failed" "$unverified"
