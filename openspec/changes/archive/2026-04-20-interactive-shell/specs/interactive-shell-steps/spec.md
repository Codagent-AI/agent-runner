## ADDED Requirements

### Requirement: Interactive mode attribute on shell steps

A shell step MAY set `mode: interactive` to request PTY-based execution with the user's terminal attached. When `mode` is absent, the shell step SHALL default to `headless`. When `mode` is `headless` (explicit or defaulted), the shell step SHALL execute via the existing non-PTY path. The `mode` value MUST be `interactive` or `headless`; any other value SHALL fail validation.

Unlike agent steps — where `mode` defaults come from the resolved profile's `default_mode` — shell steps have no profile, so the default is always `headless`.

#### Scenario: Shell step with mode: interactive

- **WHEN** a workflow declares a shell step with `command: "read -p 'Name? ' name && echo Hi $name"` and `mode: interactive`
- **THEN** validation succeeds and the runner dispatches the step through the interactive shell path

#### Scenario: Shell step with mode: headless

- **WHEN** a workflow declares a shell step with `mode: headless`
- **THEN** validation succeeds and the runner dispatches the step through the existing non-PTY shell path

#### Scenario: Shell step without mode defaults to headless

- **WHEN** a workflow declares a shell step with no `mode` field
- **THEN** the step is treated as `mode: headless` and the runner dispatches it through the existing non-PTY shell path

#### Scenario: Invalid mode value on shell step

- **WHEN** a workflow declares a shell step with `mode: foo`
- **THEN** validation SHALL fail with an error identifying the invalid mode value

### Requirement: PTY execution with TUI suspend and resume

When a shell step runs with `mode: interactive`, the runner SHALL suspend the TUI before spawning the command, execute the command inside a pseudo-terminal with the user's terminal attached for stdin, stdout, and stderr, and resume the TUI after the command exits.

#### Scenario: TUI released during interactive shell step

- **WHEN** the runner starts an interactive shell step while the TUI is active
- **THEN** the TUI is suspended before the command is spawned and the user's terminal is attached to the PTY

#### Scenario: Command reads from stdin

- **WHEN** an interactive shell step's command reads from stdin
- **THEN** the user's keystrokes are delivered to the command through the PTY

#### Scenario: TUI resumed after command exit

- **WHEN** the command in an interactive shell step exits (any exit code)
- **THEN** the TUI is resumed before the next workflow step begins

### Requirement: Shell-native exit semantics

An interactive shell step SHALL map the command's exit code to the step outcome using the same rules as a non-interactive shell step: exit code 0 maps to outcome `success`, any nonzero exit code maps to outcome `failed`. The runner SHALL NOT detect continue triggers (`/next`, keyboard shortcut, or sentinel escape sequences) during an interactive shell step, and SHALL NOT print a resume-the-session hint when the command exits.

#### Scenario: Command exits zero

- **WHEN** the command of an interactive shell step exits with code 0
- **THEN** the step outcome is `success` and the workflow advances to the next step

#### Scenario: Command exits nonzero

- **WHEN** the command of an interactive shell step exits with code 2
- **THEN** the step outcome is `failed` and workflow error handling applies as for any failed shell step

#### Scenario: User types a continue-trigger sequence

- **WHEN** the user types `/next` or presses the continue-trigger keyboard shortcut during an interactive shell step
- **THEN** the bytes are delivered to the command as normal input and the runner does not terminate the command or advance the workflow

#### Scenario: Command emits the agent sentinel

- **WHEN** the command writes the bytes used as the agent continue-trigger sentinel to its terminal
- **THEN** the runner forwards the bytes to the user's terminal and does not terminate the command or advance the workflow

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
