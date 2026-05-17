---
description: >-
  Creates a release PR for agent-runner from merged PRs since the previous
  release tag plus the current branch PR when run from a feature branch. Use
  when the user says "release", "cut a release", "create a release PR",
  "prepare a release", or "bump version".
---

Create a release PR by gathering release candidates, calculating the semver
bump, writing a changelog section, and opening or updating the release PR.

## Steps

### 1. Gather release info

Run:

```bash
bash .claude/skills/release/scripts/release-info.sh
```

Capture the JSON output. If the output contains `"error"`, stop and report the
message to the user.

### 2. Show the release summary

Display:

- Version bump: `current_version` -> `new_version`
- Number of PRs by category
- Each PR number and title, grouped by Major / Minor / Patch

### 3. Write changelog descriptions

For each PR in the JSON output, write a one-sentence description that is more
informative than the raw PR title. Strip the conventional commit prefix from
the title and keep enough context to understand the change without clicking
through.

### 4. Format the changelog section

Build the changelog section for the new version. Only include sections that
have entries. Sort entries by PR number ascending within each section.

Format:

```markdown
## <new_version>

### Major Changes

- [#<number>](https://github.com/Codagent-AI/agent-runner/pull/<number>) <description>

### Minor Changes

- [#<number>](https://github.com/Codagent-AI/agent-runner/pull/<number>) <description>

### Patch Changes

- [#<number>](https://github.com/Codagent-AI/agent-runner/pull/<number>) <description>
```

### 5. Write the changelog section to a temp file

```bash
TMPFILE=$(mktemp)
cat <<'CHANGELOG_EOF' > "$TMPFILE"
<paste the formatted changelog section here>
CHANGELOG_EOF
echo "$TMPFILE"
```

### 6. Create the release PR

Run:

```bash
bash .claude/skills/release/scripts/create-release-pr.sh <new_version> <tmpfile_path>
```

When run on `main`, the script creates `release/v<new_version>` and opens a
release PR. When run on another branch, it commits the release changes directly
to that branch and prints its PR URL.

### 7. Report

Print the PR URL returned by the script, then clean up:

```bash
rm -f "$TMPFILE"
```
