## ADDED Requirements

### Requirement: Resume model-override capability

Each registered adapter SHALL declare whether it applies a requested model when resuming an existing native session. Agent Runner SHALL use that declaration to decide whether to accept a follow-up's model override. An adapter that omits the model on resume MUST declare that it does not apply it, so an override is never accepted and then silently dropped. As of this change, Codex SHALL declare that it applies a resume model. Claude, Copilot, Cursor, and OpenCode SHALL declare that they do not.

#### Scenario: Codex applies a resume model
- **WHEN** a Codex resume invocation is built with a requested model
- **THEN** the adapter passes that model and declares resume model overrides as supported

#### Scenario: Claude omits a resume model
- **WHEN** a Claude resume invocation is built with a requested model
- **THEN** the adapter omits the model and declares resume model overrides as unsupported

#### Scenario: Declaration matches argument construction
- **WHEN** any registered adapter builds a resume invocation with a requested model
- **THEN** the model appears in the arguments if and only if the adapter declares resume model overrides as supported

### Requirement: Headless activity summaries

An adapter MAY provide activity summaries for a running autonomous-headless child by recognizing events in the child's structured stdout. The Claude and Codex adapters SHALL provide them. A recognized event SHALL be summarized by its event kind and, for tool activity, its tool name. The summary SHALL be one line of at most 200 characters. It MUST NOT contain message text, tool arguments, tool output, or the final response. Unrecognized or malformed lines SHALL be ignored without error. Summarizing activity MUST NOT alter captured stdout, output files, usage extraction, or session discovery. An adapter that provides no summaries leaves Agent Runner to fall back to output-recency summaries.

#### Scenario: Claude tool use is summarized
- **WHEN** a Claude headless child emits a stream event for a `Bash` tool use
- **THEN** the latest activity summary names the tool-use event and `Bash`, without the command text

#### Scenario: Codex tool activity is summarized
- **WHEN** a Codex headless child emits an item event for a command or tool execution
- **THEN** the latest activity summary names that event kind and the tool, without its arguments or output

#### Scenario: Malformed stream line is ignored
- **WHEN** a child writes a line the adapter cannot parse
- **THEN** the previous activity summary is kept and no error is raised

#### Scenario: Captured output is unchanged
- **WHEN** activity summaries are produced for a child
- **THEN** the child's captured stdout and output files are byte-identical to an execution without summaries

#### Scenario: Adapter without summaries
- **WHEN** a Copilot, Cursor, or OpenCode child is running
- **THEN** the adapter provides no activity summary and Agent Runner reports output recency instead
