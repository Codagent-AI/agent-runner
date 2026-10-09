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

Agent Runner leaves the spec repository uncommitted and reports changed files as the final step output. Commit them separately. Commit messages, checked-in files and PR text must omit private spec paths; a closing guard detects leaks without rewriting history.

The latest OpenSpec workflows support external roots. Older exact-version references keep their existing behavior. Direct calls to `archive-change-v1.1` must pass the recorded `spec_root` and `spec_external` context when a user setting selects an external project. Archive retry and resume reuse the same run's transition record; a new run cannot adopt a pre-existing archive.
