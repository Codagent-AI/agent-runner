#!/bin/sh
# Splice a generated block into an existing pull request body and update it via REST.
set -eu

if ! command -v jq >/dev/null 2>&1; then
  printf 'jq is required to update the pull request body\n' >&2
  exit 1
fi
if [ "$#" -ne 2 ]; then
  printf 'usage: update-pr-body.sh <pr-number> <marked-block-file>\n' >&2
  exit 1
fi
number=$1
block=$2
case "$number" in
  ''|*[!0-9]*) printf 'pull request number must be a positive integer\n' >&2; exit 1 ;;
esac
if [ "$number" -eq 0 ] || [ ! -f "$block" ]; then
  printf 'pull request number must be positive and marked block file must exist\n' >&2
  exit 1
fi

start='<!-- agent-runner:generated-body:start -->'
end='<!-- agent-runner:generated-body:end -->'
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' 0
trap 'exit 1' 1 2 3 15

if ! gh pr view "$number" --json body -q .body > "$temp_dir/view"; then
  printf 'could not read pull request %s body\n' "$number" >&2
  exit 1
fi
if ! jq -Rsrj 'if endswith("\n") then .[:-1] else . end' "$temp_dir/view" > "$temp_dir/old"; then
  printf 'could not read pull request %s body\n' "$number" >&2
  exit 1
fi
if ! jq -nrj --rawfile old "$temp_dir/old" --rawfile block "$block" --arg start "$start" --arg end "$end" '
  ($block | split($start)) as $blockStarts |
  ($block | split($end)) as $blockEnds |
  if ($blockStarts | length) < 2 or ($blockEnds | length) < 2 or ($blockStarts[0] | contains($end)) then
    error("generated block must contain ordered body markers")
  else
    ($old | split($start)) as $oldStarts |
    ($old | split($end)) as $oldEnds |
    if ($oldStarts | length) > 1 and ($oldEnds | length) > 1 and ($oldStarts[0] | contains($end) | not) then
      $oldStarts[0] + ($block | rtrimstr("\n")) +
      ($oldStarts[1:] | join($start) | split($end) | .[1:] | join($end))
    elif $old == "" then
      $block
    else
      $block + (if $block | endswith("\n") then "\n" else "\n\n" end) + $old
    end
  end
' > "$temp_dir/body"; then
  printf 'could not splice generated pull request body\n' >&2
  exit 1
fi
if ! gh api --method PATCH "repos/{owner}/{repo}/pulls/$number" -F "body=@$temp_dir/body" >/dev/null; then
  printf 'could not update pull request %s body via REST\n' "$number" >&2
  exit 1
fi
printf 'updated pull request body for #%s\n' "$number"
