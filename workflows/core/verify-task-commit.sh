#!/bin/sh
set -eu

# Input: {"starting_head": "<sha>"} plus the optional pair "started_at" (epoch
# seconds) and "record_path" (absolute path of the task's external delivery
# record). Without the pair only local delivery is verified.
payload=$(cat)

if command -v jq >/dev/null 2>&1; then
  if ! inputs=$(
    printf '%s' "$payload" |
      jq -r '
        def ctl: explode | any(. < 32 or . == 127);
        if type != "object" then
          error("input must be a JSON object")
        elif (.starting_head | type) != "string" then
          error("starting_head must be a string")
        elif has("started_at") != has("record_path") then
          error("started_at and record_path must be supplied together")
        elif has("started_at") and ((.started_at | type) != "string" or (.record_path | type) != "string") then
          error("started_at and record_path must be strings")
        else
          # A captured HEAD keeps its trailing newline.
          .starting_head |= sub("\n+$"; "") |
          if [.starting_head, .started_at, .record_path] | any(type == "string" and ctl) then
            error("inputs must not contain control characters")
          else
            .starting_head, (.started_at // ""), (.record_path // "")
          end
        end
      '
  ); then
    echo "verify-task-commit: invalid input" >&2
    exit 2
  fi
else
  inputs=$(PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

def fail(message):
    print(f"verify-task-commit: {message}", file=sys.stderr)
    sys.exit(2)

try:
    parsed = json.loads(os.environ["PAYLOAD"])
except json.JSONDecodeError as exc:
    fail(f"invalid JSON input: {exc}")
if not isinstance(parsed, dict):
    fail("input must be a JSON object")
starting_head = parsed.get("starting_head")
if not isinstance(starting_head, str):
    fail("starting_head must be a string")
# A captured HEAD keeps its trailing newline.
starting_head = starting_head.rstrip("\n")
if ("started_at" in parsed) != ("record_path" in parsed):
    fail("started_at and record_path must be supplied together")
started_at = parsed.get("started_at", "")
record_path = parsed.get("record_path", "")
if not isinstance(started_at, str) or not isinstance(record_path, str):
    fail("started_at and record_path must be strings")
for value in (starting_head, started_at, record_path):
    if any(ord(ch) < 32 or ord(ch) == 127 for ch in value):
        fail("inputs must not contain control characters")
print(starting_head)
print(started_at)
print(record_path)
PY
  ) || exit 2
fi

starting_head=$(printf '%s\n' "$inputs" | sed -n 1p)
started_at=$(printf '%s\n' "$inputs" | sed -n 2p)
record_path=$(printf '%s\n' "$inputs" | sed -n 3p)

case "$starting_head" in
  ""|*[!0-9a-f]*)
    printf 'verify-task-commit: invalid starting commit: %s\n' "$starting_head" >&2
    exit 2
    ;;
esac

if [ -n "$record_path" ]; then
  case "$started_at" in
    ""|*[!0-9]*)
      printf 'verify-task-commit: invalid started_at: %s\n' "$started_at" >&2
      exit 2
      ;;
  esac
  case "$record_path" in
    /*) ;;
    *)
      printf 'verify-task-commit: record_path must be absolute: %s\n' "$record_path" >&2
      exit 2
      ;;
  esac
fi

current_head=$(git rev-parse --verify HEAD)

# Local delivery. The external delivery record is consulted only when HEAD is
# unchanged, so a broken local history is never rescued by a record.
if [ "$current_head" != "$starting_head" ]; then
  if ! git merge-base --is-ancestor "$starting_head" "$current_head"; then
    echo "implementation task ended at a commit that is not a descendant of its starting HEAD" >&2
    exit 1
  fi

  if git diff --quiet "$starting_head" "$current_head"; then
    echo "implementation task produced commits but no tracked implementation changes" >&2
    exit 1
  fi

  commit_count=$(git rev-list --count "$starting_head..$current_head")
  printf 'implementation task produced %s commit(s)\n' "$commit_count"
  exit 0
fi

echo "implementation task did not produce a commit; refusing to advance to the next task" >&2

if [ -z "$record_path" ]; then
  exit 1
fi

if [ ! -e "$record_path" ]; then
  printf 'no external delivery record at %s\n' "$record_path" >&2
  exit 1
fi

reject() {
  printf 'external delivery record %s rejected: %s\n' "$record_path" "$1" >&2
  exit 1
}

if [ ! -f "$record_path" ]; then
  reject "malformed external delivery record: not a regular file"
fi
record_size=$(wc -c <"$record_path" | tr -d ' ')
if [ "$record_size" -gt 65536 ]; then
  reject "malformed external delivery record: larger than 64 KiB"
fi

# The parsers emit "ok" followed by repository, branch, pull request, and one
# commit per line, or "invalid" followed by the reason. Control characters are
# rejected, so every field fits on one line.
if command -v jq >/dev/null 2>&1; then
  if ! parsed=$(
    jq -rs '
      def ctl: explode | any(. < 32 or . == 127);
      def reason:
        if type != "object" then "record must be a JSON object"
        elif (.repository | type) != "string" then "repository must be a string"
        elif (.repository | ctl) then "repository must not contain control characters"
        elif (.repository | startswith("/") | not) then "repository must be an absolute path"
        elif (.commits | type) != "array" then "commits must be an array"
        elif (.commits | length) == 0 then "commits must not be empty"
        elif (.commits | length) > 100 then "commits must list at most 100 entries"
        elif any(.commits[]; type != "string" or (test("^[0-9a-f]{7,64}$") | not)) then "commit IDs must be 7-64 lowercase hex characters"
        elif any(.branch, .pull_request; . != null and type != "string") then "branch and pull_request must be strings"
        elif any(.branch, .pull_request; type == "string" and ctl) then "branch and pull_request must not contain control characters"
        else empty
        end;
      if length != 1 then
        "invalid", "record must contain a single JSON value"
      else
        .[0] | [reason] as $r |
        if ($r | length) > 0 then
          "invalid", $r[0]
        else
          "ok", .repository, (.branch // ""), (.pull_request // ""), .commits[]
        end
      end
    ' "$record_path" 2>/dev/null
  ); then
    parsed=$(printf 'invalid\nrecord is not valid JSON')
  fi
else
  parsed=$(RECORD_PATH="$record_path" python3 - <<'PY'
import json
import os
import re

def ctl(value):
    return any(ord(ch) < 32 or ord(ch) == 127 for ch in value)

def reason(record):
    if not isinstance(record, dict):
        return "record must be a JSON object"
    repository = record.get("repository")
    if not isinstance(repository, str):
        return "repository must be a string"
    if ctl(repository):
        return "repository must not contain control characters"
    if not repository.startswith("/"):
        return "repository must be an absolute path"
    commits = record.get("commits")
    if not isinstance(commits, list):
        return "commits must be an array"
    if not commits:
        return "commits must not be empty"
    if len(commits) > 100:
        return "commits must list at most 100 entries"
    if any(not isinstance(c, str) or not re.fullmatch(r"[0-9a-f]{7,64}", c) for c in commits):
        return "commit IDs must be 7-64 lowercase hex characters"
    optional = [record.get("branch"), record.get("pull_request")]
    if any(v is not None and not isinstance(v, str) for v in optional):
        return "branch and pull_request must be strings"
    if any(isinstance(v, str) and ctl(v) for v in optional):
        return "branch and pull_request must not contain control characters"
    return None

try:
    with open(os.environ["RECORD_PATH"], "rb") as handle:
        record = json.loads(handle.read().decode("utf-8"))
except (ValueError, UnicodeDecodeError, RecursionError):
    print("invalid")
    print("record is not valid JSON")
    raise SystemExit(0)

problem = reason(record)
if problem:
    print("invalid")
    print(problem)
else:
    print("ok")
    print(record["repository"])
    print(record.get("branch") or "")
    print(record.get("pull_request") or "")
    for commit in record["commits"]:
        print(commit)
PY
  )
fi

status=$(printf '%s\n' "$parsed" | sed -n 1p)
if [ "$status" != ok ]; then
  reject "malformed external delivery record: $(printf '%s\n' "$parsed" | sed -n 2p)"
fi
repository=$(printf '%s\n' "$parsed" | sed -n 2p)
branch=$(printf '%s\n' "$parsed" | sed -n 3p)
pull_request=$(printf '%s\n' "$parsed" | sed -n 4p)
commits=$(printf '%s\n' "$parsed" | sed -n '5,$p')

# Only object-level plumbing runs against the external repository, without
# optional locks or its configured filesystem monitor, so nothing it configures
# is executed and nothing in it is modified.
repo=$repository
xgit() {
  (
    unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_NAMESPACE
    GIT_OPTIONAL_LOCKS=0 git -C "$repo" -c core.fsmonitor=false "$@"
  )
}

unusable="external repository $repository: not a usable repository worktree"
[ -d "$repo" ] || reject "$unusable"
is_bare=$(xgit rev-parse --is-bare-repository 2>/dev/null) || reject "$unusable"
[ "$is_bare" = false ] || reject "$unusable"
root=$(xgit rev-parse --show-toplevel 2>/dev/null) || reject "$unusable"
[ -n "$root" ] || reject "$unusable"
repo=$root

external_common=$(xgit rev-parse --git-common-dir 2>/dev/null) || reject "$unusable"
external_common=$(cd "$root" && cd "$external_common" && pwd -P) || reject "$unusable"
run_common=$(cd "$(git rev-parse --git-common-dir)" && pwd -P)
if [ "$external_common" = "$run_common" ]; then
  reject "external repository $repository: named repository is the run repository"
fi

empty_tree=$(xgit hash-object -t tree /dev/null)
earliest=$((started_at - 300))
default_branches=$(
  xgit for-each-ref --format='%(refname) %(symref)' refs/remotes/ |
    awk '$1 ~ /\/HEAD$/ && $2 != "" { print $2 }'
)

report=""
for id in $commits; do
  full=$(xgit rev-parse --verify --quiet "$id^{commit}" 2>/dev/null) ||
    reject "commit $id: not found in $root"

  containing_ref=$(
    xgit for-each-ref --contains "$full" --format='%(refname)' refs/remotes/ |
      awk '!/\/HEAD$/ { print; exit }'
  )
  [ -n "$containing_ref" ] ||
    reject "commit $id: not reachable from any remote-tracking ref"

  tree=$(xgit rev-parse "$full^{tree}")
  if parent_tree=$(xgit rev-parse --verify --quiet "$full^1^{tree}" 2>/dev/null); then
    :
  else
    parent_tree=$empty_tree
  fi
  [ "$tree" != "$parent_tree" ] ||
    reject "commit $id: contains no tracked changes"

  committed_at=$(
    xgit cat-file commit "$full" |
      awk '/^$/ { exit } /^committer / { print $(NF - 1); exit }'
  )
  case "$committed_at" in
    ""|*[!0-9]*) reject "commit $id: committer date is unreadable" ;;
  esac
  [ "$committed_at" -ge "$earliest" ] ||
    reject "commit $id: predates the task (committed at $committed_at, task started at $started_at)"

  for default_branch in $default_branches; do
    if xgit merge-base --is-ancestor "$full" "$default_branch" 2>/dev/null; then
      reject "commit $id: already on the default branch $default_branch"
    else
      merged_status=$?
      [ "$merged_status" -eq 1 ] ||
        reject "commit $id: could not check the default branch $default_branch"
    fi
  done

  report="${report}commit: $full (contained in $containing_ref)
"
done

accepted=$(
  echo "task delivered outside this repository"
  printf 'repository: %s\n' "$root"
  printf '%s' "$report"
  if [ -n "$branch" ]; then
    printf 'branch (reported, unverified): %s\n' "$branch"
  fi
  if [ -n "$pull_request" ]; then
    printf 'pull request (reported, unverified): %s\n' "$pull_request"
  fi
  echo "note: pushed state was judged from this clone's local remote-tracking refs; the remote was not contacted"
  echo "note: this run's validator and task-compliance review did not cover the external work"
  printf 'record: %s\n' "$record_path"
)

# Downstream steps list only accepted records, never ones the gate rejected.
printf '%s\n' "$accepted" >"${record_path%.json}.accepted"
printf '%s\n' "$accepted"
