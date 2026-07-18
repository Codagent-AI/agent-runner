# interactive-shell-steps Specification

## Purpose
Define direct-terminal interactive shell steps, piped autonomous shell defaults, and validation constraints.
## Requirements
### Requirement: Interactive mode attribute on shell steps

A shell step MAY set `mode: interactive` to request direct execution with the user's terminal attached. When `mode` is absent, the shell step SHALL default to `autonomous`. When `mode` is `autonomous` (explicit or defaulted), the shell step SHALL execute through the piped shell path. The `mode` value MUST be `interactive` or `autonomous`; any other value SHALL fail validation.

Unlike agent steps — where `mode` defaults come from the resolved profile's `default_mode` — shell steps have no profile, so the default is always `autonomous`.

#### Scenario: Shell step with mode: interactive

- **WHEN** a workflow declares a shell step with `command: "read -p 'Name? ' name && echo Hi $name"` and `mode: interactive`
- **THEN** validation succeeds and the runner dispatches the step through the interactive shell path

#### Scenario: Shell step with mode: autonomous

- **WHEN** a workflow declares a shell step with `mode: autonomous`
- **THEN** validation succeeds and the runner dispatches the step through the existing piped shell path

#### Scenario: Shell step without mode defaults to autonomous

- **WHEN** a workflow declares a shell step with no `mode` field
- **THEN** the step is treated as `mode: autonomous` and the runner dispatches it through the existing piped shell path

#### Scenario: Invalid mode value on shell step

- **WHEN** a workflow declares a shell step with `mode: foo`
- **THEN** validation SHALL fail with an error identifying the invalid mode value

### Requirement: Direct terminal inheritance with TUI suspend and resume

When a shell step runs with `mode: interactive`, the runner SHALL suspend the TUI before spawning the command, execute `sh -c <command>` with the user's real terminal inherited directly as stdin, stdout, and stderr, and resume the TUI after the command exits. The runner SHALL NOT proxy, capture, inspect, buffer, or rewrite terminal bytes. The shell command SHALL use the same foreground-process-group and job-control supervisor as interactive agent steps.

#### Scenario: TUI released during interactive shell step

- **WHEN** the runner starts an interactive shell step while the TUI is active
- **THEN** the TUI is suspended before the command is spawned and the command inherits the user's terminal directly

#### Scenario: Command reads from stdin

- **WHEN** an interactive shell step's command reads from stdin
- **THEN** the user's keystrokes are delivered directly to the command by the terminal

#### Scenario: TUI resumed after command exit

- **WHEN** the command in an interactive shell step exits (any exit code)
- **THEN** the TUI is resumed before the next workflow step begins

#### Scenario: Terminal identity is inherited
- **WHEN** an interactive shell command inspects its stdin, stdout, and stderr terminal devices
- **THEN** all three are the same terminal device held by Agent Runner before spawn

#### Scenario: Terminal features remain native
- **WHEN** the command uses resize notifications, mouse input, bracketed paste, cursor modes, or terminal-generated signals
- **THEN** the terminal delivers them natively without relay logic in Agent Runner

### Requirement: Shell-native exit semantics

An interactive shell step SHALL map the command's exit code to the step outcome using the same rules as a non-interactive shell step: exit code 0 maps to outcome `success`, any nonzero exit code maps to outcome `failed`. Shell steps do not use the agent completion control channel and SHALL NOT print an agent-session resume hint when the command exits.

#### Scenario: Command exits zero

- **WHEN** the command of an interactive shell step exits with code 0
- **THEN** the step outcome is `success` and the workflow advances to the next step

#### Scenario: Command exits nonzero

- **WHEN** the command of an interactive shell step exits with code 2
- **THEN** the step outcome is `failed` and workflow error handling applies as for any failed shell step

#### Scenario: Agent completion command has no special shell semantics

- **WHEN** text resembling an agent completion command is entered or printed during an interactive shell step
- **THEN** Agent Runner does not interpret terminal bytes or advance the workflow; only the shell command's exit determines completion

### Requirement: No interactive terminal transcript

The runner SHALL NOT capture or persist terminal output from an interactive shell step. Its `step_end` audit event SHALL contain the exit code and outcome, no `stdout` field, and an empty schema-required `stderr` value. Workflows that require output SHALL use an autonomous shell step with `capture`.

#### Scenario: Interactive output is live only
- **WHEN** an interactive shell command writes output
- **THEN** the user sees it on the terminal, but the output is not retained in run state, output files, or audit data

### Requirement: Interactive shell is incompatible with capture

A shell step SHALL NOT combine `mode: interactive` with a `capture` field. The runner SHALL reject such a step at validation time.

#### Scenario: Capture combined with interactive mode

- **WHEN** a workflow declares a shell step with both `mode: interactive` and `capture: out`
- **THEN** validation SHALL fail with an error identifying the conflict between `mode: interactive` and `capture`

### Requirement: Working directory honored

An interactive shell step SHALL honor its `workdir` field the same way a non-interactive shell step does: the command SHALL be spawned with the resolved workdir as its current working directory.

#### Scenario: Interactive shell step with workdir

- **WHEN** an interactive shell step has `workdir: ./subdir`
- **THEN** the command is spawned with `./subdir` (resolved against the run's base directory) as its current working directory
