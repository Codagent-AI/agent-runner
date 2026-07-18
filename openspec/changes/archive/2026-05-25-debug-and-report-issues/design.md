## Context

The `debug-and-report-issues` change adds a built-in debug workflow plus the runtime plumbing to launch it from three trigger points (home menu, run-view `d` key, onboarding-failure modal) and to optionally hand off to `agent-runner --resume` when the user has fixed their failed run.

The design has five settled spec files in `specs/`:

- `workflow-debugger` — the debug workflow's behavioral contract (triage, redaction, `gh` flow, auto-resume, continuation)
- `failure-debug-entry-points` — the three trigger paths and the onboarding-failure modal
- `debug-inspection-cli` — the three new read-only inspection commands
- `workflow-resume-handoff` — runner-side contract for a workflow signalling a resume target
- `builtin-workflows` (delta) — adds `debug` to the `core` namespace's enumerated list

This document captures the technical choices needed to implement the contract. It also identifies spec edits that should be applied alongside (because design discovered better mechanisms than the spec phase committed to).

Pre-existing assets that are reused:

- `internal/listview/` splash modal pattern (model fields + overlay + key handling) — template for the onboarding-failure modal.
- `agent-continue-trigger` PTY sentinel — keeps its role for ending interactive steps; **not** repurposed for the resume signal.
- `syscall.Exec` in-place process replacement used by run-view `r` and `enter` resume actions.
- `usersettings.OnboardingSettings.Dismissed` field already exists in the settings struct; no new settings field is added.
- `discovery.StartRunMsg` is the existing launch channel for in-TUI workflow starts; the home menu and the onboarding-failure modal's Debug-now action flow through it. The run-view `d` key emits a separate `LaunchDebugMsg` that `cmd/agent-runner/main.go` handles by `syscall.Exec`-replacing the current process (mirroring `ResumeRunMsg`), because the run view's lifecycle is already exec-replaced for its own `r`/`enter` resume actions and a `StartRunMsg` round-trip would conflict with that pattern.

## Goals / Non-Goals

**Goals:**

- Deliver the five spec contracts with minimal new abstractions.
- Reuse existing TUI/PTY/CLI patterns wherever they fit.
- Keep the runner's view of the debug workflow's outcomes as simple as possible (the agent's own conversation memory carries the triage outcome across steps; the runner only sees a one-line marker file when a resume is requested).
- Make every new CLI surface namespaced under a single `debug` subcommand so the existing flat flag CLI is left untouched.

**Non-Goals:**

- CLI restructuring beyond the new `debug` subcommand (no migration of `--list`/`--inspect`/`--validate`/`--version` to subcommands).
- New auth surface, telemetry, or crash-reporting infrastructure.
- An in-TUI rich issue editor — issue body assembly stays inside the agent's chat.
- A general "workflow output variables" system — the resume marker is a one-off convention, not a new primitive.

## Approach

### Component overview

**New files:**

| File | Role |
|---|---|
| `workflows/core/debug.yaml` | The debug workflow definition (three interactive steps). |
| `workflows/core/debug/bundled/docs/playbook.md` | Agent playbook with triage rubric, inspection-CLI usage, `gh` flow + fallback, redaction guidance, and resume-marker convention. |
| `cmd/agent-runner/debug_cmd.go` | `debug` subcommand router + handlers for `--state`, `--audit-summary`, `--show-workflow`. |
| `internal/audit/summary.go` | `BuildSummary(r io.Reader, capBytes int) (Summary, error)` — JSONL parser → bounded structured summary. |
| `internal/audit/redact.go` | Package-level regex pattern set + `Redact(s string) string`. |
| `internal/resumehandoff/` | Helpers: `MarkerPath(sessionDir)`, `Read(sessionDir) (runID string, ok bool, err error)`. |

**Modified files:**

| File | Change |
|---|---|
| `cmd/agent-runner/main.go` | Branch on `debug` subcommand before existing flag dispatch; on every workflow-end, check resume-handoff marker and (on success) exec `--resume <id>`; on `--onboarding-from` failure, launch home TUI with `WithOnboardingFailure(...)` instead of exiting. |
| `internal/listview/model.go` | New fields and key handling for the onboarding-failure modal. |
| `internal/listview/view.go` | Render onboarding-failure modal overlay (mirror splash). |
| `internal/listview/` (option file) | New `WithOnboardingFailure(sessionDir, reason string)` constructor option. |
| `internal/runview/model.go` | `d` key handler with gate `!running && !active`; emits `LaunchDebugMsg{FailedRunID}`. |
| `internal/runview/` (help bar) | Conditional `d debug` entry. |
| `workflows/core/_group.yaml` (if present) | No change required; `debug` is a workflow under `core/`. |

The `builtin-workflows` spec delta (adds `debug` to the `core` namespace's "at minimum" list) is satisfied purely by creating `workflows/core/debug.yaml` — the existing build-time embed in `workflows/embed.go` automatically registers any file under `workflows/<namespace>/`. No registration or discovery code changes.

### Workflow YAML — three interactive steps, same session

```yaml
name: debug
description: Debug a failed agent-runner run and optionally file an issue or resume.
params:
  - name: failed_run_id
    required: false
  - name: failed_session_dir
    required: false
steps:
  - id: triage
    mode: interactive
    agent: planner
    session: new
    prompt: |
      You are the debug agent. Read your playbook at:
        {{session_dir}}/bundled/core/debug/docs/playbook.md
      Follow the "Triage step" section.

      failed_run_id: {{failed_run_id}}
      failed_session_dir: {{failed_session_dir}}

      Conclude by classifying the failure as (a) user-fixable,
      (b) suspected agent-runner bug, or (c) unknown.

  - id: handle-issue
    mode: interactive
    agent: planner
    session: resume
    prompt: |
      You are still in the debug session. You just completed the triage
      step and concluded an outcome (a), (b), or (c).
      Follow the "Handle-issue step" section of your playbook at:
        {{session_dir}}/bundled/core/debug/docs/playbook.md
      If your outcome was (a) or the user declined to file an issue, emit
      the continue marker immediately and do not prompt.

  - id: handle-resume
    mode: interactive
    agent: planner
    session: resume
    prompt: |
      You are still in the debug session. Follow the "Handle-resume step"
      section of your playbook at:
        {{session_dir}}/bundled/core/debug/docs/playbook.md
      If outcome was (a) and the user confirmed both the fix application
      and the resume prompt, write the failed run id to:
        {{session_dir}}/resume-target
      Then ask "anything else?". If the user says no, emit the continue
      marker. If the user wants to continue, stay in this step and keep
      handling their requests until they signal done.
```

The agent's per-step memory carries the triage outcome between steps. The runner does not need to know the outcome.

### Data flow

```
┌──────────────────────────────────────────────────────────────────────┐
│ Trigger                                                              │
│   • home menu (core:debug)        → no params                        │
│   • run-view 'd' key              → LaunchDebugMsg{FailedRunID}      │
│   • onboarding-failure modal      → StartRunMsg{core:debug,          │
│      "Debug now"                     Params:{failed_session_dir}}    │
└──────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
   (home menu + onboarding modal: discovery.StartRunMsg → in-process run;
    run-view 'd': LaunchDebugMsg → main.go syscall.Exec replacement)
┌──────────────────────────────────────────────────────────────────────┐
│ Process A starts core:debug (in-process or via exec replacement)     │
└──────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│ Process B                                                            │
│   • creates <debugSessionDir>                                        │
│   • runs triage → handle-issue → handle-resume                       │
│     (single agent CLI session resumed across steps)                  │
│   • agent shells:                                                    │
│       agent-runner debug --state <id>                                │
│       agent-runner debug --audit-summary <id>                        │
│       agent-runner debug --show-workflow <ref>                       │
│     and optionally:                                                  │
│       gh auth status                                                 │
│       gh issue list --search ... --state open                        │
│       gh issue create --title ... --body ...                         │
│   • on (a)+fix+resume: agent writes <debugSessionDir>/resume-target  │
│                                                                      │
│   • all steps emit continue-trigger; workflow ends success           │
└──────────────────────────────────────────────────────────────────────┘
                              │
                              ▼   (post-workflow handler in main.go)
┌──────────────────────────────────────────────────────────────────────┐
│ Resume-handoff check                                                 │
│   if outcome == success                                              │
│     && data, err := os.ReadFile(<debugSessionDir>/resume-target)    │
│     && err == nil                                                    │
│     && runID = strings.TrimSpace(string(data))                       │
│     && <runID> session dir exists                                    │
│   then syscall.Exec("agent-runner", "--resume", runID)              │
│   else return to launching screen as for any other workflow          │
└──────────────────────────────────────────────────────────────────────┘
```

### Resume-handoff wire format

Marker file at `<sessionDir>/resume-target`. Content: a single line, the failed run's id, optionally trailing newline (trimmed on read).

- Agent writes via its file tool, e.g. `echo abc123 > {{session_dir}}/resume-target`.
- Runner reads with `os.ReadFile(filepath.Join(sessionDir, "resume-target"))` once, after the workflow run reaches terminal state.
- If the file is absent → no resume.
- If the file is present but the workflow ended in any non-success state → discard silently (per spec).
- If the file is present and the workflow ended success → trim whitespace, validate the run id resolves to a known session directory, and `syscall.Exec` `agent-runner --resume <id>` in place.
- If the contained id does not resolve → return to launching screen with an inline error naming the bad id.

The helper package `internal/resumehandoff/` exposes two functions for the runner side; the agent uses plain shell to write the file (no helper command needed).

### CLI: `agent-runner debug --<op> <arg>`

Three operations as flags inside a single `debug` subcommand:

```
agent-runner debug --state <run-id>            # prints <sessionDir>/state.json as JSON
agent-runner debug --audit-summary <run-id>    # prints bounded redacted JSON summary
agent-runner debug --show-workflow <ref>       # prints raw workflow YAML bytes
```

Dispatch lives in `cmd/agent-runner/debug_cmd.go`. `main.go`'s top-level argv check routes the first non-flag arg `debug` to the subcommand router before existing flag parsing. Each operation is a thin wrapper:

- `--state` → resolve `<sessionDir>` from run id → `stateio.ReadState` → `json.NewEncoder(os.Stdout).Encode(state)`.
- `--audit-summary` → resolve `<sessionDir>` → open `audit.log` (treating missing file as "0 events but path is still set") → `audit.BuildSummary(reader, defaultCapBytes)` → JSON-encode → stdout.
- `--show-workflow` → if ref starts with `builtin:` or matches `<ns>:<name>` form, use `builtinworkflows.ReadFile`; else `os.ReadFile`. Stream bytes to stdout verbatim.

All three exit 0 on success, non-zero with stderr message on error. None are TUI-launching, so they skip the existing TTY check.

### Audit summary builder

`internal/audit/summary.go`:

```go
type Summary struct {
    Path               string                  `json:"path"`
    RunStart           *EventRef               `json:"run_start,omitempty"`
    RunEnd             *EventRef               `json:"run_end,omitempty"`
    Steps              []StepBoundary          `json:"steps"`
    SubWorkflows       []SubWorkflowBoundary   `json:"sub_workflows"`
    Errors             []ErrorEvent            `json:"errors"`
    Truncated          bool                    `json:"truncated"`
    DroppedEventsCount int                     `json:"dropped_events_count"`
}

func BuildSummary(r io.Reader, capBytes int) (Summary, error)
```

Implementation walks JSONL line-by-line using the existing `audit.Event` type, classifies each event by `EventType`, redacts string fields via `audit.Redact` (see below), accumulates the byte size of the projected JSON, and stops appending events when `capBytes` is exceeded — setting `Truncated = true` and incrementing `DroppedEventsCount` for each subsequent event. The default `capBytes` is `64 * 1024`. `Path` is always populated by the caller (it's the audit-log absolute path; building the summary doesn't need to know it, but the CLI handler injects it before printing).

### Redaction

`internal/audit/redact.go`:

```go
var Patterns = []*regexp.Regexp{
    regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]+`),
    regexp.MustCompile(`sk-[A-Za-z0-9]+`),
    regexp.MustCompile(`Bearer [A-Za-z0-9._\-]+`),
    regexp.MustCompile(`[A-Za-z0-9_]*_TOKEN=\S+`),
    regexp.MustCompile(`password=\S+`),
}

const Placeholder = "<REDACTED>"

func Redact(s string) string {
    for _, p := range Patterns {
        s = p.ReplaceAllString(s, Placeholder)
    }
    return s
}
```

`BuildSummary` calls `Redact` on every string field in event payloads (recursively descending into nested `map[string]any` payloads). The on-disk `audit.log` is never modified.

### Onboarding-failure modal (in `internal/listview/`)

Mirrors the splash-modal pattern:

```go
// model.go (additions)
type Model struct {
    // ... existing fields ...
    onboardingFailureVisible      bool
    onboardingFailureSessionDir   string
    onboardingFailureReason       string
    onboardingFailureFocusedSkip  bool  // false = Debug now focused
    onboardingFailureWriteError   string
}

func WithOnboardingFailure(sessionDir, reason string) Option { /* ... */ }
```

`View()` checks `m.onboardingFailureVisible` and overlays via `tuistyle.RenderOverlay` exactly as splash does.

`handleOnboardingFailureKey()` mirrors `handleSplashKey()`:

- Left/Right/Tab/Shift+Tab → toggle focus
- Enter/Space → activate focused button
- Esc → Skip
- Ctrl+C → exits agent-runner (no settings write)

Skip action: `usersettings.Onboarding.Dismissed = time.Now().Format(time.RFC3339)` → `usersettings.Save()`. On success, close modal. On error, close modal, set `onboardingFailureWriteError`, render inline on home until cleared.

Debug-now action: build a `discovery.StartRunMsg` for `core:debug` with `Params: {"failed_session_dir": m.onboardingFailureSessionDir}` and emit it through the existing message bus. Close modal in the same Update.

### Onboarding-failure modal trigger from `--onboarding-from`

In `cmd/agent-runner/main.go`'s existing onboarding dispatch path (the one that runs the `onboarding:onboarding` workflow when `--onboarding-from` is set), the post-run handler today returns exit code 1 on failure. After this change:

```go
result := runOnboardingWorkflow(...)
if result.Outcome != ResultSuccess && !userCancelled(result) {
    // Was: return 1
    // Now:
    settings, _ := usersettings.Load()
    if settings.Onboarding.Dismissed != "" {
        return 1  // user previously dismissed; honour silence
    }
    return runHomeTUIWithOption(
        listview.WithOnboardingFailure(result.SessionDir, result.FailureReason),
    )
}
```

`userCancelled` returns true for Ctrl+C/Escape-confirmed exits and for clean `break_if` exits — those skip the modal entirely.

When the modal is later resolved:

- Skip → exit code 1 (preserves the "onboarding failed" signal for any caller that checks).
- Debug-now → the debug workflow runs as the new foreground; its own outcome propagates.

When onboarding was launched from inside an already-running home TUI (not `--onboarding-from`), the home TUI naturally re-enters after the workflow run and the same modal-trigger check fires inside the home-TUI lifecycle.

### Run-view `d` key

In `internal/runview/model.go` Update():

```go
case "d":
    if !m.running && !m.active {
        return m, func() tea.Msg {
            return LaunchDebugMsg{FailedRunID: m.runID}
        }
    }
```

In `cmd/agent-runner/main.go` switcher, handle `LaunchDebugMsg` by exec-replacing the current process with `agent-runner run core:debug --param failed_run_id=<id>` (same in-place-exec pattern used by `ResumeRunMsg`).

Help bar in runview adds a conditional `d debug` entry when `!m.running && !m.active`.

### Bundled playbook structure

`workflows/core/debug/bundled/docs/playbook.md` (single file, sections cover each step):

```markdown
# Debug-agent playbook

## Overview
(role, three-step structure, expectation to use the inspection CLI rather
than asking the user to paste logs)

## Inspection CLI
(exact shell snippets for `agent-runner debug --state`, `--audit-summary`,
`--show-workflow`; how to interpret the audit summary; how to grep the
full audit.log at the returned path for deeper detail)

## Redaction
(what to redact in any user-facing output: tokens, env values, opaque
strings; reminder that the CLI already pre-redacts known patterns; what
NOT to include in an issue body)

## Triage step
(rubric for outcomes (a)/(b)/(c) with worked examples; how to present
the outcome to the user; instruction to emit continue marker when done)

## Handle-issue step
(workflow: gh auth status check; if OK: gh issue list --search; show
matches with three explicit choices; gh issue create on confirm; if
gh missing/unauthenticated: print body + manual-create URL; redaction
reminder; instruction to emit continue marker on no-op outcomes)

## Handle-resume step
(only if outcome was (a) and user explicitly confirmed both fix-applied
and resume; write run id to {{session_dir}}/resume-target;
"anything else?" continuation pattern; emit continue marker when done)
```

Embedded via the existing `workflows/<ns>/bundled/...` mechanism (see `internal/builtinworkflows.ListAssets` / `ReadAsset`).

## Decisions

1. **Per-session resume marker file (`<sessionDir>/resume-target`) rather than PTY sentinel or global location.**
   - Rationale: decoupled from PTY layer; works for headless and interactive agents; concurrency-safe by construction (each workflow run has its own session dir); inspectable by users.
   - Alternatives considered:
     - A second PTY sentinel mirroring the historical continuation marker. Rejected because it entangles two unrelated concepts in one byte stream and only works for interactive steps.
     - Global marker at `~/.agent-runner/resume-target`. Rejected because two debug workflows in parallel would clobber each other and a stale marker from a crashed earlier session could trigger an unwanted resume.

2. **Single `debug` subcommand with internal flag dispatch (not subcommand restructuring; not three top-level flags).**
   - Rationale: namespaces all three new commands cleanly; extensible (`agent-runner debug --output <id> <step>` later) without restructuring; the existing flat-flag CLI stays untouched.
   - Alternatives considered:
     - Three top-level flags (`--show-workflow`, `--show-state`, `--audit-summary`). Rejected: flat namespace gets noisy and these are tightly related.
     - Full subcommand restructure (`agent-runner workflow show`, `agent-runner run state`, `agent-runner run audit-summary`). Rejected: bigger refactor; deserves its own change.

3. **Three-step workflow YAML with `session: resume` across steps, no runner-mediated outcome passing.**
   - Rationale: each step is a focused contract visible in the audit log; the agent's own conversation history carries the triage outcome across step boundaries (which is what `session: resume` is for); no new "workflow output variable" primitive needed; each non-triage step has an "early continue" path so unneeded phases are real-time no-ops.
   - Alternatives considered:
     - Single interactive agent step that does everything (per the original draft). Rejected: less visible in the audit log; less testable; conflates three distinct phases.
     - Multi-step with `skip_if` driven by a runner-known outcome variable. Rejected: would require a new outcome-marker file primitive when the agent's own memory already carries the answer.

4. **GitHub issue flow via `gh` CLI with graceful fallback (replacing the browser-URL flow committed by the spec).**
   - Rationale: `gh` is widely installed and handles auth, formatting, and issue templates correctly; using it for both search (`gh issue list`) and creation (`gh issue create`) keeps the code path uniform; the fallback (print body + manual-create URL) covers the no-`gh` case without requiring agent-runner to ship its own HTTP client or platform-specific opener.
   - Alternatives considered:
     - `curl` against GitHub API + a Go-side browser opener (original spec). Rejected here: handling auth for issue creation via curl is more complex and brittle than shelling `gh`; agent-runner would have to ship and maintain its own per-OS opener abstraction; users get a worse experience.
     - A new `internal/opener/` Go package + a fourth CLI helper. Rejected: more code, more helpers, doesn't solve the auth problem.
   - Note: when the user picks an existing matched issue to view instead of filing a new one, the agent invokes `gh issue view <number> --web`, which itself shells the OS browser opener. That keeps the per-OS opener concern inside `gh` rather than in agent-runner, and the manual fallback (`gh` missing or unauthenticated) prints the URL for the user to open themselves — no agent-runner opener required either way.

5. **Onboarding-failure modal lives in the home TUI, with `--onboarding-from` failure routed through the home TUI rather than exiting directly.**
   - Rationale: one modal implementation; reuses splash-modal's overlay primitive; consistent UX (modal renders over home chrome); from-inside-TUI invocations naturally re-enter the modal-aware home TUI after the workflow ends.
   - Alternatives considered:
     - Standalone bare-overlay modal without home TUI underneath. Rejected: two modal implementations; inconsistent with splash.
     - Refactor `--onboarding-from` to always boot the home TUI in an "auto-launch onboarding" mode. Rejected: bigger surface area; changes the semantics of `--onboarding-from`.

## Risks / Trade-offs

- **Agent compliance with the playbook (especially redaction and `gh`-availability fallback) depends on the agent following written instructions.** → Mitigated by programmatic redaction in `audit-summary` (defense in depth) and by a clear `gh auth status` check at the top of the `handle-issue` playbook section that branches the entire flow.
- **`gh` is now a documented soft dependency.** Most active GitHub users have it; the fallback (print body + manual-create URL) covers the rest. The proposal is being updated to reflect this; release notes should mention it.
- **Three-step `session: resume` continuity must actually work for each supported agent CLI** (Claude, Codex, Cursor, Copilot). All four support session resume already (the run-view `r` key relies on this); the new wrinkle is sequencing two interactive steps with `session: resume` back-to-back without a runner-mediated agent reset between them. Worth a focused integration test per CLI adapter.
- **64 KB audit-summary cap may truncate large failed runs' details.** Mitigated by the `path` field — the agent can `grep`/`tail` the full `audit.log` at that path for any event the summary dropped.
- **Modal lifecycle for `--onboarding-from`** is a small but real refactor of the exit path. Manual test required with a deliberately-failing onboarding fixture.
- **`syscall.Exec` for resume-handoff inherits the launching environment.** This matches existing in-place-exec semantics for `r` and the resume-CLI flow; not a new risk, but worth verifying that env vars set by the onboarding-failure modal launching path don't leak into the resumed run unexpectedly.

## Migration Plan

- **No data migration.** `usersettings.Onboarding.Dismissed` field already exists; we're only adding a new write-site for it.
- **No breaking CLI changes.** The `debug` subcommand is additive; existing flags are unchanged.
- **No spec changes to audit/state shape.** `audit.log` and `state.json` are consumed as they exist today.

Rollout:

1. Implement the three new CLI inspection ops (`debug --state`, `--audit-summary`, `--show-workflow`) and their helpers (`internal/audit/summary.go`, `internal/audit/redact.go`).
2. Implement the resume-handoff helper package and the post-workflow check in `main.go`.
3. Implement the `d` keybinding in run-view (small).
4. Implement the onboarding-failure modal in listview (mirror splash) and the `--onboarding-from` failure-routing change in `main.go`.
5. Add `workflows/core/debug.yaml` and `workflows/core/debug/bundled/docs/playbook.md`.
6. Update `workflows/core/_group.yaml` if a per-namespace metadata entry is desired (optional).
7. Manual exercise the three trigger points end-to-end against a deliberately-failing fixture.

Rollback: the change is additive; reverting the commits removes the workflow and CLI without breaking existing functionality.

## Testing

**Unit:**

- `audit.BuildSummary` — happy path with mixed events, redaction substitution, cap truncation, missing audit log → empty summary with path populated.
- `audit.Redact` — each pattern matches as expected; non-matching strings unmodified; nested map-walking covers payload depth.
- `resumehandoff.Read` — missing file → ok=false; well-formed content → trimmed id; whitespace-only content → ok=false; multi-line content → first line only.
- `debug` subcommand argv routing — `debug --state` / `--audit-summary` / `--show-workflow` dispatch; unknown op exits non-zero.

**Integration:**

- Fixture failed run + `agent-runner debug --audit-summary <id>` → JSON output matches expected; redaction visible; truncated case.
- End-to-end debug workflow: launched with `failed_run_id`, completes successfully without writing marker → no resume exec; with marker written → resume exec attempted.
- Onboarding-failure modal: deliberately-failing onboarding workflow via `--onboarding-from` → modal renders; Skip persists `Onboarding.Dismissed`; Debug-now launches `core:debug` with `failed_session_dir` param.
- Run-view `d` key: on an inactive failed run → debug workflow launches with `failed_run_id` param.

**Manual:**

- Cross-CLI session-resume across the three interactive steps (Claude, Codex, Cursor, Copilot).
- `gh`-missing path: ensure agent prints body + URL and continues gracefully.
- `gh`-unauthenticated path: same as missing.
- Concurrent debug workflows in two terminals: no cross-contamination of marker files.

## Spec edits applied alongside this design

The following spec files are edited in the same change to align with decisions reached above:

- **`workflow-debugger/spec.md`** — "Pre-submission duplicate search" requirement is rewritten to use `gh issue list --search ... --state open` (instead of `curl` against the GitHub search API). "GitHub issue review and submission" requirement is rewritten to use `gh issue create --title ... --body ...` (instead of opening a prefilled URL via OS browser opener). Both pick up a new failure mode: when `gh auth status` returns non-zero, skip dedupe / fall back to printing the body + manual-create URL.
- **`workflow-resume-handoff/spec.md`** — the `deferred-to-design` scenario in the "Workflow can signal a resume target during a run" requirement is completed with the concrete marker-file convention: `<sessionDir>/resume-target` containing the failed run's id on a single line.
- **`proposal.md`** — the GitHub-issue-reporting bullet drops the "no auth, no `gh` dependency" claim and notes `gh` as a soft dependency with documented fallback. The Technical Approach key-decision about a browser opener is replaced with the `gh` CLI decision.

## Open Questions

None. All deferred-to-design and "exact scope" items from the proposal and specs are resolved above.
