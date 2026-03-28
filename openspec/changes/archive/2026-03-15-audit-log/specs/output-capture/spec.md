## ADDED Requirements

### Requirement: Shell stderr capture

Shell steps SHALL pipe stderr in addition to stdout. Stderr SHALL be teed to the terminal in real-time and stored for the audit log. See `stderr-capture` spec for full requirements.

#### Scenario: Stderr piped alongside stdout
- **WHEN** a shell step with `capture: output` executes and produces both stdout and stderr
- **THEN** stdout is captured into the variable and teed, stderr is separately teed and stored for the audit log
