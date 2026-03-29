## Context

The runner currently scatters execution artifacts — state files go to the working directory (or engine-specified path), audit logs go to `~/.agent-runner/projects/{encoded-cwd}/logs/`. The CLI uses Cobra with three subcommands (`run`, `resume`, `validate`) despite being a simple tool. Resuming requires passing an explicit state file path. Additionally, the audit logger's `cwd` parameter is passed as an empty string, losing project context in log paths.

## Goals / Non-Goals

**Goals:**
- Unify state and audit files under `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/`
- Flatten CLI to a single command with `--resume`, `--session`, and `--validate` flags
- Drop Cobra in favor of Go's `flag` stdlib
- Fix the audit logger `cwd` bug (currently passed empty string)

**Non-Goals:**
- Migrating existing state files from old locations
- Session cleanup / garbage collection
- Changing how resume works internally (step resumption logic stays the same)

## Decisions

**1. Drop Cobra, use `flag` stdlib.**
The CLI becomes a single command: `agent-runner [--resume] [--session <id>] [--validate] <workflow.yaml> [params...]`. Go's `flag` package handles the three flags. Flags must precede positional args (Go `flag` convention). Positional args (workflow file + params) are parsed from `flag.Args()` after flag parsing. In resume mode (`--resume`), the workflow file is not required — the saved state contains the workflow path.

**2. `--resume` is a boolean flag, `--session` is a separate optional string flag.**
`--resume` alone means "resume most recent session for this project directory". `--resume --session <id>` resumes a specific session. `--session` without `--resume` is an error. `--validate` and `--resume` are mutually exclusive. Resume mode takes no positional arguments — the workflow file and params are restored from the saved state.

**3. Session directory replaces all state/audit path resolution.**
`initRunState()` creates the session directory at `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/` and passes that path to both `stateio.WriteState()` and `audit.NewLogger()`. The engine override chain (`GetStateDir()`, `opts.StateDir`) is removed. `resolveStateDir()` is deleted.

**4. `CreateLogger` takes the session directory path directly.**
Instead of constructing its own path, the audit logger receives the session directory. `CreateLogger(workflowName, cwd)` is replaced with `NewLogger(filepath.Join(sessionDir, "audit.log"))` — the existing `NewLogger` function already works for this. `CreateLogger`, `encodePath`, and `sanitizeWorkflowName` move out of the audit package into a shared location since session directory creation needs them.

**5. Resume scans for most recent `state.json` by modification time.**
`--resume` without `--session` scans `~/.agent-runner/projects/{encoded-cwd}/runs/*/state.json`, picks the most recently modified file, and passes it to the existing resume logic. `--resume --session <id>` looks for `runs/<id>/state.json` directly.

**6. `ResumeWorkflow` takes a state file path (unchanged internally).**
The resume logic itself doesn't change — it still reads a state file, restores context, and continues execution. The only change is how the state file path is resolved (from session directory instead of CLI argument).

## Risks / Trade-offs

**Risk: Session ID collisions.** If the same workflow starts twice in the same second, the session IDs collide. This is acceptable for now — the tool is interactive and single-user. If it becomes a problem later, append a short random suffix.

**Trade-off: Dropping Cobra removes auto-generated help.** We lose Cobra's help formatting, shell completions, and usage generation. For a single-command CLI with three flags this is fine — a manual `--help` / `-h` handler with `flag.Usage` is trivial.

**Trade-off: No migration.** Old state files are abandoned. Users with in-progress workflows will need to restart. Acceptable since this is a dev tool with no production state to preserve.

## Migration Plan

No data migration. Old `agent-runner-state.json` files in working directories are ignored. Old audit logs under `logs/` remain on disk but are no longer written to. The `resume` command is removed — users switch to `--resume`.

## Open Questions

None — all decisions are settled.
