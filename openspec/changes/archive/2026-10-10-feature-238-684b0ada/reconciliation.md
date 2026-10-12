# Reconciliation record

This file records how each old delta was handled. The code on `main` is the ground truth. **Reconciliation baseline:** `main` at `629bd3d4eede21f828fb79dfd3b3f506e628959f` (2026-10-09). Every delta copies main-spec text from this revision; see design.md step 4 for the drift check. Classifications:
- **APPLY**: written into this change's deltas, edited to match the code.
- **REFLECTED**: the main spec already says it.
- **SUPERSEDED**: newer specs or code replaced it.
- **UNIMPLEMENTED**: the code doesn't do it, so it was omitted.
- **INAPPLICABLE**: it targets a capability that no longer exists.

## start-run

All three tasks are implemented on `main`: `internal/discovery`, `internal/listview/newtab.go`, `internal/runview` (definition view), `internal/paramform`, and the launch wiring in `cmd/agent-runner/main.go`.

| Old capability | Requirement | Class | Destination |
|---|---|---|---|
| list-runs | Tab navigation includes new tab | APPLY, corrected: the Worktrees tab exists only inside a git repo | list-runs ADDED |
| list-runs | Default tab on entry | APPLY, corrected: `--list` and `--resume` open Current Dir | list-runs ADDED |
| list-runs | New tab workflow list rendering | SUPERSEDED by `new-tab-layout`; only the row-content part applied | new-tab-layout ADDED "New tab workflow row content" |
| list-runs | New tab keybindings | APPLY | new-tab-layout ADDED |
| list-runs | New tab search filter | APPLY, corrected: matches name and description, not path; underline styling; Escape behavior | new-tab-layout ADDED |
| (main) new-tab-layout | Workflow groups render with header and description | SUPERSEDED by the "Plan with an agent" entry (`internal/listview/newtab.go:116`; tests `TestNewTab_InitialCursorSkipsLeadingHeader`, `TestNewTab_NavigationAroundIntakeEntry`). The entry conflicts with this change's search-focus rule | REMOVED; ADDED "Workflow group headers" and "Plan with an agent entry" |
| view-run | Run-view entry points (MODIFIED) | APPLY, corrected: three entry points, no direct-by-name entry | view-run MODIFIED (also "Exit behavior"; list-runs "Open run from TUI") |
| view-run | Post-run Escape navigates to list | APPLY, corrected; the clause "run visible in current-dir list" is UNIMPLEMENTED (follow-up 1) | view-run ADDED |
| workflow-definition-view | Workflow definition view mode; Start run from definition view | APPLY, moved: `view-run` already owns the definition preview | view-run ADDED |
| workflow-discovery | 4 requirements | APPLY, corrected: covers versioning and invalid-file entries | workflow-discovery (new) |
| workflow-param-form | 4 requirements | APPLY, corrected: underline focus, whitespace counts as missing | workflow-param-form (new) |

## make-pty-great

Commit `d7724a12` had already hand-edited most of these into the main specs, under new requirement names in some cases.

| Capability | Requirement | Class | Destination |
|---|---|---|---|
| interactive-terminal-handoff | Direct terminal inheritance; TUI release and restore; Graceful termination; No terminal transcript | REFLECTED | — |
| interactive-terminal-handoff | Foreground process group ownership | REFLECTED; its job-control half is SUPERSEDED by `7e10b32a` (SIGTTIN/SIGTTOU recovery) | MODIFIED "Full suspension forwarding" |
| interactive-terminal-handoff | Natural exit and crash handling | APPLY: exit without completion → `aborted`; supervision error → `failed` | ADDED |
| interactive-terminal-handoff | Runner crash does not orphan the CLI | APPLY, gap: resume cleans up stale endpoints and survivors | MODIFIED (existing "…orphan the child") |
| interactive-shell-steps | Direct terminal execution with TUI suspend and resume | REFLECTED under new names | — |
| live-run-view | Interactive agent steps suspend the TUI | REFLECTED | — |
| workflow-execution | Agent step execution dispatch | REFLECTED | — |
| audit-log-entries | Event types | REFLECTED. Main's list was corrected to the event types production code writes, as defined in `internal/audit/types.go`, with one deliberate omission: `nested_agent_end`. Its only emitter was removed in `4f04880b` (2026-09-08). Today `internal/metrics/collector.go` only reads it, from audit logs written between 2026-08-28 and 2026-09-08. Whether the list should also name read-only legacy types is an open question in the acceptance ledger | MODIFIED |
| cli-adapter | No permission loosening in interactive mode; Adapters honor autonomous permission mode | Not written: PR #232 owns both (see follow-ups 6 and 7) | — |
| cli-adapter | Capture forces autonomous-headless | REFLECTED | — |
| step-control-channel | Per-run control endpoint | REFLECTED (privacy scenario added) | MODIFIED "Private per-run control endpoint" |
| step-control-channel | Per-step completion credential | REFLECTED | merged into ADDED "Fresh authenticated attempt credential" |
| step-control-channel | Completion event semantics; Completion instruction injection | APPLY | ADDED |
| step-control-channel | Universal completion surface | APPLY, narrowed to adapters that support interactive invocation. OpenCode rejects interactive steps before spawn (`internal/cli/opencode.go:156-165`, `internal/exec/agent.go:484-493`), so it is not listed among the pre-approving adapters. Partly UNIMPLEMENTED (follow-up 3) | ADDED |
| step-control-channel | Completion ack and turn durability; In-session completion command | REFLECTED | — |
| pseudo-terminal, agent-continue-trigger | 5 REMOVED entries | INAPPLICABLE: specs already deleted | — |

## call-agent-skill and async-mcp

The code differs from the async-mcp design. `call_agent` waits for the child with no time limit by default. Only a Cursor parent gets a 45-second wait limit and then polls with `get_agent_call` (`internal/exec/agent_call.go:101-116`). The specs follow the code.

| Capability | Requirement(s) | Class | Destination |
|---|---|---|---|
| agent-calls | Tool availability; Acceptance boundary; Synchronous execution; Long-running MCP execution; Call safety; Results and failures | APPLY, edited; main scenario names kept with corrected bodies | MODIFIED |
| agent-calls | Job identity; Status retrieval; Uncollected calls fail parent; Explicit cancellation | APPLY | ADDED |
| agent-calls | Parent session discovery excludes called children | APPLY as ADDED (absent from main), split in two. The Runner collects and forwards child IDs for every adapter (`internal/exec/invocation.go:181-194`). Exclusion and failing closed on multiple matches are scoped to Cursor (`internal/cli/cursor.go`), the only adapter that honors them. Other adapters are UNIMPLEMENTED (follow-up 4) | ADDED |
| agent-calls | Cancellation propagation | SUPERSEDED: the child is leased to the parent attempt, not to the MCP request | REMOVED; ADDED "Agent-call cancellation propagation" |
| step-control-channel | Private per-run control endpoint (both changes) | APPLY: all three tools plus `AGENT_RUNNER_ATTEMPT_ID` | MODIFIED |
| step-control-channel | Fresh authenticated attempt | SUPERSEDED: a lost client no longer cancels the child (`internal/control/control.go:867-869`) | REMOVED; ADDED "Fresh authenticated attempt credential" |
| cli-adapter | Agent-call tool provisioning; Long-running tool controls | APPLY, corrected: timeout control is MAY (only Codex and Copilot) | MODIFIED |
| call-agent-skill | 8 requirements | APPLY, written from the current `SKILL.md` in Codagent-AI/agent-skills | call-agent-skill (new) |
| cursor-cli-support | Interactive session ID discovery; agent-call wait budget | APPLY | ADDED |
| step-model | Static Runner-owned tools; Tools field is agent-only | APPLY, corrected: `submit_route` is also accepted, reserved for intake | ADDED |
| builtin-workflows | Call-capable OpenSpec v2 steps declare and use call_agent | SUPERSEDED by the `workflows/core/*` layout | ADDED corrected "Call-capable builtin steps declare and use call_agent" |

## repair-development-audit-recovery

| Capability | Requirement | Class | Destination |
|---|---|---|---|
| automatic-run-audit | Reserved launches can be explicitly reconciled | REFLECTED (identical) | — |
| lightweight-audit-reporting | Pending delivery failures remain independently durable | APPLY, merged into the existing requirement | MODIFIED "Reporting failure is local and non-blocking" |
| lightweight-audit-reporting | Unsafe optional notes are omitted | APPLY, mostly reflected already. The note checks are rewritten as the deterministic checks the code applies (`internal/devaudit/value_audit.go:1059-1073`); semantic filtering of evidence excerpts or transcript-like notes is UNIMPLEMENTED and rests on the judge prompt | MODIFIED "External rows are an allowlisted high-level projection" |
| lightweight-audit-reporting | Audit issue bodies are durable and repairable | APPLY | ADDED |

## testing-evidence-fixes

| Capability | Requirement | Class | Destination |
|---|---|---|---|
| cli-adapter | Headless Claude runs without background tasks | APPLY (`internal/cli/claude.go:170-182`) | ADDED |

## make-evals-great

The folder holds only `.openspec.yaml`: no artifacts and no deltas. It is archived as is.

## Retired main-spec scenarios

- **step-control-channel, "Lost agent-call client cancels leased execution":** contradicts the code. Retired by replacing "Fresh authenticated attempt".
- **agent-calls, "MCP cancellation releases the call slot" and "Bridge connection loss cancels the child":** contradict the code. Retired by replacing "Cancellation propagation".
- **new-tab-layout, "Initial cursor position skips the leading header" and "Upward navigation from the first workflow focuses the search box":** contradict the code. Retired by replacing "Workflow groups render with header and description"; the corrected behavior is in "Plan with an agent entry" and "Upward navigation from the first workflow skips the leading header".

## Follow-ups (not fixed here: spec-only change)

1. After Escape on a finished run, the agent-runner process changes directory into the run's storage directory before relaunching. The relaunched list's Current Dir tab is then empty and the run is missing (`cmd/agent-runner/main.go:1588-1592`, `internal/listview/model.go:192-201`).
2. Typing `q` in the New tab search box or a param form field quits the app (`cmd/agent-runner/main.go:1747-1750`).
3. Autonomous-interactive completion on Copilot, and possibly Codex, can wait for human approval of `step complete`: there is no exact pre-approval and no fail-early check.
4. Parent session discovery ignores called-child exclusions on Copilot, interactive Codex without a preset ID, and interactive OpenCode.
5. The loader accepts `submit_route` inside nested loop or group steps. It is never provided to the agent there.
6. After PR #232 merges, the cli-adapter requirement "No permission loosening in interactive mode" still needs two things:
   - The scenarios "Exact completion command runs without prompting" and "Unrelated commands retain normal permission behavior".
   - A note that an exact `step submit-route` command is also pre-approved (`internal/cli/adapter.go:127-145`).
7. Main-spec mismatches seen outside this reconciliation's scope:
   - `view-run` "r is ignored on failed run" (the code resumes failed runs).
   - The `lightweight-audit-reporting` note checks are only deterministic. The spec now states this; broader semantic filtering would be new work.
8. The new main specs `call-agent-skill`, `workflow-discovery`, and `workflow-param-form` get `Purpose: TBD` when archived, like 26 existing main specs. Filling in a purpose is optional cleanup.
