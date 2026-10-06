## Why

When an agent session crashes rather than finishing (`Selected model is at capacity`, an auth or network
error, a CLI that fails to start or exits non-zero), Agent Runner records the step as `failed`, the same
outcome it records for an ordinary failed check. Inside a builtin sub-workflow (`implement-task`,
`verify-change`, `archive-change`, `finalize-pr`), that crash is further hidden: the caller sees only that
the sub-workflow step failed, and a crash that a builtin absorbs with `continue_on_failure` (for example
`finalize-pr`'s `wait-ci` agent) surfaces later as a red CI or validator gate.

The Agent Factory (Codagent-AI/agent-factory#92) needs to tell these apart. A crash is transient and should
get the factory's technical-failure recovery (fresh-clone retry, checkpoint resume, review retry); a real
red validator or CI result after an agent that finished should settle as `failed`. Paul decided
(2026-10-04) that any crashed agent step counts as technical across the factory's workflows, and that Agent
Runner should report the crash rather than the factory parsing logs. agent-factory#92 is blocked on this.

The audience is any workflow author who must react differently to "the infrastructure broke" versus "the
work is wrong", with the factory as the immediate consumer.

## What Changes

- Agent Runner classifies a failed agent execution as an **infrastructure failure** when the agent session
  did not finish: the CLI could not be launched, a headless CLI exited non-zero (after the adapter's
  existing result filtering) or was killed by something other than the runner, or the runner's own
  invocation machinery failed mid-session (control channel, durability). This covers workflow agent steps
  and repair agents.
- Failures that are not crashes keep their current classification as ordinary step failures:
  definition/configuration errors detected before launch (profile resolution, prompt interpolation,
  unsupported mode), and sessions that finished but were judged failed (autonomous `AskUserQuestion`,
  uncollected agent calls). User aborts and interrupts stay `aborted`.
- The step outcome stays `failed`; the classification is an additional, orthogonal **failure kind**
  (`infrastructure` vs. an ordinary step failure). Everything that treats `failed` as failed today —
  `continue_on_failure`, `skip_if: previous_success`, `break_if: failure`, warnings, resume — behaves
  exactly as before.
- Agent Runner reports two related signals and propagates both through containers (groups, loops,
  sub-workflows, including builtins):
  - **Failure kind** — for a step or container that ends failed or exhausted, whether the failure that
    ended it was an infrastructure failure. It describes the terminating failure only.
  - **Crash observed** — whether any agent session crashed anywhere inside the step or container during
    this execution, including crashes absorbed by `continue_on_failure` and crashes inside containers that
    later recovered and **succeeded**. For example, if `finalize-pr`'s `fix-pr` agent crashes and a later
    CI check passes, `finalize-pr` still ends `success` but reports a crash observed. The signal is sticky:
    nothing inside the execution clears it. On resume, the signal from a step that re-executes is replaced
    by its new execution; signals from steps that completed before the interruption are kept.

  The outcome stays as today in both cases, so the policy — for example, the factory's "any crash is
  technical" — belongs to the caller.
- The calling workflow can read both signals for the immediately preceding step in its scope through new
  built-in variables (working names `{{last_step_failure_kind}}` and `{{last_step_crash_observed}}`). They
  can be used in `sh:` `skip_if`, commands, and prompts, so a factory workflow can follow a sub-workflow or
  group with a step that fails the run as technical whenever a crash was observed.
- Each crash records a durable **failure origin**: the agent step's identity (step ID, audit prefix and
  scope, attempt) and a bounded error or stderr excerpt. When an infrastructure failure ends the run, the
  run's classified failure reason comes from this origin and not from shell or script check evidence (for
  example `generate-code failed (infrastructure): Selected model is at capacity`). Ordinary check failures
  keep today's check-derived reason. The origin follows the existing failure-record lifecycle: a later
  terminating failure replaces it, a step that succeeds clears its own origin, and resume keeps earlier
  origins for inspection. A later failure therefore cannot inherit a stale reason.
- Both signals and the origin are recorded durably: on `step_end` / `sub_workflow_end` / `iteration_end` /
  `run_end` audit events and in persisted run state, so they survive resume. A run that halted, or
  completed, can be classified from `run_end` alone.

No breaking changes: outcome values, exit codes, and existing audit fields are unchanged; new fields are
additive.

## Capabilities

### New Capabilities
- `infrastructure-failure-classification`: when an agent execution counts as an infrastructure failure,
  what is excluded, how the failure kind and the crash-observed signal propagate through groups, loops, and
  sub-workflows (including successful ones), when the signal is replaced on resume, and how a calling
  workflow observes them.

### Modified Capabilities
- `step-flow-control`: the preceding step's failure kind and crash-observed signal become observable to the
  next step alongside its outcome; `continue_on_failure` semantics are unchanged.
- `sub-workflows`: a sub-workflow step reports the failure kind of the failure that ended the child, and
  whether any crash was observed inside it, even when it succeeded.
- `builtin-vars`: adds the last-step failure-kind and crash-observed built-in variables.
- `step-repair`: a crashed repair agent makes the check's terminal failure an infrastructure failure and
  marks a crash observed.
- `audit-log-entries`: end events, including `run_end`, carry the failure kind, the crash-observed signal,
  and the failure origin.
- `run-failure-evidence`: adds an agent-crash failure origin; run-end reason selection prefers it when an
  infrastructure failure ended the run and keeps check evidence for ordinary check failures. It also
  defines how the origin is replaced, cleared, and kept across resume.

## Technical Approach

Classification happens where the evidence already exists. `runAgentProcess` / `InvokeAgent` in
`internal/exec/agent.go` already distinguish launch failure, non-zero exit, runner errors, and
"finished but judged failed"; they return the kind with the outcome instead of collapsing it to
`OutcomeFailed`. The same applies to the repair-agent path in `internal/exec/repair.go`.

The kind travels next to `StepOutcome` rather than as a new `StepOutcome` value. A new outcome value would
ripple through every `== OutcomeFailed` comparison, `break_if: failure`, warning handling, the TUI, and
consumers of audit `outcome`, and every one of them would need to treat it as failed anyway. The kind lives
on the execution context next to `LastStepOutcome`. The crash-observed signal is a sticky flag on each
container frame that ORs into its parent when the container finishes, whatever its outcome. This keeps
today's flow control untouched and makes propagation a container-finish concern in `finishChildStep`
(sub-workflows), the group executor, the loop executor, and the runner's top-level step loop.

The failure origin sits next to the existing `LastFailure` check record (`internal/model/execution.go`).
It is not folded into that record, because a direct agent crash has no check, and an earlier continued
check would otherwise supply unrelated evidence. `classifyRunFailure` in `internal/runner/runner.go` picks
the agent origin when the terminating failure kind is infrastructure, and the check record otherwise.

The built-in variables are set from the preceding step's recorded signals, scoped exactly like
`skip_if: previous_success`. Persistence goes through the existing state and audit writers, so a resumed
run reports the same signals, and the factory can read them from `run_end` whether or not the run halted.

Main risk: some headless CLIs may exit non-zero for reasons that are not crashes (for example a turn limit).
The default treats any non-zero exit of an unfinished session as infrastructure, which matches the issue's
definition; adapters keep their existing `HeadlessResultFilter` hook and can be refined later if a
non-crash exit is found. The crash-observed signal is the other judgment call. It exists because builtins
such as `finalize-pr` deliberately absorb agent failures, which could otherwise hide a crash entirely or
relabel it as a red gate. Agent Runner reports the crash and does not change the outcome, so callers that
do not care see no change.

## Out of Scope

- Changing the factory's workflows (agent-factory#92 consumes this separately).
- Retrying or recovering infrastructure failures inside Agent Runner.
- New process exit codes, or changing `continue_on_failure` so it refuses to absorb crashes.
- Classifying shell or script step failures, or failures inside `call_agent` child agents, as
  infrastructure.
- Parsing agent output text to detect specific provider errors.
- TUI redesign beyond showing the classified failure reason where the reason already appears.

## Impact

- Code: `internal/exec` (agent, repair, group/loop/sub-workflow finish, outcome recording),
  `internal/model` (execution context failure kind, crash-observed flag, failure origin, built-in
  variables, persisted step state), `internal/runner` (top-level propagation, run end, failure-reason
  selection in `classifyRunFailure`), `internal/audit` (end-event fields), and the
  failure-reason formatting used by `runview`, `listview`, and the resume hint.
- Data: additive fields in audit events and run state; existing runs without the field read as ordinary
  failures.
- Users: workflow authors gain a way to branch on crashes; existing workflows and builtins behave the same.
- Downstream: unblocks Codagent-AI/agent-factory#92.
