---
title: External OpenSpec Projects
group: Configuration
order: 8
description: Keep an OpenSpec project outside the code repository.
---

# External OpenSpec Projects

The OpenSpec project can live in a separate directory, including a subdirectory of another repository. The spec root is the directory containing `openspec/`. It must already exist and contain an OpenSpec project.

## Configure a root

Add a mapping to your user settings at `~/.agent-runner/settings.yaml`:

```yaml
openspec_roots:
  /path/to/code-repository: ~/path/to/spec-repository/project
```

Use the absolute main-worktree path as the key. Linked Git worktrees use the same mapping. Symlinks are resolved, and non-string values are ignored. Keep this mapping in user settings, where other settings updates preserve it.

Override the setting for one invocation with a workflow parameter:

```bash
agent-runner run openspec:change change_name=add-export --param spec_root=/path/to/spec-project
```

The parameter wins over user settings; without either, the run uses its working directory. Relative parameters resolve against the launch directory, and `~/` expands to your home directory. The resolved context is saved in run state, so changing settings does not redirect a resumed run.

## Lifecycle behavior

Definition, planning, acceptance artifacts and archive operations use the spec project. Agents receive its existing `AGENTS.md`, `CLAUDE.md`, and `openspec/config.yaml` instruction paths. Implementation, code validation, commits and pull requests stay in the code repository. External creation rejects existing active or archived changes; planning and implementation require an active change.

Claude, Codex and Copilot receive additional workspace access through `--add-dir`. OpenCode already permits access; Cursor fails before starting because it cannot add a workspace directory.

Agent Runner leaves the spec repository uncommitted and reports what changed as the final step output: uncommitted files under the spec root when it is in a Git repository, otherwise the change's active or archive directory. Commit them separately. Agents are instructed to keep the spec-root path out of commit messages, checked-in files and PR text; Agent Runner does not scan for it, so review PR text before publishing.

The `openspec:change`, `openspec:simple-change`, `openspec:plan-change` and `openspec:implement-change` workflows support external roots. The external archive runs `openspec validate` and `openspec archive` in the spec root. If the change is already archived and its active directory is gone, a retried or resumed archive reports that and succeeds; if both directories exist, it fails. A direct call to `archive-change-v1.0` with only `change_name` keeps the repository-local archive and commit verification; pass `spec_root` and `spec_external: "true"` to archive in an external project.
