#!/bin/sh
# Stop before opening a pull request when bounded validator repair stayed red.
set -eu

result_file=$(jq -r '.result_file // ""')
if [ -z "$result_file" ] || [ ! -s "$result_file" ]; then
  printf 'validator result is missing: %s\n' "$result_file" >&2
  exit 1
fi

status=$(sed -n '1p' "$result_file")
case "$status" in
  PASS) exit 0 ;;
  FAIL) ;;
  *) printf 'invalid validator result: %s\n' "$status" >&2; exit 1 ;;
esac

branch=$(git branch --show-current)
if [ -z "$branch" ]; then
  printf 'cannot push failed validation from detached HEAD\n' >&2
  exit 1
fi
remote=$(git config "branch.$branch.remote" || true)
merge_ref=$(git config "branch.$branch.merge" || true)
if [ -n "$remote" ] && [ -n "$merge_ref" ]; then
  git push "$remote" "HEAD:$merge_ref"
else
  git push --set-upstream origin "$branch"
fi

printf 'validator remained red after bounded repair; branch pushed without opening a pull request:\n' >&2
sed '1d' "$result_file" >&2
exit 1
