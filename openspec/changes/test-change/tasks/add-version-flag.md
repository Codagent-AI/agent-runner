# Task: Add --version flag

## Goal

Add a `--version` / `-v` flag to the agent-runner CLI that prints the binary version and exits cleanly.

## Background

You MUST read these files before starting:
- `openspec/changes/test-change/design.md` for full design details
- `openspec/changes/test-change/specs/version-flag/spec.md` for acceptance criteria

The CLI entry point is `cmd/agent-runner/main.go`. It uses `cobra` for command handling. The root command is defined in the `run()` function as `rootCmd`. There is currently no version variable or flag.

## Done When

All three spec scenarios pass: `--version` prints the injected version, `-v` behaves identically, and an uninjected build prints `dev`. A test confirms the version variable defaults to `"dev"`.
