# headless-progress-indication Specification

## Purpose
TBD - created by archiving change improve-output. Update Purpose after archive.
## Requirements
### Requirement: Headless prompt display

When a headless agent step starts, baton SHALL print the full resolved prompt text to stdout, indented with two spaces per line. The prompt is printed after the step heading and mode label, before the agent subprocess starts.

#### Scenario: Prompt displayed for headless step
- **WHEN** a headless agent step starts with a resolved prompt
- **THEN** the prompt text is printed to stdout, indented with two spaces per line

#### Scenario: Prompt not displayed for interactive step
- **WHEN** an interactive agent step starts
- **THEN** the prompt text is NOT printed (the user sees it in the interactive session)

#### Scenario: Long prompt displayed in full
- **WHEN** a headless agent step has a multi-line resolved prompt
- **THEN** the full prompt is printed without truncation

### Requirement: Headless spinner animation

While a headless agent step is running, baton SHALL display an ASCII spinner animation with the label `agent running...` to indicate the step is in progress. The spinner is cleared when the step completes or is interrupted.

#### Scenario: Spinner shown during headless execution
- **WHEN** a headless agent step is running
- **THEN** an ASCII spinner with label `agent running...` is displayed on the current terminal line

#### Scenario: Spinner cleared on step completion
- **WHEN** a headless agent step finishes (success or failure)
- **THEN** the spinner is stopped and the line is cleared before subsequent output

#### Scenario: Spinner cleared on ctrl-c
- **WHEN** the user presses ctrl-c during a headless agent step with an active spinner
- **THEN** the spinner is stopped and cleared before baton exits

#### Scenario: Spinner not shown for interactive steps
- **WHEN** an interactive agent step is running
- **THEN** no spinner is displayed

