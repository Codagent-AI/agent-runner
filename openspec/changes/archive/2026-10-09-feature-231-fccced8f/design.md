## Context

`ClaudeAdapter.BuildArgsWithError` (`internal/cli/claude.go`) picks the Claude permission mode inline:

```go
if context.IsAutonomous() || context == ContextExternalUser {
    permissionMode := "acceptEdits"
    if usersettings.EffectiveAutonomousPermissionMode(input.PermissionMode) == usersettings.PermissionModeYOLO {
        permissionMode = "bypassPermissions"
    }
    args = append(args, "--permission-mode", permissionMode)
}
```

The branch runs for the autonomous-headless, autonomous-interactive and external-user contexts, on both
fresh (`--session-id`) and resumed (`--resume`) sessions. The runner fills in `BuildArgsInput.PermissionMode` from
`ctx.AutonomousPermissionMode` (`internal/exec/agent.go`, around `PermissionMode:`), and
`resolveInvocationContext` decides the context. That function forces autonomous-headless whenever the
step captures output, even when the user picked an interactive backend. For every autonomous step,
`agent.go` also sets `DisallowedTools = ["AskUserQuestion"]`. It prepends the autonomy preamble only on
fresh sessions (`!isResume`). On resume it sends no `--append-system-prompt` and passes the current step
instructions and completion instruction in the user prompt; `agent_test.go` already pins that the preamble
is omitted on resume. None of those runner paths change.

User-facing text that describes YOLO:
- `internal/settingseditor/editor.go`: the YOLO option description, "Bypass per-command approval for
  shell, file, and network actions. Recommended only inside an external sandbox such as Docker."
- `internal/onboarding/native/native.go`: the YOLO option label, "YOLO - Pre-approve shell, file,
  and network actions; use only inside an external sandbox".
- `docs/agent-profiles.md`: the User Settings table lists `autonomous_permission_mode` values only.

The `user-settings-editor` and `native-setup` specs require only generic YOLO risk copy (broader
authority plus a sandbox recommendation), so changing the wording does not touch either contract.

Existing tests: `internal/cli/adapter_test.go` includes "yolo autonomous uses bypass permission mode"
(headless), "yolo interactive does not loosen permissions", and "yolo autonomous-interactive uses
bypass permission mode". The last one asserts the behavior this change replaces.
`internal/exec/agent_test.go` includes "capture forces headless even when autonomous backend is
interactive".

## Goals / Non-Goals

**Goals:**
- Claude, `yolo`, autonomous-interactive → `--permission-mode auto`, for fresh and resumed sessions.
- Every other Claude context and setting combination emits exactly what it emits today.
- Pin the unchanged neighbors with tests: headless yolo, capture-forced headless, conservative, interactive, external-user.
- YOLO copy and docs describe the per-backend Claude behavior, the requirement that Claude's auto mode be available, the Manual fallback, and `crossSessionInbound` guidance.

**Non-Goals:**
- Detecting whether Claude's auto mode is available, or falling back to another mode at runtime.
- Showing in the run view or audit log that a step is stalled on a Claude permission prompt.
- Changes to the runner, `BuildArgsInput`, settings schema, other adapters, or Claude messaging settings.

## Approach

### Adapter

Move the selection into a small pure helper in `internal/cli/claude.go` and call it from
`BuildArgsWithError`:

```go
// claudePermissionMode returns the --permission-mode value for an invocation,
// or "" when none is emitted (interactive context).
func claudePermissionMode(context InvocationContext, mode usersettings.AutonomousPermissionMode) string {
    if !context.IsAutonomous() && context != ContextExternalUser {
        return ""
    }
    if usersettings.EffectiveAutonomousPermissionMode(mode) != usersettings.PermissionModeYOLO {
        return "acceptEdits"
    }
    if context == ContextAutonomousInteractive {
        // Auto keeps the session in Claude's prompting permission class (the
        // class ordinary interactive sessions use), so cross-session messages
        // aren't held by default. Headless keeps bypassPermissions because a
        // -p run has no prompt to fall back to.
        return "auto"
    }
    return "bypassPermissions"
}
```

`BuildArgsWithError` appends `--permission-mode <value>` when the helper returns a non-empty value.
Argument order stays the same (after `--effort` and before `-p`), so existing tests that compare exact
argument slices keep passing. Also update the "Patterns" doc comment on `BuildArgs` to say that
autonomous-interactive yolo uses `--permission-mode auto`.

No error path is added. When Claude rejects the flag or auto mode is unavailable, Claude handles it:
it starts in Manual mode or raises a CLI error. The step's existing launch and failure handling
covers both, as it does for any Claude startup problem today.

### Copy

- Settings editor YOLO description: say that YOLO pre-approves shell, file and network actions, that
  Claude steps on an interactive backend use Claude's auto mode (classifier-reviewed, and they may
  occasionally prompt), and keep the external-sandbox recommendation. Keep it to one or two short
  sentences that fit the existing panel.
- Native onboarding YOLO label: keep "Pre-approve shell, file, and network actions; use only inside an
  external sandbox". It is still accurate and the label space is tight. No change is required; a
  wording tweak in the same spirit is acceptable.

### Docs

Add an "Autonomous permission mode" subsection under User Settings in `docs/agent-profiles.md` with a
small table of Claude's emitted mode:

| Setting | Autonomous-headless (incl. capture steps) | Autonomous-interactive |
| --- | --- | --- |
| `conservative` | `acceptEdits` | `acceptEdits` |
| `yolo` | `bypassPermissions` | `auto` |

followed by short notes:
- `auto` requires Claude's auto-mode availability (supported model, provider and plan; not disabled by `disableAutoMode` or server-side). When it is unavailable, Claude starts in Manual and the step may wait on permission prompts. Agent Runner does not substitute another mode.
- Even when available, `auto` can fall back to prompting after repeated classifier blocks. Autonomy is best-effort; clarifying questions remain blocked.
- Cross-session messaging: an `auto` step and the user's ordinary sessions are in the same permission class, so messages between them are delivered by default. Headless yolo steps and capture steps stay in the bypass class. For coordination across that boundary, set `crossSessionInbound: accept` on the receiving session (for a `-p` worker, through its `--settings`).

## Decisions

1. **Use a pure helper instead of nesting another branch inline.** The mapping is now two-dimensional
   (context × setting). A helper makes the mapping readable and lets a table-driven test cover every
   cell. The alternative, inline nested ifs, works but hides the matrix.
2. **Branch on `ContextAutonomousInteractive` explicitly.** That leaves external-user and headless on
   `bypassPermissions` without having to reason about `IsHeadless()`. External-user counts as
   "headless" in `IsHeadless()` but is not autonomous, so an explicit check is clearer than using
   `!IsHeadless()`.
3. **No availability probe and no fallback.** This was decided in the proposal (PR-001). Probing would
   mean spawning Claude or reading account and server state the runner does not own, and a fallback to
   `bypassPermissions` would bring back the mismatch without the user seeing it.
4. **No stall visibility work.** A Claude permission prompt in an autonomous-interactive session is
   on the user's own terminal, which the runner hands over directly. The runner cannot reliably
   detect the prompt, and adding detection is a separate feature. The docs disclose the behavior.
5. **Leave the onboarding label alone.** Its current wording is still true, and the spec only requires
   generic risk copy. The settings editor description gets the per-backend detail because it is the
   longer explanatory text.

## Risks / Trade-offs

- **Older Claude CLIs without `auto`.** A Claude Code build that predates `auto` rejects the flag at
  startup, and the step fails at launch with Claude's error. The issue reporter's CLI (2.1.296) and
  current builds accept it. We accept this rather than adding version detection. The docs can note
  the requirement.
- **Steps that used to run unattended may now wait at the terminal.** This is accepted by the
  proposal and disclosed in the docs and the settings copy.
- **Messaging still holds across permission classes** (headless yolo and capture steps). This is out of
  scope by decision, and the docs give `crossSessionInbound` guidance.
- **Merge overlap with `make-pty-great`.** That change edits the same `cli-adapter` requirements. Its
  delta was written before this change and still says yolo applies the same flag set on both backends.
  Whichever change archives second must merge the requirement text. The scenarios for this change are
  self-contained, which keeps that merge mechanical.

## Verification

Use TDD in `internal/cli/adapter_test.go`:
- Replace "yolo autonomous-interactive uses bypass permission mode" with a test asserting
  `--permission-mode auto` and the absence of `bypassPermissions`.
- Add a table-driven test of the helper's outputs (or the `BuildArgs` permission flag) across
  {interactive, autonomous-headless, autonomous-interactive, external-user} × {"", conservative, yolo}:
  interactive → none; conservative or empty → `acceptEdits`; yolo → `bypassPermissions` everywhere
  except autonomous-interactive, which gets `auto`.
- Add a resume case: `Resume: true, SessionID: "id"`, autonomous-interactive, yolo →
  `--permission-mode auto`. This test covers adapter args only: the runner does not re-inject the
  autonomy preamble on resume, and that existing behavior stays as it is.
- Add an autonomous-interactive yolo case with `DisallowedTools: ["AskUserQuestion"]` asserting that
  `--disallowedTools AskUserQuestion` and `--permission-mode auto` are both present. This shows that
  the adapter keeps the autonomy contract alongside `auto`.

In `internal/exec/agent_test.go`:
- Extend or add a test next to "capture forces headless even when autonomous backend is interactive".
  With `ctx.AutonomousPermissionMode = "yolo"` and `AutonomousBackend = "interactive-claude"` (or
  `interactive`), assert that the spawned Claude args contain `--permission-mode bypassPermissions`
  and `-p`. Inspect the args through the existing `mockRunner`'s recorded invocations.

The existing exec tests that cover `AskUserQuestion` blocking and autonomy preamble injection for
autonomous-interactive steps stay unchanged and must keep passing.

Run `go test ./internal/cli ./internal/exec ./internal/settingseditor ./internal/onboarding/...`
while iterating, then `make test` and `make lint`. If the settings-editor test pins the YOLO
description text, update it with the copy change.

## Migration Plan

There is no data or settings migration. The change takes effect on the next Claude launch for
autonomous-interactive yolo steps, including resumed sessions. To roll back, revert the adapter
change: the helper's yolo branch for autonomous-interactive returns `bypassPermissions` again.
