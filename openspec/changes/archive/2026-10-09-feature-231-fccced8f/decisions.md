# Decisions

## propose

### Verdict: go with caveats
- **Decision:** Proceed. The change is small and contained in one adapter, and it fixes an observed
  failure in cross-session messaging.
- **Alternatives considered:** No-go, leaving yolo as `bypassPermissions` everywhere and documenting
  the mismatch. Rejected: it leaves cross-session coordination broken for the backend that has a human present.
- **Decision-bearing:** yes

### Only the yolo mapping changes for autonomous-interactive
- **Decision:** On the autonomous-interactive backend with `autonomous_permission_mode: yolo`, Claude
  gets `--permission-mode auto`. Conservative/default stays `acceptEdits`.
- **Alternatives considered:** Always use `auto` on autonomous-interactive, regardless of the setting.
  Rejected: the issue says "instead of `bypassPermissions`", which is emitted only under yolo, and
  raising conservative users to `auto` would grant more authority than their setting promises.
- **Decision-bearing:** yes. This is the reading of the issue we chose and is recorded as an
  assumption in proposal.md.

### Autonomous-headless keeps bypassPermissions
- **Decision:** Leave headless yolo unchanged.
- **Alternatives considered:** Use `auto` for headless as well. Rejected: the issue limits itself to
  the autonomous-interactive backend, and a headless session has no human to answer `auto`'s
  fallback prompts.
- **Decision-bearing:** no. The issue states this scope explicitly.

### No new setting or per-step override
- **Decision:** Reuse the existing `autonomous_permission_mode` values and add no configuration surface.
- **Alternatives considered:** A new `auto` setting value, or a per-profile permission mode.
  Rejected as scope creep: neither is needed to fix the issue.
- **Decision-bearing:** no

### Availability of auto mode deferred to design
- **Decision:** Record that `auto` may be unavailable on some accounts, plans or models as a design
  risk, and do not stop on it.
- **Alternatives considered:** Stop for a direction decision. Rejected: the fix stays inside this
  repository, and fallback behavior is a design-level choice.
- **Decision-bearing:** no

## proposal-review

### PR-001: the "human present" premise conflicts with the autonomy contract (applied)
- **Decision:** Applied. Checked against `cli-adapter` spec (unsupervised autonomous backends, the
  `AskUserQuestion` block, the autonomy prompt), `internal/exec/agent.go` autonomyPreamble, and the
  Claude permission-modes docs: unavailable `auto` starts in Manual, and repeated classifier blocks
  bring prompts back. The proposal now:
  - adopts best-effort autonomy, with possible manual permission intervention, for the
    Claude/yolo/autonomous-interactive combination instead of inferring a human from a TTY;
  - separates CLI permission prompts from agent clarification questions and keeps the
    `AskUserQuestion` block and the autonomy preamble;
  - brings the related spec wording, the settings-editor YOLO copy and `docs/agent-profiles.md`
    into scope;
  - decides the unavailable-`auto` policy (document the requirement and disclose the Manual
    fallback; no detection and no silent `bypassPermissions` fallback).
- **Alternatives considered:** Silently fall back to `bypassPermissions` when `auto` is unavailable
  (rejected: it brings back the class mismatch without the user seeing it). Probe availability at
  runtime (rejected: depends on account, model and server state outside this repository). Keep the
  "human present" rationale (rejected: contradicts the existing spec).
- **Decision-bearing:** yes. Best-effort autonomy and the unavailable-`auto` policy are product-level
  choices, made inside this repository and consistent with the issue. Not direction-level.

### PR-002: the cross-session messaging fix was overstated (applied)
- **Decision:** Applied. Checked against the Claude cross-session messaging docs: by default the
  inbound decision depends on permission classes (bypass versus everything else, with `auto`,
  `acceptEdits` and `dontAsk` in the non-bypass class), not on matching mode names. Also checked the
  capture-forced headless path in `internal/exec/agent.go`. The proposal now explains the rationale in
  terms of classes and limits the promised outcome to non-bypass peers, subject to inbound policy. It
  states that headless yolo peers, steps forced to headless by capture, and bypass sessions are still
  held, and it documents `crossSessionInbound: accept` on the receiver as user guidance.
- **Alternatives considered:** Automatically set `crossSessionInbound` for spawned steps (rejected:
  overrides the user's messaging policy, which is a security choice). Extend `auto` to headless
  (rejected: outside the issue's scope, and with no prompt tool, `-p` `auto` silently drops blocked
  actions).
- **Decision-bearing:** no. It corrects the rationale and the stated scope; the behavior is unchanged.

## spec

### Only cli-adapter gets a delta spec
- **Decision:** Write one delta, `specs/cli-adapter/spec.md`, which modifies "No permission loosening
  in interactive mode" and "Adapters honor autonomous permission mode".
- **Alternatives considered:** Also modify `user-settings-editor` and `native-setup` for the YOLO copy.
  Rejected: their scenarios require only generic risk copy (broader authority, sandbox
  recommendation). The reworded copy still satisfies them, so the copy change is implementation and
  docs, not a contract change.
- **Decision-bearing:** no

### The unavailable-auto scenario is specified at the runner boundary
- **Decision:** The spec requires that the runner emit `auto` without probing availability and never
  retry or substitute another mode. Claude's own fallback to Manual is described, not asserted, because
  the runner does not control it.
- **Alternatives considered:** Leave it to design with a deferred-to-design marker. Rejected: the
  proposal already decided the policy.
- **Decision-bearing:** no. It follows the PR-001 decision.

### External-user context pinned as unchanged
- **Decision:** Add a scenario saying external-user Claude args never gain `auto` and keep whatever
  mode they emitted before.
- **Alternatives considered:** Assert a specific mode. Rejected: external-user permission behavior is
  owned by another spec, and this change must not redefine it.
- **Decision-bearing:** no

### Capture-forced headless scenario added
- **Decision:** Pin that a capture step resolves to autonomous-headless and keeps `bypassPermissions`
  even when the user picks interactive Claude, the mixed-class case named in the proposal.
- **Alternatives considered:** None. It makes an existing behavior that the proposal relies on explicit.
- **Decision-bearing:** no

## design

### Use a pure claudePermissionMode helper
- **Decision:** Pull the (context × setting) mapping into a helper in `claude.go` and test it with a table.
- **Alternatives considered:** Nest another inline branch. Rejected: it hides the two-dimensional mapping.
- **Decision-bearing:** no

### Branch explicitly on ContextAutonomousInteractive
- **Decision:** Only `ContextAutonomousInteractive` gets `auto`. External-user and headless keep `bypassPermissions`.
- **Alternatives considered:** Key off `!IsHeadless()`. Rejected: external-user counts as headless but is not autonomous, so the intent is less clear.
- **Decision-bearing:** no

### No stall detection for Claude permission prompts
- **Decision:** Do not change the run view or audit log. Disclose the behavior in the docs instead.
- **Alternatives considered:** Detect prompts in the terminal handoff. Rejected: unreliable, and a separate feature.
- **Decision-bearing:** no. It settles the proposal's design risk inside the agreed scope.

### Keep the onboarding YOLO label; update the settings-editor description
- **Decision:** Only the settings-editor description gains per-backend detail. The onboarding label stays because it is still accurate.
- **Alternatives considered:** Rewrite both. Rejected: the label space is tight and no spec requires it.
- **Decision-bearing:** no

### Do not detect the Claude CLI version
- **Decision:** Accept that Claude CLIs without `auto` fail at launch, and note the requirement in the docs.
- **Alternatives considered:** Probe `claude --help` or the version. Rejected: adds startup cost and complexity for builds older than current.
- **Decision-bearing:** no

## test-plan

### Two exec-level integration obligations, no E2E
- **Decision:** INT-001 (direct interactive launch args: auto mode, AskUserQuestion block, autonomy preamble, resume) and INT-002 (headless and capture-forced headless keep bypassPermissions), using the existing `interactiveRunnerFn` and `mockRunner` seams.
- **Alternatives considered:** A PTY smoke E2E with a fixture CLI. Rejected: it would assert the same argv as INT-001 at higher cost and with more flakiness.
- **Decision-bearing:** no

### HT-001 for a real cross-session messaging check
- **Decision:** Keep one human-only check, a live autonomous-interactive Claude run receiving a message from an ordinary session.
- **Alternatives considered:** Drive it from a synthetic PTY. Rejected: repository guidance says starting runs and live agent conversations need a human at a real terminal.
- **Decision-bearing:** no

### Acceptance envelope protects user settings and other sessions
- **Decision:** Allow a few cheap real `claude` calls and a backed-up, restored change to `autonomous_permission_mode`. Forbid editing `~/.claude` settings and messaging other live sessions.
- **Alternatives considered:** Allow toggling `disableAutoMode` to exercise the Manual fallback. Rejected: it changes the user's global Claude configuration, and the scenario is defined at the argument level.
- **Decision-bearing:** no

## approach-review

### AR-001: the resume path does not re-inject the autonomy preamble (applied)
- **Decision:** Applied. Confirmed in `internal/exec/agent.go`: the preamble is added only when
  `!isResume`, resume sends no `--append-system-prompt`, and `agent_test.go` already pins that the
  preamble is omitted on resume. Changes made:
  - the spec scenario is split into a fresh-session version (system prompt begins with the preamble)
    and a resumed-session version (AskUserQuestion blocked, no re-injection, existing resume prompt
    and completion instruction);
  - the requirement text now says autonomy instructions are delivered "as today";
  - design.md describes the fresh-only injection;
  - INT-001's assertions are split per case, and the resume case asserts `--resume`, `auto`,
    AskUserQuestion, no `--append-system-prompt`, the completion instruction, and no preamble.
- **Alternatives considered:** Change the runner to re-inject the preamble on resume. Rejected: it
  contradicts the design's "no runner change" goal and the existing pinned behavior, and it is
  unrelated to the issue.
- **Decision-bearing:** no. This corrects the artifacts to match existing behavior.

### AR-002: HT-001 did not control for inbound policy or name its peers (applied)
- **Decision:** Applied. HT-001 now uses two dedicated, test-owned sessions in scratch directories,
  and the envelope exempts only those sessions. Before sending, the tester records both sessions'
  modes and checks that no `crossSessionInbound` applies to the receiver (managed, user, project,
  `--settings`). Any applicable or unexcludable override, or auto mode being unavailable, makes the
  result blocked or inconclusive rather than pass or fail. The receiver is kept alive past the
  default `dialogExpiry`, both sessions are cleaned up and runner settings restored, and the live
  observation is reported separately from INT-001.
- **Alternatives considered:** Set `crossSessionInbound` explicitly for the test. Rejected: that tests
  the override rather than the default class behavior this change fixes, and it would change global
  Claude settings.
- **Decision-bearing:** no

## tasks

### Single implementation task
- **Decision:** One task covers the adapter helper, the copy and docs, the unit tests, and INT-001/INT-002. HT-001 is excluded because it is human-only.
- **Alternatives considered:** Separate tasks for code, copy and docs. Rejected: the workflow requires exactly one task, and the change is small.
- **Decision-bearing:** no
