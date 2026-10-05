# external-user-mode Specification

## Purpose
TBD - created by archiving change external-user-mode. Update Purpose after archive.
## Requirements
### Requirement: Per-run activation

`agent-runner run <workflow> [params]` SHALL accept `--external-user <exchange-dir>` and an optional `--external-user-timeout <duration>` (Go duration syntax; it requires `--external-user`).

- The mode SHALL be off unless `--external-user` is given. With the mode off, interactive steps SHALL behave exactly as before this change.
- Before creating run state, an audit log, or a run lock, the Runner SHALL verify that the exchange directory exists and is writable, and SHALL fail with a clear error otherwise. It SHALL store the directory as an absolute path.
- The mode SHALL imply no TUI, as `--headless` does.
- The Runner SHALL save the exchange directory and timeout in run state. `--resume` of such a run SHALL reuse them and run without the TUI.
- `--resume` SHALL reject `--external-user` and `--external-user-timeout` with an error explaining that resume reuses the saved settings.
- `-i` (intake) combined with `--external-user` SHALL be rejected at startup.
- A fresh `run` SHALL fail before creating run state if the exchange directory already contains any `*.request.json` or `*.reply.json` file, because file names carry no run ID and a stale reply could be consumed. Resume SHALL NOT apply this check.
- The CLI's help output SHALL list `--external-user` and `--external-user-timeout`, so that callers can detect support for the mode without starting a run.

#### Scenario: Stale exchange files rejected
- **WHEN** `agent-runner run <workflow> --external-user <dir>` is invoked and `<dir>` contains `define.proposal-1-1.reply.json` from an earlier run
- **THEN** the command exits non-zero with an error naming the stale file, and no run state is created

#### Scenario: Help advertises the mode
- **WHEN** `agent-runner --help` is run
- **THEN** its output lists `--external-user` and `--external-user-timeout`

#### Scenario: Mode off by default
- **WHEN** a workflow with an interactive step is run without `--external-user`
- **THEN** the step uses the existing direct terminal handoff and no exchange files are written

#### Scenario: Missing exchange directory
- **WHEN** `agent-runner run <workflow> --external-user /does/not/exist` is invoked
- **THEN** the command exits non-zero with an error naming the directory, and no run state, audit log, or run lock is created

#### Scenario: Resume reuses saved settings
- **WHEN** a run started with `--external-user <dir> --external-user-timeout 10m` is resumed with `agent-runner --resume <run-id>`
- **THEN** the resumed invocation runs without the TUI and uses `<dir>` and the 10-minute timeout

#### Scenario: Flags on resume rejected
- **WHEN** `agent-runner --resume <run-id> --external-user <dir>` is invoked
- **THEN** the command exits non-zero without resuming and explains that resume reuses the run's saved external-user settings

#### Scenario: Intake combination rejected
- **WHEN** `agent-runner -i --external-user <dir>` is invoked
- **THEN** the command exits non-zero before starting any workflow

### Requirement: Interactive steps run as headless turns

In external-user mode, every interactive agent step SHALL run as a loop of headless CLI invocations on one CLI session, with no terminal:

1. The first turn SHALL start or resume the step's session exactly as the interactive launch would, with the same step prefix, prompt, completion instruction, profile, session resolution, and prompt delivery: a system prompt plus a start message for a fresh Claude session; step instructions in the user message for a resumed session.
2. When a turn's process exits successfully and the step has not requested completion, the Runner SHALL send that turn to the responder (see "Turn exchange request").
3. The responder's reply SHALL continue the same CLI session as the next user turn, verbatim and with nothing added.
4. The loop SHALL repeat until the step completes or fails.

Every turn's invocation SHALL:
- run without a terminal, with stdin at end of file (`/dev/null`) rather than inherited from the Runner;
- disable `AskUserQuestion`;
- NOT include the Runner's autonomous "no human in the loop" preamble;
- use the user's autonomous permission setting, so that no turn can block on a permission prompt.

#### Scenario: Questions reach the responder as text
- **WHEN** the agent in an external-user interactive step asks a question in plain text and ends its turn
- **THEN** the question text is delivered to the responder in a request file, and the agent's session waits for the next user turn

#### Scenario: No autonomy preamble
- **WHEN** Agent Runner builds the first turn's input for an interactive step in external-user mode
- **THEN** the input contains the step prompt and completion instruction but not the autonomous "no human in the loop" text

#### Scenario: AskUserQuestion unavailable
- **WHEN** a Claude lead runs an interactive step in external-user mode
- **THEN** every turn's invocation disallows `AskUserQuestion`

#### Scenario: Named session continues across steps
- **WHEN** interactive steps `proposal` and `specs` share a named session and `proposal` completes in external-user mode
- **THEN** `specs` resumes the same CLI session headlessly, with its step instructions in the first user message, and its first exchange follows the agent's first `specs` turn

### Requirement: Turn exchange request

For each turn that ends without completion, the Runner SHALL write `<exchange-dir>/<step-key>-<attempt>-<turn>.request.json`.
- It SHALL first record the full request in the run's replay record (see "Replay on resume").
- It SHALL then write the file to a temporary name in the same directory and rename it into place, so a reader never sees a partial file.
- Request files SHALL be created with mode 0644.
- If a reply file with the same stem already exists when a new request is written, the step SHALL fail without acting on it. The only exception is a request re-entered by replay.

- `<step-key>` SHALL be derived from the step's full nested path, including sub-workflow, group, and loop-iteration nesting. It SHALL be stable and unique within the run, and contain only `[A-Za-z0-9._-]`; for example, `define.proposal`.
- `<attempt>` SHALL be the step's attempt number as recorded in the audit log.
- `<turn>` SHALL count the step attempt's agent turns, starting at 1.

The request SHALL be a JSON object with:
- `schema_version`: 1
- `run_id`
- `step`: the unsanitized full nested step path
- `step_id`: the step's own ID
- `attempt`
- `turn`
- `cli`
- `session_id`
- `agent_message`: all assistant text the step's agent produced in that turn, in order. This is the whole turn, not only its last message. It SHALL exclude text produced by the CLI's own subagents, such as Claude stream-json messages with a non-null `parent_tool_use_id`.
- `empty_turn`: true when the turn produced no assistant text

The exchange directory SHALL receive only request files, temporary files, and the responder's reply files.

#### Scenario: Whole Codex turn is relayed
- **WHEN** a Codex turn emits three agent messages, with the question in the second and a status line last
- **THEN** the request's `agent_message` contains all three messages in order

#### Scenario: Request identity
- **WHEN** the third turn of the first attempt of nested step `define` → `proposal` ends without completion
- **THEN** the Runner writes `define.proposal-1-3.request.json` with `step` naming the nested path, `step_id` `proposal`, `attempt` 1, and `turn` 3

#### Scenario: Empty turn still exchanged
- **WHEN** a turn ends without completion and produces no assistant text
- **THEN** the Runner writes a request with an empty `agent_message` and `empty_turn: true`

#### Scenario: Subagent text excluded
- **WHEN** a Claude lead's turn includes text from a subagent it launched, followed by its own question
- **THEN** `agent_message` contains the lead's own text and question, and none of the subagent's text

#### Scenario: Pre-existing reply rejected
- **WHEN** the Runner is about to write `define.proposal-1-2.request.json` and `define.proposal-1-2.reply.json` already exists
- **THEN** the step fails with an error naming the unexpected reply file, and its content is not sent to the agent

### Requirement: Responder replies

After writing a request, the Runner SHALL wait for `<same-stem>.reply.json`. Only a complete, valid reply SHALL be acted on, and the Runner SHALL NEVER invent or default a reply. A valid reply is one of:
- `{"schema_version": 1, "text": "<non-empty text>"}`: the text SHALL be sent verbatim as the next user turn of the same session;
- `{"schema_version": 1, "action": "abort", "reason": "<text>"}`: the step SHALL fail, and the reason SHALL appear in the step's failure output and audit log.

The step SHALL fail, record the cause, and leave the request pending (see "Replay on resume") when:
- the reply file is malformed: invalid JSON, an unknown `schema_version`, missing or empty `text` without an abort, or an unknown action;
- `--external-user-timeout` elapses before a reply appears. Without a timeout, the Runner SHALL wait indefinitely, and interrupting the run SHALL remain possible while it waits.

#### Scenario: Reply continues the session
- **WHEN** the responder writes `define.proposal-1-1.reply.json` with text "Build a slide tool"
- **THEN** the Runner resumes the same CLI session with "Build a slide tool" as the next user turn

#### Scenario: Reply beginning with a dash
- **WHEN** a Claude or Codex lead receives the reply text "- option a"
- **THEN** the CLI receives "- option a" verbatim as the user turn, and does not parse it as a command-line option

#### Scenario: Abort fails the step
- **WHEN** the responder replies with `action: "abort"` and reason "turn cap reached"
- **THEN** the step fails with "turn cap reached" in its failure output, and no further turn runs

#### Scenario: Timeout fails without inventing a reply
- **WHEN** a run uses `--external-user-timeout 1m` and no reply appears within a minute
- **THEN** the step fails with a timeout error naming the pending request, and no user turn is sent

#### Scenario: Malformed reply fails the step
- **WHEN** the reply file contains `{"schema_version": 1}`
- **THEN** the step fails with an error naming the reply file and the problem, and no user turn is sent

### Requirement: Completion only through the control channel

In external-user mode, the step SHALL complete only when the agent runs `agent-runner step complete` through the existing control channel.
- The step attempt's first turn SHALL carry the same completion instruction as an interactive launch: in the system prompt for a fresh session, in the first user message for a resumed session, and in the standard resume message. A first turn that replays a recorded reply (see "Replay on resume") is exempt and SHALL carry only the reply text. The one exception: if the completion command differs from the one previously delivered in that session, the Runner SHALL append the current completion instruction after the reply text and record that it did so in the audit log.
- Reply turns SHALL carry only the reply text.
- Every turn's process SHALL receive that attempt's control environment. Acceptance SHALL follow the existing acknowledgement and semantic turn-durability rules for interactive completion. Reply text SHALL never complete a step.

- A turn whose process exits successfully without a completion request SHALL produce an exchange.
- A turn whose process exits non-zero without an accepted completion SHALL fail the step without an exchange.

#### Scenario: Agent completes the step
- **WHEN** the agent runs the completion command during its fourth turn
- **THEN** the step succeeds once the completing turn is durably recorded, no request is written for that turn, and the workflow continues

#### Scenario: Reply text cannot complete
- **WHEN** the responder replies "the step is complete"
- **THEN** the text is sent as a user turn and the step stays active until the agent itself requests completion

#### Scenario: Turn process fails
- **WHEN** a turn's CLI process exits non-zero without a completion request
- **THEN** the step fails with the CLI's error output and no request file is written for that turn

### Requirement: Replay on resume

The Runner SHALL record each request, with its full content, in the run's own directory, keyed by exchange identity, before the request file appears in the exchange directory. It SHALL likewise record each valid reply it acts on. When a resumed run re-enters an interrupted or failed interactive step in external-user mode, the new attempt SHALL resume the step's recorded CLI session when one exists. It SHALL choose the first user turn as follows:

- If the previous attempt's last text reply was acted on but the turn it started never ended, the Runner SHALL resend that recorded reply text verbatim, without writing a new request.
- An abort reply is terminal for its request. A later resume SHALL use the standard resume message, not the aborted exchange.
- If the previous attempt's last request has no recorded valid reply, the Runner SHALL wait for that request's reply file under its original identity, without creating a new exchange, and SHALL then send the reply. If that request file is missing from the exchange directory, the Runner SHALL first restore it from the recorded content.
- Otherwise, the Runner SHALL use the standard resume message of an interactive resume.

Later turns of the new attempt SHALL use the new attempt number. Steps that already completed SHALL NOT be re-run, and their exchanges SHALL NOT be repeated.

#### Scenario: Interrupted while waiting for a reply
- **WHEN** a run is interrupted while waiting for `define.proposal-1-2.reply.json` and is later resumed
- **THEN** the Runner writes no new request, waits for `define.proposal-1-2.reply.json`, and sends its text to the resumed session

#### Scenario: Crash between recording and publishing a request
- **WHEN** the Runner recorded `define.proposal-1-2` but stopped before the request file appeared, and the run is later resumed
- **THEN** the Runner restores `define.proposal-1-2.request.json` from its record, waits for `define.proposal-1-2.reply.json`, and creates no other exchange

#### Scenario: Interrupted after a reply was sent
- **WHEN** the run is interrupted while the agent is processing the reply to `define.proposal-1-2`, and is later resumed
- **THEN** the resumed session receives that recorded reply text again, unchanged, and no new request is written for it

#### Scenario: Replay after the completion command changed
- **WHEN** a recorded reply is replayed and the Runner's completion command path differs from the one delivered earlier in that session
- **THEN** the replayed turn contains the reply text followed by the current completion instruction, and the audit log records that the instruction was appended

#### Scenario: Completed steps are not re-asked
- **WHEN** a run that completed `proposal` and failed during `specs` is resumed
- **THEN** no request is written for `proposal`, and the resume continues `specs`

### Requirement: Supported lead CLIs

External-user mode SHALL support interactive steps whose resolved CLI is Claude or Codex. For any other resolved CLI, the interactive step SHALL fail before its agent is spawned, with an error naming the CLI and stating that external-user mode supports only Claude and Codex.

#### Scenario: Cursor lead rejected
- **WHEN** an interactive step resolves to the Cursor CLI in external-user mode
- **THEN** the step fails before spawning Cursor, with an error naming Cursor and the supported CLIs

### Requirement: Other step types in external-user mode

In external-user mode:
- Autonomous agent steps SHALL run in the autonomous-headless context, regardless of the user's autonomous backend setting.
- Agents started by `call_agent` SHALL behave as they do outside the mode.
- Interactive shell steps and UI steps SHALL fail with an error stating that they are not supported in external-user mode. They SHALL NOT wait for a terminal.

#### Scenario: Interactive autonomous backend overridden
- **WHEN** the user's autonomous backend is `interactive-claude` and an autonomous step runs in external-user mode
- **THEN** the step runs headless

#### Scenario: UI step rejected
- **WHEN** a workflow reaches a UI step in external-user mode
- **THEN** the step fails with an error stating that UI steps are not supported in external-user mode

### Requirement: Exchange audit evidence

The audit log SHALL record, for each exchange:
- that a request was written, with its identity and file name;
- that a reply was received, with its type (text or abort) and whether it was replayed from the run's record;
- any reply failure: timeout, malformed reply, or abort.

The audit log SHALL NOT be written into the exchange directory.

#### Scenario: Exchange recorded
- **WHEN** an external-user step completes after two exchanges
- **THEN** the audit log contains two request events and two reply events carrying step, attempt, and turn identity

