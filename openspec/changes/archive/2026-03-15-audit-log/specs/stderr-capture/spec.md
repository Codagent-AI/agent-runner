## ADDED Requirements

### Requirement: Shell stderr piping

Baton SHALL pipe stderr from all shell steps instead of inheriting it. The stderr output SHALL be teed to the terminal in real-time and stored for inclusion in the audit log.

#### Scenario: Stderr displayed and captured
- **WHEN** a shell step produces stderr output
- **THEN** the stderr is displayed to the terminal in real-time and stored for the audit log `step_end` entry

#### Scenario: Stderr captured regardless of capture field
- **WHEN** a shell step has no `capture` field
- **THEN** stderr is still piped, teed, and stored for the audit log

#### Scenario: Stderr captured alongside stdout capture
- **WHEN** a shell step has `capture: output` and produces both stdout and stderr
- **THEN** stdout is captured into the variable and teed, stderr is separately captured and teed, both are included in the audit log

#### Scenario: No stderr output
- **WHEN** a shell step produces no stderr
- **THEN** the audit log `step_end` entry includes an empty stderr field
