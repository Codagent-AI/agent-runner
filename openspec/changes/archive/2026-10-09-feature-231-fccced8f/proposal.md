## Why

When `autonomous_permission_mode` is `yolo`, the Claude adapter launches every autonomous step with
`--permission-mode bypassPermissions`, including steps on the autonomous-interactive backend: Claude
runs in the user's terminal. On 2026-10-09 an `orchestrate-implementation` step ran this way
(`claude --resume <id> --permission-mode bypassPermissions ...`). Messages between that session and
another Claude session were held for approval and expired without being delivered.

Claude Code decides whether to accept a cross-session message from the permission-mode class of the
sender and the receiver. The class matters, not the exact mode name. Sessions that bypass permission
prompts form one class. Every other session, including `auto`, `acceptEdits` and `dontAsk`, forms the
other. When no `crossSessionInbound` setting applies, a message that crosses the two classes is held
for approval in either direction, and an unanswered hold expires after `dialogExpiry`, five minutes by
default. Ordinary interactive Claude sessions now start in `auto`, so a step in `bypassPermissions`
falls in the opposite class from the user's own sessions. Messages between them are held by default.
Workflows that rely on that coordination, such as an orchestrator talking to the user's session or to
an interactive peer, fail silently.

Claude Code's `auto` permission mode avoids that and still lets yolo steps run largely unattended. A
classifier approves routine actions without a prompt, and the session falls in the same
(non-bypass) class as ordinary interactive sessions. It is not a strict equivalent of
`bypassPermissions`. When the classifier blocks actions repeatedly, Claude Code goes back to permission
prompts in an interactive session. When `auto` is not available to the session, Claude Code starts it
in Manual mode instead. This proposal accepts that trade for this one combination (Claude, yolo,
autonomous-interactive backend): **best-effort autonomy, where a CLI permission prompt can still
occasionally appear and need manual intervention**. That is not the same as a human being present,
and nothing here assumes one. Steps on the autonomous-interactive backend stay autonomous: the runner
still blocks `AskUserQuestion` and still delivers the autonomy instructions as today (on fresh sessions), so the agent does not ask
clarifying questions. The only thing that can now need a human is the CLI's own permission prompt,
where `bypassPermissions` would have gone ahead.

Autonomous-headless steps keep `bypassPermissions`. In a `-p` run with no prompt tool, `auto` drops
blocked actions without telling anyone, which would quietly weaken headless yolo runs. The issue also
limits the change to the autonomous-interactive backend.

## What Changes

- When the Claude adapter runs in the autonomous-interactive context with
  `autonomous_permission_mode: yolo`, it emits `--permission-mode auto` instead of
  `--permission-mode bypassPermissions`.
- **Unavailable `auto`:** if `auto` is unavailable (unsupported model or provider, `disableAutoMode`,
  or turned off server-side), Claude starts in Manual and the step may wait on permission prompts. The
  adapter does not detect this and does not silently fall back to `bypassPermissions`. Users who need
  unattended runs must meet Claude Code's auto-mode requirements; the documentation states this along
  with the Manual fallback.
- The `cli-adapter` specification changes:
  - "Adapters honor autonomous permission mode": Claude's yolo flag differs by backend, `auto` on
    autonomous-interactive and `bypassPermissions` on autonomous-headless. The scenario saying yolo
    emits the same flag set on both backends changes to match.
  - The permission wording that describes both autonomous backends as unsupervised ("No permission
    loosening in interactive mode", the autonomous-interactive permission-grant scenario) is clarified.
    For Claude in yolo on the autonomous-interactive backend, autonomy is best-effort and a CLI
    permission prompt can still appear. These permission prompts are separate from agent clarification
    questions, which stay blocked: "AskUserQuestion blocked" and "Autonomy system prompt enrichment"
    are unchanged.
- User-facing copy: the YOLO option in the settings editor (`internal/settingseditor/editor.go`) says
  it bypasses per-command approval. It will say that Claude steps on the interactive backend use
  Claude's auto mode (classifier-reviewed approval with an occasional prompt) and that headless steps
  bypass approval. The `autonomous_permission_mode` documentation (`docs/agent-profiles.md`) is
  updated the same way.
- These cases do not change:
  - conservative mode, or no setting: `acceptEdits` on both autonomous backends;
  - autonomous-headless with yolo: `bypassPermissions`;
  - the external-user context;
  - interactive (non-autonomous) steps, which still get no loosening flags;
  - every other adapter.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-adapter`: "Adapters honor autonomous permission mode" changes so that, in yolo mode, Claude's
  autonomous-interactive invocation uses `--permission-mode auto` while autonomous-headless keeps
  `bypassPermissions`. The related wording about autonomous permissions and unsupervised operation is
  clarified for that combination: best-effort autonomy, where a CLI permission prompt may still appear
  and clarification questions stay blocked.

## Technical Approach

The flag change stays inside `ClaudeAdapter.BuildArgsWithError` (`internal/cli/claude.go`).
Permission-mode selection already branches on the invocation context and the resolved
`autonomous_permission_mode`. One more branch on `ContextAutonomousInteractive` picks `auto` over
`bypassPermissions` when the mode is yolo. `BuildArgsInput`, the user settings and the runner need no
changes, because the adapter already receives both inputs. The existing adapter tests in
`internal/cli/adapter_test.go` that expect `bypassPermissions` for autonomous-interactive yolo will be
updated, and new tests will pin the unchanged headless and conservative cases. The settings-editor
copy and the documentation change only text.

The same mapping covers fresh and resumed sessions. `--permission-mode` is already emitted on
`--resume`, which is how the observed session was launched.

**Assumption (decision-bearing): only the yolo mapping changes.** The issue asks for `auto`
"instead of `bypassPermissions`", and the adapter emits `bypassPermissions` only under yolo. Under
the default conservative setting, autonomous-interactive steps keep `acceptEdits`. `auto`
pre-approves more than `acceptEdits` does, and the conservative setting promises no broader authority
than the baseline. Raising it silently would break that promise. Users who want `auto` already opt in
through yolo.

**Decided policy for unavailable `auto`:** document the requirement and disclose Claude's Manual
fallback. Do not detect or override it. Detecting availability would mean probing Claude's
account, model and server state, which this repository cannot do reliably. Falling back to
`bypassPermissions` would bring back the permission-class mismatch this change removes, and the user
would not see it happen.

**What cross-session messaging this fixes.** The change removes the default approval barrier between
an autonomous-interactive yolo Claude step and peers in the non-bypass class: the user's ordinary
interactive sessions, other autonomous-interactive yolo steps, and steps in `acceptEdits`. Inbound
policy and Claude's normal message checks still apply. It does **not** fix pairings with peers that
remain in the bypass class:
- autonomous-headless yolo steps;
- steps that capture output, which `internal/exec/agent.go` forces onto the headless backend even
  when the user chose an interactive backend, so a single workflow can mix the two classes;
- any session the user started with `bypassPermissions`.

Messages between those peers and the changed step are still held by default. Users who need
coordination across backends can set `crossSessionInbound: accept` on the receiving session (for a
`-p` worker, through its `--settings`). The documentation will mention this as guidance. The runner
will not set it automatically, because overriding a user's inbound messaging policy is a security
choice that belongs to the user.

**Risks for design to resolve:**

- *How the step looks when it stalls.* Design should decide how a step waiting on a Claude permission
  prompt (classifier fallback or Manual start) shows up in the run view and audit log, if at all.
- *Concurrent change.* The in-flight change `make-pty-great` also edits the `cli-adapter` permission
  requirements. Its delta has to be reconciled with this one when either is archived.

## Out of Scope

- Changing the conservative or default mapping (`acceptEdits`) on any backend.
- Changing autonomous-headless or external-user permission flags, including the headless
  steps that capture forces.
- Detecting at runtime whether `auto` is available, or falling back to another mode automatically.
- Setting or overriding `crossSessionInbound`, `dialogExpiry`, or any other Claude messaging policy
  for the user.
- Changing the `AskUserQuestion` block or the autonomy preamble.
- Adding a new `autonomous_permission_mode` value, or a per-step or per-profile permission-mode
  override.
- Changing permission behavior for Codex, Copilot, Cursor or OpenCode.

## Impact

- Code: the `internal/cli/claude.go` permission-mode selection, the `internal/cli/adapter_test.go`
  Claude cases, and the YOLO description in `internal/settingseditor/editor.go`.
- Specs: `openspec/specs/cli-adapter/spec.md`, specifically the "Adapters honor autonomous permission
  mode" requirement and scenarios, plus the wording about autonomous-interactive permissions and
  unsupervised operation in "No permission loosening in interactive mode".
- Docs: `docs/agent-profiles.md` and any other documentation of `autonomous_permission_mode`. These
  cover Claude's auto requirements, the Manual fallback, and the `crossSessionInbound` guidance for
  coordination across backends.
- Users: yolo users whose Claude steps run on the autonomous-interactive backend get `auto` mode.
  Messages between those steps and non-bypass peers (including the user's ordinary sessions) are no
  longer held by default. Messages with headless yolo peers and bypass sessions still are. The step
  may occasionally wait on a Claude permission prompt, and does so routinely where `auto` is
  unavailable. No setting, persisted state or CLI flag changes.
