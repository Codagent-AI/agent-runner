## Why

When an agent-runner workflow fails today, the user is left to fend for themselves: the run view shows a failure reason string but offers no diagnostic action, and the onboarding workflow drops users silently back on the home screen if it fails — masking what is almost always a real bug we should hear about. Real users do not have the source tree, the audit log path, or the workflow YAML at their fingertips, so even motivated users cannot easily file an actionable issue. We need a built-in, agent-driven debug path that turns a failure into either a fix or a high-quality GitHub issue, accessible from every place a user encounters one.

## What Changes

- **New built-in workflow** `workflows/core/debug.yaml` — an interactive agent workflow whose job is to triage a failed run, decide between (a) root-cause identified / user-fixable, (b) root-cause identified / suspected bug → file issue, or (c) unknown → file issue with collected evidence.
- **Input shape**: workflow accepts either a `failed_session_dir` or a `failed_run_id` parameter. When neither is supplied (e.g., launched from main menu cold), the agent prompts the user to pick from recent failed runs or paste a path.
- **Bundled debugging playbook**: the workflow bundles a markdown playbook that tells the agent exactly where to find the audit log (`<sessionDir>/audit.log`, JSONL), the run state (`state.json` → `workflowFile`), how to read built-in workflow YAML, common failure modes, and the GitHub issue template URL format. This replaces the dev-mode assumption that the agent can grep the repo.
- **New read-only CLI inspection helpers** — a single `debug` subcommand with three operation flags that the debug agent uses to gather context in a real install where the source tree is absent:
  - `agent-runner debug --show-workflow <ref>` — prints the resolved YAML for any workflow reference, including `builtin:` (read from embedded FS) and on-disk paths. Returns the named ref's YAML only; the agent walks composition by issuing additional calls.
  - `agent-runner debug --state <run-id>` — prints the run's `state.json` contents (the recorded `workflowFile`, params, step pointer, status, etc.). Stable JSON output the agent can parse.
  - `agent-runner debug --audit-summary <run-id>` — parses `<sessionDir>/audit.log` and returns a compact summary (step boundaries, errors, run start/end, sub-workflow boundaries) plus the absolute path to the full audit log. Bounded output size; if the agent needs more detail (specific event payloads, raw output capture), it `grep`s/`tail`s the file at the returned path. This avoids dumping a potentially multi-megabyte audit log into the agent's context.
- **GitHub issue reporting**: the agent uses the `gh` CLI for both duplicate-search and issue creation. Before assembling a new issue, the agent runs `gh auth status` to confirm `gh` is installed and authenticated; on success, it uses `gh issue list --search ... --state open` to find similar open issues in the agent-runner repo and presents matches to the user with three explicit choices (open a match, file new, cancel). If the user proceeds with a new issue, the agent assembles a body (failure reason, redacted audit excerpt, workflow ref + version, agent-runner version, OS/CLI versions) and submits via `gh issue create --title ... --body ...`. **Fallback** when `gh` is missing or unauthenticated: the agent prints the proposed title + body in chat in a copy-friendly format plus the manual-create URL `https://github.com/<org>/agent-runner/issues/new` for the user to paste into. `gh` is a documented soft dependency; no repo-side issue template is required for this change (one can be added independently later).
- **Version / OS / CLI metadata collection**: the bundled playbook instructs the agent to obtain agent-runner version via `agent-runner --version`, OS via `uname -a` (or platform equivalent), and external agent CLI versions via the CLI's own `--version`. No new helper command is required for metadata; if the spec phase finds these insufficient, a `agent-runner doctor`-style helper can be added then.
- **Main menu entry**: surface the debug workflow as a standard discovered builtin in the new-workflow tab (no new "system entry" concept needed) so users can reach it without an active failure. The TUI may visually highlight or pin it, but it is launched the same way as any other builtin.
- **Run view shortcut**: add a `d` keybinding on the run-view failure surface that launches the debug workflow pre-filled with the current run's id.
- **Onboarding failure modal**: when the onboarding workflow run reaches a terminal failure status (a step errored or the run otherwise ended in a non-success state), intercept the return-to-home and show a full-screen modal: "Onboarding failed unexpectedly" + failure reason + two actions: **Debug now** (launches debug workflow pre-filled with the failed session dir) or **Skip** (proceeds to home). The modal does NOT fire on user cancellation (Ctrl-C / ESC) or on a clean `break_if`-driven early exit — only on actual failure.
- **Auto-resume on confirmed fix**: when triage outcome is "user-fixable" AND the user confirms they have applied the fix AND the user explicitly confirms a resume prompt, the debug workflow SHALL end with a *resume-handoff* signal carrying the failed run's id. Agent-runner SHALL then exec `agent-runner --resume <failed_run_id>` in place (the same in-place-exec pattern the run-view `r` keybinding already uses). On bug or unknown outcomes, no resume offer is shown. The wire format is a per-session marker file at `<sessionDir>/resume-target` (resolved during design).

## Capabilities

### New Capabilities

- `workflow-debugger`: The debug workflow itself — its inputs (session dir or run id, with interactive fallback), the bundled debugging playbook the agent is given, the triage decision tree (user error vs. suspected bug vs. unknown), and the GitHub issue assembly + prefilled-URL flow.
- `failure-debug-entry-points`: The three trigger paths into the debug workflow — the main-menu entry, the run-view `d` keybinding on failed runs, and the onboarding-failure modal (including its trigger condition, layout, and the Debug/Skip actions).
- `workflow-resume-handoff`: A reusable runner-side contract — when a workflow signals a resume target (run id) during its run, agent-runner SHALL, on workflow completion, exec `agent-runner --resume <run-id>` in place instead of returning to the launching screen. Used by the debug workflow today; mechanism is general. Signal wire format is a design decision.
- `debug-inspection-cli`: A small family of read-only CLI commands the debug agent uses to gather context without source-tree access — all exposed as flags on a single `debug` subcommand. Covers:
  - `debug --show-workflow <ref>` — resolves builtin and on-disk refs and prints the YAML for the *named ref only* to stdout. It does NOT recursively expand or inline composed sub-workflows; the agent walks composition references by issuing additional `--show-workflow` calls.
  - `debug --state <run-id>` — prints the run's `state.json` (workflow ref, params, step pointer, status) as stable JSON.
  - `debug --audit-summary <run-id>` — emits a bounded, structured summary of the run's audit log (step boundaries, errors, run start/end, sub-workflow boundaries) plus the absolute path to the full `audit.log` so the agent can grep/tail it for additional detail without ever pulling the full file into context. Bounded at 64 KB by default; pre-redacted for known secret patterns.

### Modified Capabilities

- `builtin-workflows`: extending the `core` namespace's enumerated "at minimum" workflow list to include `debug`. The run-view `d` binding behavior and the onboarding-failure modal trigger are fully captured by the new `failure-debug-entry-points` capability and do not require deltas to `view-run`, `live-run-view`, or the home TUI specs (matching the pattern of `splash-modal`, which is self-contained and does not amend the home-screen tab layout).

No changes expected to `audit-log-*` specs — the debug workflow consumes the audit log as it exists today.

## Technical Approach

### Architecture sketch

```
                ┌─────────────────────────────────────┐
                │            agent-runner TUI         │
                │                                     │
   main menu ──►│  [Debug a failed run / report …]   │──┐
                │                                     │  │
   run view  ──►│  failed run: press 'd'              │──┤
                │                                     │  │
   onboarding ─►│  ┌─────────────────────────────┐   │  │
   fails       │  │ "Onboarding failed"         │   │  │
                │  │  [Debug now] [Skip]         │──┤  │
                │  └─────────────────────────────┘   │  │
                └─────────────────────────────────────┘  │
                                                         ▼
                              params: failed_session_dir or failed_run_id
                                                         │
                                                         ▼
                       ┌────────────────────────────────────────────────┐
                       │   builtin workflow: workflows/core/debug.yaml  │
                       │                                                │
                       │  step 1 (optional): resolve run / prompt user │
                       │  step 2: interactive agent (planner)          │
                       │     • bundled docs/playbook                   │
                       │     • shells `agent-runner run state …`       │
                       │     • shells `agent-runner run audit-summary` │
                       │     • shells `agent-runner workflow show …`   │
                       │     • greps full audit.log at returned path   │
                       │     • triages → fix | report | unknown        │
                       │     • submits issue via `gh` (or prints body  │
                       │       + manual URL when `gh` is unavailable)  │
                       └────────────────────────────────────────────────┘
```

### Key technical decisions

1. **One new workflow, not an enhancement of `help.yaml`.** Debugging a failure is a focused intent (triage → fix-or-report) with a different bundle of docs, a parameter contract (`failed_session_dir` / `failed_run_id`), and three programmatic entry points. Keeping it separate from general Q&A keeps both prompts focused.

2. **CLI inspection helpers over file bundling for run/workflow context.** Real installs don't have the source tree, and recursively bundling the full workflow graph plus run state plus audit log at launch time duplicates content and risks blowing the agent's context with a large `audit.log`. Instead, three small read-only commands (`workflow show`, `run state`, `run audit-summary`) let the agent pull exactly what it needs, with the audit-summary returning a bounded structured view + a path the agent can `grep` for deeper investigation. Reusable for future debug-style agents.`

3. **`gh` CLI for GitHub interaction, with graceful manual fallback.** Using `gh` for both duplicate-search (`gh issue list --search`) and creation (`gh issue create`) is uniform, handles auth correctly, and gives the user a proper issue URL back. `gh` becomes a documented soft dependency; when missing or unauthenticated, the agent falls back to printing the title + body + manual-create URL for the user to paste into the GitHub web form. This avoids shipping a per-OS browser opener or a Go-side HTTP client.

4. **Modal for onboarding, keybinding for run view.** Onboarding failure is a discoverability problem — users don't yet know agent-runner's affordances, so a modal is needed to surface the path. Run-view failure is a flow problem — users already know the run-view UI, so a keybinding (with a help-bar hint) is enough.

5. **Input parameter contract: session dir OR run id.** Two parameters, both optional. Run view passes the id (it has it cheaply); onboarding modal passes the session dir (it has that path directly). Main menu passes neither; the workflow's first step interactively resolves.

### Risk areas

- **`gh` CLI availability**: `gh` is a soft dependency. When missing or unauthenticated (`gh auth status` non-zero), the agent falls back to printing the title + body + manual-create URL. Browser opening of matched existing issues is delegated to `gh issue view --web`, which handles per-OS opener concerns internally.
- **Redaction of audit-log secrets**: audit events may include prompts or env that contain tokens. Redaction is applied programmatically in `audit-summary` (regex pattern set replaced with `<REDACTED>`) and the playbook also instructs the agent to summarize/redact further when composing any issue body. Defense in depth.
- **Triggering a workflow from inside the TUI from non-discovery paths**: existing menu-launch flows go through `discovery.StartRunMsg`. The onboarding-failure modal Debug-now action uses `StartRunMsg`; the run-view `d` keybinding emits a separate `LaunchDebugMsg` that `main.go` handles by `syscall.Exec`-replacing the current process (mirroring the existing `ResumeRunMsg` exec pattern).

## Out of Scope

- Automatic issue submission (without user review). The agent always presents the proposed title and body and waits for explicit confirmation; only then does `gh issue create` run (or the fallback URL get printed).
- A general "telemetry" or crash-reporter system. This change is interactive-only.
- Changes to audit-log format or coverage. The debug workflow consumes today's audit log as-is.
- An in-TUI rich issue-editor. Issue body is assembled by the agent in its chat surface; the final review and submit happens via `gh issue create` (or the manual web form on fallback).

## Impact

- **New files**:
  - `workflows/core/debug.yaml`
  - `workflows/core/debug/bundled/docs/playbook.md` (bundled playbook markdown)
  - `cmd/agent-runner/debug_cmd.go` (the `debug` subcommand router and the three op handlers)
  - `internal/audit/summary.go`, `internal/audit/redact.go` (audit-summary builder + redaction pattern set)
  - `internal/resumehandoff/` (per-session marker-file helpers used by the runner-side post-workflow handler)
- **Modified packages**:
  - `internal/listview/` — onboarding-failure modal screen (mirrors splash-modal pattern); `core:debug` appears in the new-workflow tab via the existing discovery mechanism (no special entry)
  - `internal/runview/` — `d` keybinding on inactive runs (any non-active status), help-bar hint, `LaunchDebugMsg` emission
  - `cmd/agent-runner/main.go` — `debug` subcommand routing before existing flag dispatch; `LaunchDebugMsg` exec-replacement handler; post-workflow resume-handoff marker check; `--onboarding-from` failure routed into the home TUI with `WithOnboardingFailure(...)`
- **No new external Go dependencies**. `gh` CLI is a documented soft dependency invoked via `os/exec`.
- **No changes** to audit, state.json, or step-executor packages.
- **Docs**: a short `docs/` page for users describing the three entry points, what data the debug agent collects (privacy-relevant), and the `gh` CLI soft dependency.
