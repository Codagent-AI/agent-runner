# Task: Debug workflow + run-view trigger

## Goal

Ship the actual debug workflow (`workflows/core/debug.yaml` plus its bundled playbook) and the run-view `d` keybinding that launches it for the currently-viewed run. After this task, an end user viewing any inactive run in the run view can press `d`, the debug workflow launches with the failed run's id pre-filled, the agent walks the user through triage, and optionally either opens a GitHub issue via `gh` or writes a resume-handoff marker to auto-resume the failed run on success.

## Background

This task delivers the visible debug feature reachable from the run view. It assumes the runner-side helpers already exist: the agent will shell out to `agent-runner debug --state <id>`, `agent-runner debug --audit-summary <id>`, and `agent-runner debug --show-workflow <ref>` for context gathering, and the resume-handoff marker file at `<sessionDir>/resume-target` is read by the runner after the workflow ends.

### Why this exists

When a workflow fails, today the user is dropped in the run view with a failure-reason string and no diagnostic action. Real users don't have the source tree, audit log path, or workflow YAML at their fingertips, so even motivated users can't easily file an actionable issue. The debug workflow turns a failure into either a concrete fix (with optional auto-resume) or a high-quality GitHub issue, and the run-view `d` key is the most natural place to reach it — the user is already looking at the failed run.

### Key design decisions

**Three interactive steps with `session: resume`** across them — `triage`, `handle-issue`, `handle-resume`. The agent's triage conclusion lives in its own conversation history; it carries across step boundaries via `session: resume`, so no runner-mediated outcome variable is needed. Each non-triage step has an "early continue" path: when nothing needs doing (e.g., outcome was user-fixable so `handle-issue` is a no-op), the agent emits the continue marker immediately and the step is a real-time no-op.

**`gh` CLI for both duplicate-search and issue creation**, with a graceful manual fallback when `gh` is missing or unauthenticated. The playbook drives this: `gh auth status` is checked at the top of `handle-issue`; on non-zero exit the agent switches to printing the title + body + manual-create URL.

**Programmatic redaction in the audit-summary CLI plus playbook-side redaction guidance** (defense in depth). The agent never sees raw matched secrets in the summary output; the playbook also instructs it to summarize/redact further when composing any issue body.

**Per-session resume marker** at `<sessionDir>/resume-target` in the debug workflow's own session directory. Written only after outcome (a) AND the user confirms both that the fix was applied AND the resume prompt — never on bug/unknown outcomes, never on implicit "I think I fixed it" mentions.

**Run-view `d` gate**: available whenever the viewed run is inactive (any non-active status — `failed`, `completed`, etc.) AND the live-run-view is not currently executing a workflow. Available at any drill depth. Becomes available immediately when a live run transitions to terminal — user does not need to exit and re-enter the run view.

### Code-touch points

You MUST read these files before starting:

- `openspec/changes/debug-and-report-issues/design.md` — full design context, including the workflow YAML skeleton and playbook structure.
- `openspec/changes/debug-and-report-issues/specs/workflow-debugger/spec.md` — verbatim spec for the workflow's behavioral contract.
- `openspec/changes/debug-and-report-issues/specs/failure-debug-entry-points/spec.md` — verbatim spec for the run-view `d` keybinding and the main-menu discoverability.
- `openspec/changes/debug-and-report-issues/specs/builtin-workflows/spec.md` — the delta that adds `debug` to the `core` namespace's enumerated "at minimum" list.
- `workflows/onboarding/onboarding.yaml` and `workflows/onboarding/help.yaml` — examples of existing interactive-agent workflows that bundle markdown docs.
- `internal/runview/model.go` — the `r` resume-run keybinding (around the resume-handler block) is the structural template for the new `d` handler, including the inactive-run gate and the help-bar entry.
- `cmd/agent-runner/main.go` — the `ResumeRunMsg` handler that execs in place is the template for the new `LaunchDebugMsg` handler.

**New files to create:**

- `workflows/core/debug.yaml` — workflow definition with three interactive steps (`triage`, `handle-issue`, `handle-resume`), `session: new` on step 1 and `session: resume` on steps 2 and 3, two optional params (`failed_session_dir`, `failed_run_id`), prompts referencing the playbook at `{{session_dir}}/bundled/core/debug/docs/playbook.md`.
- `workflows/core/debug/bundled/docs/playbook.md` — single playbook file with sections: Overview, Inspection CLI, Redaction, Triage step, Handle-issue step, Handle-resume step. Per the design's bundled-playbook structure.
- (Optional) `workflows/core/debug/bundled/...` other assets if needed.

**Existing files to modify:**

- `internal/runview/model.go` — add `case "d"` handler alongside `case "r"`. Gate `!m.running && !m.active`. On press, emit `LaunchDebugMsg{FailedRunID: m.runID}`. Help bar adds `d debug` entry, gated identically.
- `cmd/agent-runner/main.go` — handle `LaunchDebugMsg` by exec-replacing the current process with `agent-runner run core:debug --param failed_run_id=<id>` (mirror the existing `ResumeRunMsg` exec pattern).

### Workflow YAML shape

The workflow has exactly three interactive steps in this order, all sharing the same agent CLI session:

```yaml
steps:
  - id: triage
    mode: interactive
    session: new
    # agent investigates with inspection CLI, classifies outcome (a/b/c),
    # presents result to user
  - id: handle-issue
    mode: interactive
    session: resume
    # if outcome was (b)/(c): gh auth → gh issue list (dedupe) → present body
    #   → gh issue create OR manual fallback
    # if outcome was (a) or user declined: agent emits continue marker
    #   immediately without prompting
  - id: handle-resume
    mode: interactive
    session: resume
    # if outcome was (a) AND user confirmed fix AND confirmed resume:
    #   agent writes failed run id to {{session_dir}}/resume-target,
    #   then emits continue marker
    # else: ask "anything else?" — if no → continue marker; if yes →
    #   stay in this step conversationally and handle further requests
```

Use the existing onboarding/help workflow as the structural template for an interactive-agent step with a bundled playbook reference. Both optional params (`failed_session_dir`, `failed_run_id`) are passed through to the agent prompt via `{{...}}` interpolation; the agent resolves them per the cold-start selection rules in its playbook.

### Playbook structure

Single file at `workflows/core/debug/bundled/docs/playbook.md` with these sections (titles in this order):

- **Overview** — the agent's role, the three-step structure, the expectation to use the inspection CLI rather than asking the user to paste logs.
- **Inspection CLI** — exact shell snippets for `agent-runner debug --state <id>`, `--audit-summary <id>`, `--show-workflow <ref>`; how to read the JSON outputs; how to `grep`/`tail` the full audit log at the path the summary returns.
- **Redaction** — what to summarize vs verbatim-quote in any user-facing output (issue bodies in particular); reminder that the CLI already pre-redacts known patterns; instruction to summarize/redact further.
- **Triage step** — the rubric for outcomes (a) user-fixable, (b) suspected bug, (c) unknown, with worked examples; instruction to present the outcome to the user; reminder to emit the continue marker when done.
- **Handle-issue step** — only enter when outcome is (b)/(c); start with `gh auth status`; on success use `gh issue list --repo <org>/agent-runner --search "<query>" --state open --json number,title,url,createdAt,author` for dedupe, present three explicit choices (open match / file new / cancel), and on file-new use `gh issue create --repo <org>/agent-runner --title "..." --body "..."`. On `gh` missing/unauthenticated, switch to manual fallback: print title + body in copy-friendly format + the manual-create URL `https://github.com/<org>/agent-runner/issues/new`. Outcome (a) skips this step (emit continue marker immediately).
- **Handle-resume step** — only act when outcome was (a) AND user explicitly confirmed both that the fix was applied AND the resume prompt; write the failed run id to `{{session_dir}}/resume-target` (`echo <id> > {{session_dir}}/resume-target`); ask "anything else?"; on done emit continue marker; on more, stay in this step and handle requests until done.

**Constraints and conventions:**

- Workflows are configuration, not application code — per project policy, TDD does not apply to workflow YAML edits. Tests are not expected for the workflow file or playbook content alone. (See `CLAUDE.md` development workflow notes.)
- The `d` keybinding handler IS application code; write a test for it (gate evaluation, message emission). Use `google/go-cmp` for structured comparisons.
- The `LaunchDebugMsg` handler in main.go uses the same in-place-exec pattern as `ResumeRunMsg`; do not introduce a new exec helper.
- After this change, `core:debug` is automatically registered as a builtin workflow under the `core` namespace (via the existing build-time `workflows/` embed). The `builtin-workflows` spec's `core` namespace requirement is satisfied by adding the YAML; no other registration is needed.
- Run `make fmt`, `make test`, `make lint`, `make build` before committing.

**Strictly self-contained:** This task assumes the runner-side helpers (`agent-runner debug` subcommand, `internal/audit/summary.go`, `internal/audit/redact.go`, `internal/resumehandoff/`, the post-workflow marker check in `main.go`) already exist. You do not implement them in this task.

## Spec

### Requirement: Workflow accepts optional failed-run identifier

The debug workflow SHALL accept two optional input parameters: `failed_session_dir` (an absolute path to a run's session directory) and `failed_run_id` (the canonical run id). When both are provided, `failed_run_id` SHALL take precedence and `failed_session_dir` SHALL be ignored. The workflow SHALL resolve the chosen input to a single canonical session directory before any triage step runs.

#### Scenario: failed_run_id supplied
- **WHEN** the workflow is launched with `failed_run_id` set and the id resolves to a known run
- **THEN** the agent operates on that run's session directory for the remainder of the workflow

#### Scenario: failed_session_dir supplied
- **WHEN** the workflow is launched with only `failed_session_dir` set and the path is a valid run session directory
- **THEN** the agent operates on that session directory; the run id is derived from the directory's `state.json`

#### Scenario: Both params supplied
- **WHEN** the workflow is launched with both parameters set and they refer to different runs
- **THEN** `failed_run_id` wins; `failed_session_dir` is ignored

#### Scenario: Neither param supplied
- **WHEN** the workflow is launched with neither parameter set
- **THEN** the agent enters the cold-start interactive run-selection flow before performing any triage

### Requirement: Cold-start interactive run selection

When launched without either input parameter, the agent SHALL list the recent failed runs in the current project (run id, workflow name, age, and a short failure-reason snippet) and SHALL prompt the user to pick one or paste a session-directory path before proceeding to triage. The agent SHALL NOT perform triage steps until a run has been selected.

#### Scenario: List shown on cold start
- **WHEN** the agent is launched cold and recent failed runs exist for the current project
- **THEN** the agent's first user-visible message lists those runs with id, workflow name, age, and failure-reason snippet

#### Scenario: User selects from list
- **WHEN** the user selects one of the listed runs by number or id
- **THEN** the agent treats that run's session directory as the target and proceeds to context gathering

#### Scenario: User pastes a session-directory path
- **WHEN** the user pastes an absolute path that is a valid run session directory
- **THEN** the agent uses that path as the target and proceeds to context gathering

#### Scenario: User pastes an invalid path
- **WHEN** the user pastes a path that is not a valid run session directory
- **THEN** the agent informs the user and prompts again; triage does not begin

#### Scenario: No failed runs in project
- **WHEN** the agent is launched cold and no failed runs exist in the current project
- **THEN** the agent informs the user and offers either to debug a successful run by path/id or to end the workflow

### Requirement: Context gathering via inspection CLI

Before presenting any assessment to the user, the agent SHALL gather context for the chosen run by invoking, at minimum, `agent-runner debug --state <id>` and `agent-runner debug --audit-summary <id>`. The workflow YAML SHALL be retrieved via `agent-runner debug --show-workflow <ref>` when referenced. The agent SHALL NOT ask the user to paste log contents that the inspection CLI can produce.

#### Scenario: State and audit summary invoked before assessment
- **WHEN** the agent transitions from input-resolution to assessment for a chosen run
- **THEN** both `debug --state` and `debug --audit-summary` have been invoked at least once

#### Scenario: Workflow YAML retrieved via CLI
- **WHEN** the agent needs to reason about the failed workflow's definition
- **THEN** the agent retrieves it by invoking `debug --show-workflow <ref>`, not by reading from a hard-coded path

#### Scenario: Agent greps full audit log for deeper detail
- **WHEN** the bounded audit summary is insufficient and the agent needs specific event payloads or output capture
- **THEN** the agent reads the full `audit.log` at the absolute path returned by `debug --audit-summary`

#### Scenario: Audit log missing
- **WHEN** the failed run has no `audit.log` (e.g. crashed before any audit event was written)
- **THEN** the agent proceeds with `debug --state` output and the workflow YAML alone, and tells the user the audit log was absent so the assessment may be less specific

#### Scenario: Agent does not ask user to paste logs
- **WHEN** the agent needs audit-log content the inspection CLI can produce
- **THEN** the agent invokes the CLI rather than asking the user to paste log lines

### Requirement: Triage outcome classification

The agent's assessment for the chosen run SHALL classify it as exactly one of: **(a) user-fixable** (a concrete remediation the user can apply), **(b) suspected agent-runner bug** (the failure appears to be in agent-runner itself), or **(c) unknown** (insufficient evidence to choose between user error and bug). The workflow SHALL NOT end without presenting at least one classification.

#### Scenario: User-fixable outcome
- **WHEN** the agent classifies the failure as (a)
- **THEN** the agent presents a concrete remediation description to the user (e.g. "your working tree has uncommitted changes; stash or commit before re-running")

#### Scenario: Suspected-bug outcome
- **WHEN** the agent classifies the failure as (b)
- **THEN** the agent offers to file a GitHub issue (per the issue-review-and-submission requirement)

#### Scenario: Unknown outcome
- **WHEN** the agent classifies the failure as (c)
- **THEN** the agent offers to file a GitHub issue framed as "unknown cause; here is the evidence we collected"

#### Scenario: Classification required before completion
- **WHEN** the workflow is at the point of signalling completion
- **THEN** the agent has presented at least one of (a), (b), or (c)

### Requirement: Audit-log redaction (defense in depth)

Secret-like patterns SHALL be redacted from agent-visible audit content. The `agent-runner debug --audit-summary` command SHALL apply a programmatic redaction pass over the audit content before output, replacing matches of a known pattern set with the literal placeholder `<REDACTED>`. The pattern set SHALL include at minimum: GitHub tokens (`gh[pousr]_[A-Za-z0-9]+`), OpenAI-style keys (`sk-[A-Za-z0-9]+`), HTTP bearer credentials (`Bearer [A-Za-z0-9._-]+`), env-style token assignments (`[A-Za-z0-9_]*_TOKEN=[^\s]+`), and `password=[^\s]+` assignments. Additionally, the bundled playbook SHALL instruct the agent to apply further redaction (summarize rather than verbatim quote opaque/long values) when composing any issue body.

Note: the programmatic redaction pass is implemented by the inspection-CLI work and is assumed present. This task is responsible for the playbook-side redaction instruction.

#### Scenario: Playbook prompts further agent-side redaction
- **WHEN** the agent prepares to assemble a GitHub issue body
- **THEN** the bundled playbook instruction directs the agent to summarize/redact any remaining opaque or long values rather than quote them verbatim

### Requirement: GitHub CLI availability check

The agent SHALL check whether the `gh` CLI is available and authenticated before performing any GitHub interaction for outcome (b) or (c). The check SHALL be performed by invoking `gh auth status` and inspecting its exit status. If the exit status is zero, the agent SHALL use `gh` for both duplicate-search and issue creation. If the exit status is non-zero (gh missing from PATH, gh present but not authenticated, or any other failure), the agent SHALL switch to the manual fallback path (see "Issue submission via gh or manual fallback") without attempting any further `gh` invocation in this workflow run.

#### Scenario: gh present and authenticated
- **WHEN** the agent has reached the issue flow for outcome (b) or (c) and `gh auth status` exits zero
- **THEN** the agent uses `gh` for duplicate-search and issue creation

#### Scenario: gh missing
- **WHEN** the agent has reached the issue flow and `gh` is not on PATH
- **THEN** the agent switches to the manual fallback path and does not attempt further `gh` invocations

#### Scenario: gh unauthenticated
- **WHEN** the agent has reached the issue flow, `gh` is on PATH, but `gh auth status` exits non-zero
- **THEN** the agent switches to the manual fallback path and does not attempt further `gh` invocations

#### Scenario: Outcome (a) skips gh check
- **WHEN** triage outcome is (a) user-fixable
- **THEN** the `gh auth status` check is not performed (the issue flow is not entered at all)

### Requirement: Pre-submission duplicate search

For outcome (b) or (c), and only when the gh availability check passed, before assembling a new-issue body the agent SHALL search GitHub for similar **open** issues in the agent-runner repository. The search SHALL be performed by invoking `gh issue list --repo <org>/agent-runner --search "<query>" --state open --json number,title,url,createdAt,author` and parsing the JSON response. The query SHALL be constructed from salient terms in the failure reason and the failing workflow name. The agent SHALL present any matches (title, URL, opened-by, age) to the user and SHALL offer three explicit choices: (i) open one of the matches in the browser instead of filing a new issue, (ii) file a new issue anyway, or (iii) cancel the issue flow. The agent SHALL NOT proceed to body assembly without the user choosing (ii) or explicitly electing to file a new one.

If the `gh issue list` call fails (non-zero exit, malformed JSON, network error from gh's side), the agent SHALL inform the user that duplicate search could not run, and SHALL offer two explicit choices: (a) proceed to file a new issue anyway, or (b) cancel. The agent SHALL NOT silently proceed without surfacing the search failure.

#### Scenario: Matches found and presented
- **WHEN** `gh issue list` returns one or more open issues for the query
- **THEN** the agent lists them (title, URL, opened-by, age) and offers the three explicit choices: open a match, file new, or cancel

#### Scenario: User selects an existing match
- **WHEN** the user picks one of the listed matches
- **THEN** the agent opens that issue's HTML URL by invoking `gh issue view <number> --web` (or by printing the URL for the user to open if `gh issue view --web` fails); body assembly is skipped

#### Scenario: User chooses to file new despite matches
- **WHEN** the user elects to file a new issue after seeing matches
- **THEN** the agent proceeds to the issue-submission flow for a brand-new issue

#### Scenario: User cancels after seeing matches
- **WHEN** the user picks cancel after seeing matches
- **THEN** no issue is assembled, no `gh issue create` call is made, and the agent returns to the workflow-continuation prompt ("anything else?")

#### Scenario: No matches found
- **WHEN** `gh issue list` returns zero matches
- **THEN** the agent tells the user no similar open issues were found and proceeds directly to the issue-submission flow

#### Scenario: gh issue list fails
- **WHEN** the `gh issue list` call exits non-zero or returns unparseable output
- **THEN** the agent reports the failure and offers two explicit choices: proceed to file a new issue without dedupe, or cancel; the agent does NOT skip the search silently

#### Scenario: Skipped under manual fallback
- **WHEN** the gh availability check has switched the workflow into the manual fallback path
- **THEN** duplicate search is skipped (the agent has no programmatic way to search), and the agent informs the user that dedupe is unavailable in this mode

### Requirement: Issue submission via gh or manual fallback

When the user has elected to file a new issue (no matches found, or matches were shown and the user chose "file new", or the user chose to proceed without dedupe), the agent SHALL present the proposed issue title and body in chat for review and SHALL wait for explicit user confirmation before submitting. On confirmation:

- **If the gh availability check passed**, the agent SHALL submit the issue by invoking `gh issue create --repo <org>/agent-runner --title "<title>" --body "<body>"`. On success, the agent SHALL print the resulting issue URL (from `gh`'s stdout) in chat. On non-zero exit, the agent SHALL switch to the manual fallback path for this submission and report the `gh` error.
- **If the manual fallback path is active** (gh missing, unauthenticated, or `gh issue create` just failed), the agent SHALL NOT attempt any URL opener. Instead, the agent SHALL print the full title and body in chat in a copy-friendly format AND print the manual-create URL `https://github.com/<org>/agent-runner/issues/new` with instructions for the user to paste the title and body there.

#### Scenario: Body presented before submission
- **WHEN** the agent has reached the issue-submission flow
- **THEN** the proposed title and body are shown in chat and no `gh issue create` call is made until the user confirms

#### Scenario: User confirms — gh creates issue
- **WHEN** the user confirms the body, the gh availability check passed, and `gh issue create` exits zero
- **THEN** the agent prints the resulting issue URL from `gh`'s stdout in chat

#### Scenario: gh issue create fails after confirm
- **WHEN** the user confirms the body, the gh availability check passed, and `gh issue create` exits non-zero
- **THEN** the agent reports the `gh` error and switches to the manual fallback for this submission: prints the title + body + manual-create URL

#### Scenario: Manual fallback active — print body and URL
- **WHEN** the user confirms the body and the manual fallback path is active
- **THEN** the agent prints the proposed title and body in a copy-friendly format AND prints `https://github.com/<org>/agent-runner/issues/new` with paste instructions; no opener is invoked and no `gh` call is made

#### Scenario: User requests changes
- **WHEN** the user declines the proposed body or requests edits
- **THEN** the agent revises the body, re-presents it, and waits again for explicit confirmation; no submission or print happens in the meantime

#### Scenario: Outcome (a) does not trigger issue flow
- **WHEN** the triage outcome is (a) user-fixable
- **THEN** the agent does not assemble or present a GitHub issue body

#### Scenario: New-issue flow only after duplicate-search resolution
- **WHEN** outcome is (b) or (c) and the gh availability check passed
- **THEN** the agent has run duplicate-search (or surfaced its failure) before reaching the new-issue flow

### Requirement: Auto-resume offer on confirmed fix

After presenting a (a) user-fixable outcome AND receiving an explicit signal from the user that the fix has been applied, the agent SHALL ask whether to resume the failed run now and SHALL require a yes/no answer. On explicit yes, the workflow SHALL emit a *resume-handoff* signal carrying the failed run's id (per the `workflow-resume-handoff` capability) and end successfully. On explicit no, the agent SHALL continue with the standard "anything else?" continuation. The resume offer SHALL NOT appear when the outcome is (b) or (c), and SHALL NOT be acted on implicitly from a passing mention of a fix.

#### Scenario: Fix applied, resume confirmed
- **WHEN** outcome is (a), the user has explicitly confirmed the fix is applied, and the user explicitly answers yes to the resume prompt
- **THEN** the workflow emits the resume-handoff signal with the failed run's id and ends successfully

#### Scenario: Fix applied, resume declined
- **WHEN** outcome is (a), the user has explicitly confirmed the fix is applied, and the user answers no to the resume prompt
- **THEN** no resume-handoff is emitted; the agent proceeds to the standard "anything else?" continuation

#### Scenario: Bug outcome suppresses resume offer
- **WHEN** outcome is (b) or (c)
- **THEN** the resume prompt is never presented

#### Scenario: Implicit fix mention does not bypass confirmation
- **WHEN** the user mentions a fix in passing without explicit confirmation that it was applied
- **THEN** the agent prompts explicitly to confirm before offering resume; no resume-handoff is emitted from the passing mention alone

#### Scenario: Resume target is the failed run, not the debug run
- **WHEN** the resume-handoff is emitted
- **THEN** the carried run id is the originally-failed run (`failed_run_id`, or the id derived from `failed_session_dir`), not the debug workflow's own run id

### Requirement: Workflow continuation pattern

After delivering a triage outcome (and after any resume offer or issue-submission action has resolved), the agent SHALL ask the user whether they are done (e.g. "Are you done?"). If the user signals done, the agent SHALL emit the standard continue-trigger to end the workflow run successfully. If the user signals not-done, the agent SHALL continue interactively in the same session and MAY handle additional runs, follow-up questions, or another full triage cycle.

#### Scenario: User signals done
- **WHEN** the user answers yes to "Are you done?"
- **THEN** the agent emits the continue-trigger; the workflow run ends with outcome success

#### Scenario: User signals not done
- **WHEN** the user answers no to "Are you done?" (they want to continue)
- **THEN** the agent continues in the same interactive session without ending the workflow

#### Scenario: User abandons the session
- **WHEN** the user closes the agent CLI (Ctrl+C, `/exit`, or similar) before signalling done
- **THEN** the workflow records the outcome per the existing interactive-agent abort behavior; no resume-handoff is emitted

### Requirement: Main-menu discoverability

The debug workflow SHALL be discoverable as a standard builtin under the `core` namespace (registered name: `core:debug`) and SHALL appear in the new-workflow tab of the home TUI alongside other `core:` workflows. No special pinning, highlighting, dedicated top-level shortcut, or alternate entry point is added for it.

#### Scenario: Debug workflow appears in new-workflow tab
- **WHEN** the user opens the new-workflow tab of the home TUI
- **THEN** the discovered workflow list includes `core:debug` alongside other `core:` workflows

#### Scenario: Debug workflow launches with no params from menu
- **WHEN** the user selects `core:debug` from the new-workflow tab and starts it
- **THEN** the workflow launches with neither `failed_session_dir` nor `failed_run_id` set, triggering the cold-start interactive run-selection flow defined by the `workflow-debugger` capability

#### Scenario: No special menu affordance
- **WHEN** the home TUI is rendered
- **THEN** there is no pinned, highlighted, or top-level entry for `core:debug` outside of the standard discovery list

### Requirement: Run-view `d` keybinding launches debug

The run view SHALL provide a `d` keyboard action that launches `core:debug` with `failed_run_id` pre-filled to the currently-viewed run's id. The action SHALL be available whenever the viewed run is **inactive** (any non-active status, including `failed`, `completed`, and otherwise inactive) AND the live-run-view is not currently executing a workflow. It SHALL be available at any drill depth. It SHALL become available **immediately** when a live run transitions to a terminal state — the user SHALL NOT need to exit and re-enter the run view to use it.

#### Scenario: d on inactive run launches debug with run id
- **WHEN** the viewed run is inactive, the live-run-view is not running a workflow, and the user presses `d`
- **THEN** `core:debug` launches with `failed_run_id` set to the current run's id

#### Scenario: d ignored while run is active
- **WHEN** the viewed run is active (the live-run TUI is still executing the workflow, or the run lock is active)
- **THEN** pressing `d` does nothing

#### Scenario: d available at any drill depth
- **WHEN** the user is drilled inside a sub-workflow, loop, or iteration in an inactive run and presses `d`
- **THEN** `core:debug` launches with `failed_run_id` set to the top-level run id (drill depth does not affect the action; the param always refers to the outer run)

#### Scenario: d becomes available immediately on live-run termination
- **WHEN** a workflow finishes in the live-run-view (success or failure) and transitions to a terminal state
- **THEN** `d` becomes bound and usable without the user exiting and re-entering the run view

#### Scenario: Help bar advertises d when available
- **WHEN** the gate for `d` is satisfied
- **THEN** the help bar includes an entry for the `d` binding

#### Scenario: Help bar omits d when unavailable
- **WHEN** the gate for `d` is not satisfied (run is active, or live-run-view is running a workflow)
- **THEN** the help bar does not include the `d` entry

### Requirement (delta): Core namespace for general-purpose builtins

The builtin set SHALL include a `core` namespace containing general-purpose workflows that are not tied to any particular planning methodology. The `core` namespace SHALL at minimum contain `finalize-pr`, `implement-task`, `run-validator`, and `debug`.

#### Scenario: Debug workflow available under core
- **WHEN** the user runs `agent-runner run core:debug`
- **THEN** the debug workflow loads from the embedded `core` namespace and executes

## Done When

- `workflows/core/debug.yaml` exists with three interactive steps (`triage`, `handle-issue`, `handle-resume`) using `session: new` then `session: resume`, and two optional params (`failed_session_dir`, `failed_run_id`).
- `workflows/core/debug/bundled/docs/playbook.md` exists with the six sections (Overview, Inspection CLI, Redaction, Triage step, Handle-issue step, Handle-resume step), covering the behavioral rubrics above.
- The `d` keybinding handler exists in `internal/runview/model.go` with the inactive-run gate, the `LaunchDebugMsg` emission, and the conditional help-bar entry. Unit tests cover gate evaluation and message emission.
- `main.go` handles `LaunchDebugMsg` by exec-replacing the current process with `agent-runner run core:debug --param failed_run_id=<id>` (mirrors `ResumeRunMsg`).
- `agent-runner run core:debug` runs to completion end-to-end against a fixture failed run launched from the run view's `d` key; the agent reaches all three steps and the workflow ends success.
- `make fmt`, `make test`, `make lint`, `make build` all pass.
