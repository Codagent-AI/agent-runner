## Why

Today the backend for autonomous agent steps comes only from the global user setting
`autonomous_backend` (`headless`, `interactive`, `interactive-claude`) in
`~/.agent-runner/settings.yaml`. A workflow author can't say "this particular step must run
autonomous-interactive" even when its correctness depends on that.

The motivating case is a lead orchestrator step (`orchestrate-implementation` in the orchestrated
implement-change workflow proposed in PR #209). It delegates work to sub-agents. Under the headless
backend, when the lead ends its turn early the CLI process exits and kills any sub-agent still running.
A `codex exec` experiment showed the sub-agent `turn_aborted` 18 ms after the parent's
`task_complete`, its work never happened, and the step still reported success. Prompt instructions such
as "wait for every sub-agent" aren't a reliable guard. The autonomous-interactive backend already keeps
the session alive until the agent explicitly signals completion through the control channel. It also
keeps the autonomous permission flags, so an unattended orchestrator and its sub-agents don't stop on
approval prompts.

The workarounds both fall short:

- `mode: interactive` deliberately runs without permission loosening ("No permission loosening in
  interactive mode"), so an unattended step would block on approvals.
- The global `autonomous_backend: interactive` setting works, but it changes every autonomous step in
  every run and depends on each user's personal settings instead of the workflow.

Workflow authors need to pin the backend where the workflow's correctness depends on it.

## What Changes

- Agent steps gain an optional `autonomous_backend` field with the same values as the user setting:
  `headless`, `interactive`, `interactive-claude`. When present on a step that resolves to autonomous
  mode, it overrides the global setting for that step only. When absent, behavior is unchanged.
- Values keep the meanings they already have: `interactive-claude` means interactive only when the
  step's resolved CLI is Claude, and headless otherwise. `headless` lets a step stay headless even when
  the user's global setting is interactive.
- An explicit step-level interactive backend is a requirement, not a preference. When a step sets
  `autonomous_backend: interactive`, or `interactive-claude` and its resolved CLI is Claude, and the
  environment can't provide that backend, the step fails before the agent is launched. The error states
  the step, the requested backend, and the reason (no TTY, or external-user mode). The step is never
  silently downgraded to headless, which would bring back the sub-agent abort the field exists to
  prevent. This follows the existing precedent that interactive shell and UI steps fail rather than
  degrade in external-user mode.
- Steps without the field keep today's behavior. A global interactive setting still falls back to
  headless with a warning when there is no TTY, and external-user mode still runs those steps headless.
  `interactive-claude` on a step whose resolved CLI isn't Claude still means headless, with no failure.
- Load-time validation rejects invalid values. It also rejects the field on steps where it can never
  apply: shell, script, UI, and sub-workflow steps, and agent steps whose explicit `mode` is
  `interactive`. It rejects the contradictory combination of `autonomous_backend: interactive` (or
  `interactive-claude`) with `capture` on the same step, instead of silently forcing headless.
- Docs describe the step field and how it takes precedence over the user setting.

No breaking changes: the field is optional and additive, and existing workflows and settings files behave
the same.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `step-model`: adds the per-step `autonomous_backend` field, its allowed values, and the step types
  and field combinations it is rejected on.
- `cli-adapter`: invocation-context resolution ("TTY fallback for autonomous-interactive", "Capture
  forces autonomous-headless", "Autonomy system prompt enrichment for interactive backend") resolves
  the backend from the step field first, then from the user setting. The TTY fallback is narrowed: it
  applies only when the interactive request comes from the user setting. A step-level request with no
  TTY fails the step.
- `external-user-mode`: "Other step types in external-user mode" adds that an autonomous step with an
  explicit step-level interactive backend fails with a clear error instead of running headless. Steps
  that only inherit the user setting still run headless.

## Technical Approach

- **Model:** add `AutonomousBackend string` (`yaml:"autonomous_backend,omitempty"`) to `model.Step`
  and validate it in `Step.Validate` alongside the existing `capture`/`mode` checks. The model package
  must not import `usersettings`, so the three allowed literals are validated in the model package.
  Alternatively they move to a small shared location; this is a design-time choice.
- **Resolution:** `resolveInvocationContext` in `internal/exec/agent.go` already centralizes backend
  choice. It gains the step's backend. The effective backend is the step value if set, otherwise
  `ctx.AutonomousBackend`. Resolution must also report where the backend came from, so it can tell a
  required step-level request from an inherited preference. For a required request that can't be met
  (no TTY, or external-user mode), resolution returns an error that fails the step before launch.
  Today the function can only return a context, so its signature changes. `ResolveAgentInvocationContext`
  and every other caller pass the step through, so system-prompt enrichment, disallowed tools, and
  permission flags follow the effective context automatically.
- **Precedence:** step field > user setting > default (`headless`). `capture` with a step-level
  interactive request is rejected at load time. Capture with an inherited interactive setting still
  forces headless. No-TTY and external-user fallbacks apply only to inherited settings; step-level
  requests fail instead.
- **Scope of inheritance:** the field applies to the step it is written on. It does not cascade into
  group, loop, or sub-workflow children. Nested agent steps set it themselves.

Alternatives considered:

- **New mode value** such as `mode: autonomous-interactive`. Rejected: `mode` is also resolved from
  profile `default_mode` and is checked in many places. A third mode would spread through validation,
  session, and UI code for what is really a backend choice within autonomous mode.
- **Boolean** such as `interactive_backend: true`. Rejected: it can't express "force headless" or
  "interactive only for Claude", and it diverges from the user-setting vocabulary.
- **Agent-profile-level backend.** Deferred: profiles are user-owned configuration, so this would
  repeat the problem the issue is about. The workflow should own the requirement.

## Out of Scope

- Adopting the field in the orchestrated implement-change workflow. That workflow lives in the
  still-open PR #209; the adoption is a one-line YAML change there or a follow-up after it merges.
- Changing the fallback for steps that only inherit the global setting. Detecting aborted native
  sub-agents under the headless backend is also out of scope.
- Supporting `capture` on autonomous-interactive steps (existing TODO in `agent.go`).
- Workflow-level or run-level backend defaults, CLI flags, and agent-profile backend fields.
- Changes to the user settings file format or settings editor.

## Impact

- **Code:** `internal/model/step.go` (field and validation), `internal/exec/agent.go` (resolution),
  and possibly `internal/validate` or `internal/prevalidate` if they enumerate step fields. Tests sit
  next to each package.
- **Specs:** delta specs for `step-model`, `cli-adapter`, and `external-user-mode`.
- **Docs:** workflow step reference, plus the `autonomous_backend` entry in `docs/agent-profiles.md`
  noting the step override.
- **Users:** workflow authors get a new optional field. A workflow that sets an interactive backend on
  a step can't complete that step without a TTY or in external-user mode. That includes unattended
  runners that use `--external-user` or `--headless` without a terminal; they get a clear failure
  instead of a silently incomplete step. Authors adopting the field should account for this. End users' global setting keeps applying to
  every step that doesn't set the field. Persisted run state and audit formats are unchanged.
