## Why

The agent-runner CLI has no way to report its own version, making it difficult to confirm which build is running during debugging or CI verification. Adding a `--version` flag provides a standard, low-effort solution to this gap.

## What Changes

- Add `--version` / `-v` flag to the agent-runner CLI binary
- Printing the version string to stdout and exiting with code 0

## Capabilities

### New Capabilities

- `version-flag`: CLI flag that prints the current binary version and exits cleanly

### Modified Capabilities

<!-- none -->

## Impact

- `cmd/` entry point — flag registration and version string injection at build time
- No API, workflow, or data model changes
