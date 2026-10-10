# Capability: live-run-view (delta)

## MODIFIED Requirements

### Requirement: Interactive agent steps suspend the TUI

When the workflow dispatches an interactive agent step, the run-view TUI SHALL suspend, releasing the terminal so the agent process has full control. When the agent process exits, the TUI SHALL re-enter automatically without user input, regardless of the agent's exit status.

#### Scenario: Interactive step takes over terminal
- **WHEN** an interactive agent step starts
- **THEN** the run-view TUI suspends and the agent process owns the terminal

#### Scenario: Agent exits successfully and returns to TUI
- **WHEN** the interactive agent process exits with a successful outcome (completion event accepted and turn durability confirmed, per `step-control-channel`)
- **THEN** the run-view TUI re-enters automatically, the step's row reflects `success`, and workflow execution continues

#### Scenario: Agent exits abnormally and returns to TUI
- **WHEN** the interactive agent process exits without an accepted completion event (the session was abandoned or the CLI returned non-zero)
- **THEN** the run-view TUI re-enters automatically and the step's row reflects the recorded outcome (`aborted` or `failed`, per the existing interactive-agent behavior defined in the agent-runner engine)
