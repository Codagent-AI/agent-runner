## Why

When an agent-runner workflow fails today, the user is left to fend for themselves: the run view shows a failure reason string but offers no diagnostic action, and the onboarding workflow drops users silently back on the home screen if it fails — masking what is almost always a real bug we should hear about. Real users do not have the source tree, the audit log path, or the workflow YAML at their fingertips, so even motivated users cannot easily file an actionable issue. We need a built-in, agent-driven debug path that turns a failure into either a fix or a high-quality GitHub issue, accessible from every place a user encounters one.

## What Changes

- **New built-in workflow** `workflows/core/debug.yaml` — an interactive agent workflow whose job is to triage a failed run, decide between (a) root-cause identified / user-fixable, (b) root-cause identified / suspected bug → file issue, or (c) unknown → file issue with collected evidence.
- **Input shape**: workflow accepts either a `failed_session_dir` or a `failed_run_id` parameter. When neither is supplied (e.g., launched from main menu cold), the agent prompts the user to pick from recent failed runs or paste a path.
- **Bundled debugging playbook**: the workflow bundles a markdown playbook that tells the agent exactly where to find the audit log (`<sessionDir>/audit.log`, JSONL), the run state (`state.json` → `workflowFile`), how to read built-in workflow YAML, common failure modes, and the GitHub issue template URL format. This replaces the dev-mode assumption that the agent can grep the repo.
- **New CLI helper** `agent-runner workflow show <ref>` — prints the resolved YAML for any workflow reference, including `builtin:` (read from embedded FS) and on-disk paths. This is how the debug agent inspects the failing workflow's source in a real install.
- **GitHub issue reporting**: agent assembles a prefilled issue body (failure reason, redacted audit excerpt, workflow ref + version, agent-runner version, OS/CLI versions) and either opens the prefilled URL in the user's browser via the OS opener, or prints it for copy/paste if no browser is available. URL uses the generic `https://github.com/<org>/agent-runner/issues/new?title=...&body=...` form — no repo-side issue template is required for this change; one can be added independently later. No auth, no `gh` dependency.
- **Version / OS / CLI metadata collection**: the bundled playbook instructs the agent to obtain agent-runner version via `agent-runner --version`, OS via `uname -a` (or platform equivalent), and external agent CLI versions via the CLI's own `--version`. No new helper command is required for metadata; if the spec phase finds these insufficient, a `agent-runner doctor`-style helper can be added then.
- **Main menu entry**: surface the debug workflow as a standard discovered builtin in the new-workflow tab (no new "system entry" concept needed) so users can reach it without an active failure. The TUI may visually highlight or pin it, but it is launched the same way as any other builtin.
- **Run view shortcut**: add a `d` keybinding on the run-view failure surface that launches the debug workflow pre-filled with the current run's id.
- **Onboarding failure modal**: when the onboarding workflow run reaches a terminal failure status (a step errored or the run otherwise ended in a non-success state), intercept the return-to-home and show a full-screen modal: "Onboarding failed unexpectedly" + failure reason + two actions: **Debug now** (launches debug workflow pre-filled with the failed session dir) or **Skip** (proceeds to home). The modal does NOT fire on user cancellation (Ctrl-C / ESC) or on a clean `break_if`-driven early exit — only on actual failure.

## Capabilities

### New Capabilities

- `workflow-debugger`: The debug workflow itself — its inputs (session dir or run id, with interactive fallback), the bundled debugging playbook the agent is given, the triage decision tree (user error vs. suspected bug vs. unknown), and the GitHub issue assembly + prefilled-URL flow.
- `failure-debug-entry-points`: The three trigger paths into the debug workflow — the main-menu entry, the run-view `d` keybinding on failed runs, and the onboarding-failure modal (including its trigger condition, layout, and the Debug/Skip actions).
- `workflow-source-inspection`: The new `agent-runner workflow show <ref>` CLI command — resolves builtin and on-disk refs and prints the YAML for the *named ref only* to stdout (it does NOT recursively expand or inline composed sub-workflows; the agent walks composition references by issuing additional `workflow show` calls). Behavior on missing refs, on circular composition (not applicable to a single-ref read but relevant to whether the command ever follows references), and on exact output format (raw bytes vs. normalized) are open items to resolve in the spec.

### Modified Capabilities

The following existing specs are likely to need deltas — exact scope to be confirmed during the spec phase:

- `live-run-view` and/or `view-run`: adding the `d` keybinding and surfacing it in the help bar when the focused run is in a failed state.
- `builtin-workflows`: registering `core/debug.yaml` as a standard discoverable builtin. No change to the discovery/launch contract is expected — the run-view shortcut and onboarding-failure modal launch it via the same `discovery.StartRunMsg` path with input params attached.

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
                       │     • reads audit.log, state.json             │
                       │     • shells `agent-runner workflow show …`   │
                       │     • triages → fix | report | unknown        │
                       │     • opens prefilled GH issue URL via opener │
                       └────────────────────────────────────────────────┘
```

### Key technical decisions

1. **One new workflow, not an enhancement of `help.yaml`.** Debugging a failure is a focused intent (triage → fix-or-report) with a different bundle of docs, a parameter contract (`failed_session_dir` / `failed_run_id`), and three programmatic entry points. Keeping it separate from general Q&A keeps both prompts focused.

2. **CLI helper over file bundling for workflow source access.** Real installs don't have the source tree, and recursively bundling the full workflow graph at launch time duplicates content and requires the runner to know the full composition upfront. A `agent-runner workflow show <ref>` command is small, reusable for future agents, and lets the debug agent walk references on demand.

3. **Prefilled GitHub issue URL, not `gh` CLI integration.** No new dependency, no auth flow, no failure mode if `gh` is missing. The agent uses the OS opener (or falls back to printing the URL). Issue body is assembled by the agent from audit + state, with the user reviewing before submitting.

4. **Modal for onboarding, keybinding for run view.** Onboarding failure is a discoverability problem — users don't yet know agent-runner's affordances, so a modal is needed to surface the path. Run-view failure is a flow problem — users already know the run-view UI, so a keybinding (with a help-bar hint) is enough.

5. **Input parameter contract: session dir OR run id.** Two parameters, both optional. Run view passes the id (it has it cheaply); onboarding modal passes the session dir (it has that path directly). Main menu passes neither; the workflow's first step interactively resolves.

### Risk areas

- **Browser-opener portability**: macOS `open`, Linux `xdg-open`, headless-server fallback. Needs a small abstraction.
- **Redaction of audit-log secrets**: audit events may include prompts or env that contain tokens. The playbook must instruct the agent to summarize/redact rather than paste verbatim. **Open decision for the spec phase (must be resolved before tasks, not deferred to implementation)**: whether to add a programmatic redaction pass before the agent ever sees the log, or to rely solely on prompt-side guidance. Privacy-relevant — the choice constrains both the workflow's pre-processing step and the GitHub-issue-body assembly.
- **Triggering a workflow from inside the TUI from non-discovery paths**: existing menu-launch flows go through `discovery.StartRunMsg`. Onboarding-failure-modal and run-view `d` keybinding need to emit the same message with a `builtin:core/debug.yaml` ref and the input params attached.

## Out of Scope

- Automatic issue submission (without user review). The agent always opens the prefilled URL or prints it; the human submits.
- Auto-retry of the failed workflow after a fix suggestion. The debug workflow ends after presenting a fix or filing an issue; the user re-runs manually.
- A general "telemetry" or crash-reporter system. This change is interactive-only.
- Diagnostic UI for *successful* runs (e.g., "explain this run"). Trigger paths are failure-scoped.
- Changes to audit-log format or coverage. The debug workflow consumes today's audit log as-is.
- An in-TUI rich issue-editor. Issue body is assembled by the agent in its chat surface; the GitHub web form is where the user edits and submits.

## Impact

- **New files**:
  - `workflows/core/debug.yaml`
  - `workflows/core/debug/docs/` (bundled playbook markdown)
  - new CLI subcommand under `cmd/agent-runner/` for `workflow show`
- **Modified packages**:
  - `internal/listview/` — main-menu entry and the onboarding-failure modal screen
  - `internal/runview/` — `d` keybinding on failed runs, help-bar hint, message wiring to launch debug workflow
  - `cmd/agent-runner/` — `workflow show` subcommand wiring
  - `internal/discovery/` or equivalent — resolve `builtin:` refs to YAML bytes for the `workflow show` command (read path may already exist; otherwise a small helper)
  - main entry point — detect onboarding failure and route to the modal instead of dropping to home
- **No new external dependencies**. Browser opener is `os/exec` over `open` / `xdg-open` / `cmd /c start`.
- **No changes** to audit, state.json, or step-executor packages.
- **Docs**: a short `docs/` page for users describing the three entry points and what data the debug agent collects (privacy-relevant).
