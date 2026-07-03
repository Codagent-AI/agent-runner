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
message to the user. In particular, do not continue from a feature branch whose
associated PR is already merged/closed or does not target `main`; switch to or
create the open PR for the current unreleased changes first.

### 2. Reclassify internal-only changes

The `major`/`minor` buckets from the script are based only on conventional-commit
prefixes (`feat:`, `!:`, a `breaking` label) — they say nothing about whether the
change is visible to an end user of the built `agent-runner` binary/CLI. Before
showing the summary, review every PR in the `major` and `minor` buckets and
downgrade any that are purely internal or development-facing to `patch`.

A PR is internal/development-facing if someone who only runs the built binary
would never notice it — e.g. devcontainer/sandbox tooling, internal eval
harnesses, CI/workflow changes, internal test infrastructure, or refactors with
no user-visible behavior change. Do not downgrade PRs that change CLI behavior,
flags, output, or workflows shipped in the binary, or that fix a user-facing
bug, even if the PR touches internal code to get there.

Never move a PR up a category (e.g. patch -> minor) — the prefix-based
classification already captures the strongest signal for that direction; this
step only corrects false positives in `major`/`minor`.

After reclassifying, recompute `new_version` from `current_version` using the
corrected buckets: major bump if any PR remains in `major`, else minor bump if
any remains in `minor`, else patch bump.

When you downgrade a PR, note it in the summary (see step 3) so the user can
override the call if they disagree.

### 3. Show the release summary

Display:

- Version bump: `current_version` -> `new_version` (using the corrected buckets
  from step 2)
- Number of PRs by category, after reclassification
- Each PR number and title, grouped by Major / Minor / Patch
- Any PRs downgraded in step 2, with a one-line reason
- When a current branch PR is included, confirm it is the open PR for the
  current branch, not an already-merged predecessor.

### 4. Write changelog descriptions

For each PR in the JSON output, write a one-sentence description that is more
informative than the raw PR title. Strip the conventional commit prefix from
the title and keep enough context to understand the change without clicking
through.

### 5. Format the changelog section

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

### 6. Write the changelog section to a temp file

```bash
TMPFILE=$(mktemp)
cat <<'CHANGELOG_EOF' > "$TMPFILE"
<paste the formatted changelog section here>
CHANGELOG_EOF
echo "$TMPFILE"
```

### 7. Create the release PR

Run:

```bash
bash .claude/skills/release/scripts/create-release-pr.sh <new_version> <tmpfile_path>
```

When run on `main`, the script creates `release/v<new_version>` and opens a
release PR. When run on another branch, it commits the release changes directly
to that branch and prints its PR URL.

### 8. Report

Print the PR URL returned by the script, then clean up:

```bash
rm -f "$TMPFILE"
```
