## Why

Onboarding and day-to-day Agent Runner use depend on a constellation of preconditions — git, helper binaries (`agent-plugin`, `agent-validator`), at least one supported agent CLI, adequate terminal size — and today none of these can be checked in one place. When something is missing, users discover it mid-wizard or mid-run with a confusing failure. A focused `agent-runner doctor` command lets users (and support workflows) get a fast, boring, definitive read on whether the environment is ready.

## What Changes

- Add a new top-level `agent-runner doctor` subcommand that runs a fixed set of diagnostic checks and prints a plain-text report with `✓` / `✗` / `⚠` status lines plus a `Fix:` or `Tip:` hint for each non-pass. There are exactly three statuses; "skipped" / "indeterminate" cases are reported as `⚠`.
- Checks performed (status per check shown in parentheses):
  - Inside a git repository at the working directory (`✓` / `✗`).
  - `agent-plugin` resolvable on `PATH` (`✓` / `✗`).
  - `agent-validator` resolvable on `PATH` (`✓` / `✗`).
  - At least one supported agent CLI resolvable on `PATH` (`✓` / `✗`). This is a single gated check. The supported CLI set is sourced from the existing canonical allowlist in the codebase (currently `cmd/agent-runner/main.go`'s `allowedResumeCLIs`, kept in sync with `internal/config.validCLI`); doctor MUST NOT introduce a parallel list.
  - Per-CLI informational lines for each CLI in that allowlist: `✓` if present on `PATH`, `⚠` if missing. These lines never produce `✗` and never affect exit code; the gate above is what makes "no CLIs at all" a failure.
  - Terminal height at least the onboarding minimum (`✓` / `⚠`). When stdin or stdout is not a TTY (CI, piped), the check reports `⚠` ("not a TTY — terminal size not measured"); it never reports `✗`.
  - Install source detected — Homebrew, npm, or unknown (`✓` for a recognized source, `⚠` for unknown). Unknown is informational, never `✗`.
  - Codagent skills available for at least one detected agent CLI (`✓` / `✗` / `⚠`). When zero agent CLIs are detected, this check reports `⚠` ("skipped — no agent CLI detected") rather than `✗`, so the upstream CLI-gate failure is not double-counted.
- Exit code: non-zero if any check is `✗`; `⚠` never affects exit code.
- Command is invocable from the shell only. It does not appear as a TUI surface in this change.
- Doctor is purely diagnostic. It does not read or write `settings.setup.completed_at`, does not modify any settings, and does not gate native setup.

## Capabilities

### New Capabilities

- `doctor-command`: the `agent-runner doctor` subcommand, its fixed check set, output format, and exit-code semantics.

### Modified Capabilities

_None._ `native-setup` is intentionally untouched — doctor is a parallel, read-only surface and the existing setup wizard continues to own its own preflight.

## Technical Approach

`doctor` is a single, synchronous CLI command with no TUI, no engine, and no state. It composes existing detection primitives and prints to stdout.

```
agent-runner doctor
        │
        ▼
┌─────────────────────────────┐
│ cmd/agent-runner/doctor.go  │   command wiring, arg parsing
└──────────────┬──────────────┘
               │
               ▼
┌─────────────────────────────┐
│ internal/doctor             │   pure check functions + report model
│  ├── checks.go              │   one function per check, returns Result
│  └── report.go              │   formats Results to text, computes exit
└──────────────┬──────────────┘
               │ uses
               ▼
  exec.LookPath  os.Stat(.git)  term.GetSize  os.Executable / install-source
```

Key technical decisions:

- **New `internal/doctor` package, not bolted into `cmd/`.** Keeps check logic unit-testable with stub `LookPath`/`Stat`/`GetSize` dependencies, and keeps `cmd/agent-runner/doctor.go` a thin wiring layer consistent with how other subcommands in `cmd/agent-runner/` are organized.
- **Each check returns a typed `Result` (status, label, hint).** A `[]Result` is the data model; rendering and exit-code computation are pure functions over it. This makes both ergonomic to test and trivial to extend with new checks.
- **No network, no auth probing.** PATH presence only for agent CLIs. Avoids slow startups, flaky tests, and the credentials/network surface area. Per-CLI auth state is left to the CLIs themselves.
- **Single source of truth for the supported-CLI list.** Doctor consumes the existing allowlist (`cmd/agent-runner/main.go`'s `allowedResumeCLIs`, kept in sync with `internal/config.validCLI`) rather than hardcoding its own. The downstream spec must reference whichever of those is chosen as canonical so adding a new agent CLI is a one-place change.
- **Install-source detection is best-effort.** Resolve `os.Executable()`, then classify by path prefix (`/opt/homebrew`, `/usr/local/Cellar`, `*/node_modules/*`, npm global prefix). Unknown sources surface as `⚠` with a `Tip:` rather than `✗` — not having a recognized install source is informational, not a failure.
- **Terminal size check reads current rows via `golang.org/x/term` (already in module graph for TUI sizing).** Non-TTY invocations (CI, piped) report `⚠` with a "not a TTY — terminal size not measured" hint, keeping the status vocabulary to exactly `✓` / `✗` / `⚠` and keeping `doctor` useful in headless contexts.
- **Skills availability is derived, not probed.** For each detected agent CLI, check whether its plugin install location contains the Codagent skills directory; do not invoke `agent-plugin` or any network call. When zero agent CLIs are detected, the check reports `⚠` rather than `✗` so the upstream CLI-gate failure is not double-counted.
- **Output is plain text only.** No JSON in this change. If a machine-readable mode is later wanted, a `--json` flag can be added without disturbing the check model.

## Out of Scope

- Authenticating any agent CLI, or invoking any agent CLI to probe credentials.
- Calling `agent-plugin` or any network/registry to verify skill freshness.
- Auto-fixing any detected issue (no `doctor --fix`).
- TUI surface for doctor results (no home-tab entry, no listview tab).
- Reading or writing `settings.setup.completed_at` or any other settings file.
- A `--json` or other machine-readable output mode.
- Verifying agent-runner self-update channel or version skew.
- Per-workflow validation (already covered by `--validate`).

## Impact

- **New files:** `cmd/agent-runner/doctor.go` (+ test), `internal/doctor/` package (`checks.go`, `report.go`, plus tests).
- **Touched files:** `cmd/agent-runner/main.go` — register the `doctor` subcommand route and add a usage line; no behavioral changes to existing flags.
- **Specs:** new `openspec/specs/doctor-command/spec.md`.
- **Dependencies:** no new third-party dependencies; reuses `golang.org/x/term` and stdlib `os/exec`.
- **User-facing behavior:** purely additive. No existing command, flag, or workflow changes behavior.
- **CI / scripts:** `agent-runner doctor` becomes safe to use as a precondition gate in install scripts and onboarding docs because of its `✗` → non-zero exit contract.
