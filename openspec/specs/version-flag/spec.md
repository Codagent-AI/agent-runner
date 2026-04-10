## Requirements

### Requirement: Version flag output
The CLI SHALL support a `--version` / `-v` flag that prints the current binary version to stdout and exits with code 0.

#### Scenario: Version is set at build time
- **WHEN** the binary is invoked with `--version`
- **THEN** it prints the injected version string (e.g. `1.2.3`) to stdout and exits 0

#### Scenario: Short flag alias
- **WHEN** the binary is invoked with `-v`
- **THEN** it behaves identically to `--version`

#### Scenario: Version not set at build time
- **WHEN** the binary is invoked with `--version` and no version was injected at build time
- **THEN** it prints `dev` to stdout and exits 0
