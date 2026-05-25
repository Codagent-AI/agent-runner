# workflow-debugger Specification

## Purpose
Specify the built-in debug workflow that triages failed runs, helps users apply fixable remedies, and prepares GitHub issue reports for suspected Agent Runner bugs.

## Requirements
### Requirement: Workflow accepts optional failed-run identifier

The debug workflow SHALL accept two optional input parameters: `failed_session_dir` (an absolute path to a run's session directory) and `failed_run_id` (the canonical run id). When both are provided, `failed_session_dir` SHALL take precedence and `failed_run_id` SHALL be treated as advisory context only. The workflow SHALL resolve the chosen input to a single canonical session directory before triage begins.

#### Scenario: failed_run_id supplied
- **WHEN** the workflow is launched with `failed_run_id` set and the id resolves to a known run
- **THEN** the agent operates on that run's session directory for the remainder of the workflow

#### Scenario: failed_session_dir supplied
- **WHEN** the workflow is launched with only `failed_session_dir` set and the path is a valid run session directory
- **THEN** the agent operates on that session directory, invokes `debug --state-dir` and `debug --audit-summary-dir` for context, and derives the run id from the directory's `state.json` or basename

#### Scenario: Both params supplied
- **WHEN** the workflow is launched with both parameters set and they refer to different runs
- **THEN** `failed_session_dir` wins; `failed_run_id` is treated as advisory context only

#### Scenario: Neither param supplied
- **WHEN** the workflow is launched with neither parameter set
- **THEN** the agent enters the cold-start interactive run-selection flow before performing any triage

### Requirement: Cold-start interactive run selection

When launched without either input parameter, the agent SHALL list the recent failed runs in the current project (run id, workflow name, age, and a short failure-reason snippet) and SHALL prompt the user to pick one or paste a session-directory path before proceeding to triage. The agent SHALL NOT perform triage until a run has been selected.

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

Before presenting any assessment to the user, the agent SHALL gather context for the chosen run by invoking, at minimum, `agent-runner debug --state <id>` and `agent-runner debug --audit-summary <id>` when it has a run id resolvable in the current project, or `agent-runner debug --state-dir <session-dir>` and `agent-runner debug --audit-summary-dir <session-dir>` when it has a session directory. The workflow YAML SHALL be retrieved via `agent-runner debug --show-workflow <ref>` when referenced. The agent SHALL NOT ask the user to paste log contents that the inspection CLI can produce.

#### Scenario: State and audit summary invoked before assessment
- **WHEN** the agent transitions from input-resolution to assessment for a chosen run
- **THEN** the corresponding state and audit summary commands have been invoked at least once, using either run-id or session-dir forms

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

Secret-like patterns SHALL be redacted from agent-visible audit content. The `agent-runner debug --audit-summary` command SHALL apply a programmatic redaction pass over the audit content before output, replacing matches of a known pattern set with the literal placeholder `<REDACTED>`. The pattern set SHALL include at minimum: GitHub tokens (`gh[pousr]_[A-Za-z0-9]+`), OpenAI-style keys (`sk-[A-Za-z0-9]+`), HTTP bearer credentials (`Bearer [A-Za-z0-9._-]+`), env-style token assignments (`[A-Za-z0-9_]*_TOKEN=[^\s]+`), and `password=[^\s]+` assignments. Additionally, the bundled prompt file SHALL instruct the agent to apply further redaction (summarize rather than verbatim quote opaque/long values) when composing any issue body.

#### Scenario: Known secret pattern substituted in audit summary
- **WHEN** `debug --audit-summary` encounters a value matching a known pattern (e.g. `ghp_AbC123...`)
- **THEN** the output line contains `<REDACTED>` in place of the matched span and the surrounding context is preserved

#### Scenario: Prompt file prompts further agent-side redaction
- **WHEN** the agent prepares to assemble a GitHub issue body
- **THEN** the bundled prompt-file instruction directs the agent to summarize/redact any remaining opaque or long values rather than quote them verbatim

#### Scenario: Pattern set updates apply at read time
- **WHEN** the redaction pattern set is updated and the same `audit.log` is read by `debug --audit-summary` again
- **THEN** the new patterns are applied; the on-disk `audit.log` is not modified

#### Scenario: Redaction never restored
- **WHEN** the agent has received a redacted audit summary
- **THEN** the agent has no way to recover the original secret from the summary (replacement is one-way)

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

### Requirement: Manual resume command on user-fixable outcome

After presenting a (a) user-fixable outcome, the agent SHALL give concrete remediation and, when it knows the failed run id, SHALL tell the user they can resume the run after applying the fix with `agent-runner --resume <run-id>`. The workflow SHALL NOT auto-resume, SHALL NOT ask whether to resume now, and SHALL NOT emit a resume-handoff signal or write a marker file. The resume command SHALL NOT appear when the outcome is (b) or (c).

#### Scenario: User-fixable outcome with known run id
- **WHEN** outcome is (a) and the failed run id is known
- **THEN** the agent prints `agent-runner --resume <run-id>` using the originally-failed run id

#### Scenario: User-fixable outcome without known run id
- **WHEN** outcome is (a) but the agent only has a session directory and cannot derive a run id
- **THEN** the agent gives remediation and explains that it cannot provide an exact resume command without a run id

#### Scenario: Bug outcome suppresses resume command
- **WHEN** outcome is (b) or (c)
- **THEN** the resume command is not presented

#### Scenario: No resume handoff side effect
- **WHEN** the debug workflow presents any outcome
- **THEN** it does not emit a resume-handoff signal, write a resume marker, or cause agent-runner to exec `--resume`

### Requirement: Workflow continuation pattern

After delivering a triage outcome (and after any issue-submission action has resolved), the agent SHALL ask the user whether they are done (e.g. "Are you done?"). If the user signals done, the agent SHALL emit the standard continue-trigger to end the workflow run successfully. If the user signals not-done, the agent SHALL continue interactively in the same session and MAY handle additional runs, follow-up questions, or another full triage cycle.

#### Scenario: User signals done
- **WHEN** the user answers yes to "Are you done?"
- **THEN** the agent emits the continue-trigger; the workflow run ends with outcome success

#### Scenario: User signals not done
- **WHEN** the user answers no to "Are you done?" (they want to continue)
- **THEN** the agent continues in the same interactive session without ending the workflow

#### Scenario: User abandons the session
- **WHEN** the user closes the agent CLI (Ctrl+C, `/exit`, or similar) before signalling done
- **THEN** the workflow records the outcome per the existing interactive-agent abort behavior
