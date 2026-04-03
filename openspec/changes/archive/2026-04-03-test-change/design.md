## Context

The agent-runner CLI has no way to report its own version. The CLI uses `cobra` for command handling (`cmd/agent-runner/main.go`) with subcommands `run`, `validate`, and `resume`. No version variable or flag exists today.

## Goals / Non-Goals

**Goals:**
- Provide a standard `--version` / `-v` flag that prints the binary version and exits cleanly

**Non-Goals:**
- Extended version metadata (commit hash, build date, Go version)
- Version subcommand (flag only)

## Decisions

- **Version variable with ldflags injection**: Add `var version = "dev"` at package level in `cmd/agent-runner/main.go`. Override at build time via `go build -ldflags "-X main.version=1.2.3"`. This is the standard Go pattern for CLI version injection.
- **Cobra built-in version support**: Set `rootCmd.Version = version` to get `--version` and `-v` flags automatically. Use `rootCmd.SetVersionTemplate` to control output format (e.g. `{{.Version}}\n` for plain version string). This avoids custom flag registration and exit handling.

## Risks / Trade-offs

- No significant risks. The `ldflags` pattern is widely used and well-understood in Go CLIs.
- If the build does not inject a version, the default `"dev"` is printed, which satisfies the spec.

## Migration Plan

- Purely additive — no breaking changes, no rollback needed.
- CI/CD pipelines should be updated to pass `-ldflags "-X main.version=..."` at build time, but this is not required for correctness (defaults to `"dev"`).

## Open Questions

<!-- none -->
