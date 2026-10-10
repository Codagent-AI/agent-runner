# Capability: interactive-shell-steps (delta)

## MODIFIED Requirements

### Requirement: Direct terminal execution with TUI suspend and resume

When a shell step uses `mode: interactive`, Agent Runner SHALL release the TUI, start `sh -c <command>` with the user's terminal inherited directly as stdin, stdout, and stderr, supervise it with the shared interactive process-group and job-control machinery, then reclaim the terminal and restore the TUI after exit. Agent Runner SHALL NOT proxy, capture, inspect, buffer, or rewrite terminal bytes.

#### Scenario: Shell inherits the real terminal
- **WHEN** an interactive shell step starts
- **THEN** its stdin, stdout, and stderr identify the same terminal device Agent Runner held before spawn

#### Scenario: Shell output is live only
- **WHEN** the command writes terminal output
- **THEN** the output is visible to the user but absent from output files and audit values; `step_end` has no `stdout` and an empty `stderr`

#### Scenario: Exit status determines outcome
- **WHEN** the shell command exits
- **THEN** exit code 0 maps to `success` and any nonzero exit maps to `failed`, without using the agent completion channel
