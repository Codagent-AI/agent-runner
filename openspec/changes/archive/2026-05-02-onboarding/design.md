## Context

Agent Runner has no guided first-run experience. New users must hand-author config files and learn the workflow model from docs. This design covers Phases 1 (welcome) and 2 (agent-profile setup) of the onboarding workflow, adding two new step primitives (`mode: ui` and `script:`), typed captures, and a first-run dispatcher.

The codebase today has:
- `CapturedVariables map[string]string` threaded through ExecutionContext, NestedStepState, runner.Options
- `StepType()` inferring type from which fields are set (command→shell, prompt→agent, etc.)
- `ProcessRunner` interface with `RunShell` and `RunAgent`
- `ensureThemeForTUI` as the precedent for pre-TUI first-run prompts
- `workflows/embed.go` with `//go:embed *` that already includes all files but only exposes YAML via `List()`
- `internal/usersettings/` with hand-parsed Theme-only settings and atomic write
- Bubble Tea TUI with runview (main content + sidebar) and liverun coordinator

## Goals / Non-Goals

**Goals:**
- Implement typed captures as a backward-compatible extension
- Add `mode: ui` step type that renders inside the existing runview layout
- Add `script:` step type for bundled scripts with structured I/O
- Wire first-run dispatcher into TUI entry points
- Ship the onboarding workflow YAML and bundled scripts
- Add `agent-runner internal write-profile` subcommand

**Non-Goals:**
- Phases 3-7 of onboarding
- Runtime model discovery (curated static list per adapter)
- Multi-select or free-form text UI inputs
- Secrets/redaction in UI inputs
- Listview integration for "continue onboarding" entry point
- Telemetry on onboarding completion

## Approach

### Typed Captures

Define a discriminated union in `internal/model/`:

```go
type CaptureKind string

const (
    CaptureString CaptureKind = "string"
    CaptureList   CaptureKind = "list"
    CaptureMap    CaptureKind = "map"
)

type CapturedValue struct {
    Kind CaptureKind
    Str  string
    List []string
    Map  map[string]string
}
```

Change `CapturedVariables` from `map[string]string` to `map[string]CapturedValue` in:
- `ExecutionContext`
- `NestedStepState` (JSON state persistence)
- `runner.Options`
- `RunState` (via NestedStepState)

All existing shell captures produce `CapturedValue{Kind: CaptureString, Str: "..."}`. Headless agent captures likewise produce string values.

**State serialization**: JSON uses a tagged envelope:
```json
{"kind": "string", "value": "hello"}
{"kind": "list", "value": ["a", "b"]}
{"kind": "map", "value": {"k": "v"}}
```

A backward-compat `UnmarshalJSON` path handles legacy state files that store captures as plain strings (upgrades them to CapturedValue with Kind=string).

**Audit log serialization**: String captures appear as their existing string representation (no envelope). List captures appear as JSON arrays. Map captures appear as JSON objects. This preserves existing audit consumers.

**Loop overwrite**: Inside `loop:` steps, captured variables are overwritten per iteration as today. Type persists per iteration (no implicit accumulation).

### Interpolation Extension

Extend the regex from `\{\{(\w+)\}\}` to `\{\{(\w+(?:\.\w+)?)\}\}`.

**String-context interpolation** (`InterpolateTyped`):
- `{{name}}`: resolves from builtins → params → captures. For captures, requires `Kind == CaptureString`; non-string in a string context produces a descriptive error naming the variable.
- `{{name.field}}`: looks up `name` in captures, requires `Kind == CaptureMap`, extracts the field value. Field-access on a non-map capture produces an error.

**Typed-context resolution** (`ResolveTypedValue`):
- A separate helper for consumers that accept typed values natively (UI `options:` field, `script_inputs:` value positions).
- `ResolveTypedValue("{{detected_adapters}}", captures)` returns the full `CapturedValue` (list, map, or string) without requiring string coercion.
- Used by the UI executor when resolving `options:` and by the script executor when building `script_inputs` JSON.

The existing `Interpolate` (flat `map[string]string` signature) remains for backward-compat callers. `InterpolateShellSafe` gains a typed variant that shell-quotes string captures and rejects non-string captures in shell contexts.

### Step Model Extensions

New fields on `Step`:

```go
Script         string            `yaml:"script,omitempty"`
ScriptInputs   map[string]string `yaml:"script_inputs,omitempty"`
CaptureFormat  string            `yaml:"capture_format,omitempty"`
Title          string            `yaml:"title,omitempty"`
Body           string            `yaml:"body,omitempty"`
Actions        []UIAction        `yaml:"actions,omitempty"`
Inputs         []UIInput         `yaml:"inputs,omitempty"`
OutcomeCapture string            `yaml:"outcome_capture,omitempty"`
```

Supporting types:

```go
type UIAction struct {
    Label   string `yaml:"label"`
    Outcome string `yaml:"outcome"`
}

type UIInput struct {
    Kind    string   `yaml:"kind"`
    ID      string   `yaml:"id"`
    Prompt  string   `yaml:"prompt"`
    Options []string `yaml:"options,omitempty"`
    Default string   `yaml:"default,omitempty"`
}
```

New StepMode constant: `ModeUI StepMode = "ui"`.

**StepType detection**: `Mode == ModeUI` is counted as a step type boolean in `hasExactlyOneStepType`. `Script != ""` is another. If both are set, validation rejects with "must have exactly one" — no special negative-condition logic.

```go
isUI     := s.Mode == ModeUI
isScript := s.Script != ""
```

**Validation rules**:
- `model` and `cli` rejected on UI steps and script steps
- `capture` allowed on: shell, script, UI-with-inputs, and headless agent steps (preserving existing behavior)
- `script_inputs` requires `script`
- `title` required on `mode: ui`; `actions` required on `mode: ui` (at least one)
- `capture_format` requires `script` with `capture` (rejected on shell/UI steps)
- `outcome_capture` requires `mode: ui` with `actions`

### UI Step Executor

**Communication channel**: `ExecutionContext` gains:

```go
UIStepHandler func(UIStepRequest) (UIStepResult, error)
```

The handler is set by the liverun coordinator in TUI mode. When nil and the step is reached, the executor fails with "non-interactive terminal: UI steps require a TTY."

**Request/Response**:

```go
type UIStepRequest struct {
    StepID  string
    Title   string       // interpolated, ANSI-stripped
    Body    string       // interpolated, ANSI-stripped markdown
    Actions []UIAction
    Inputs  []UIInputResolved // options already resolved to concrete []string
}

type UIStepResult struct {
    Outcome  string            // selected action outcome identifier
    Inputs   map[string]string // input id → selected value
    Canceled bool              // true if user pressed Ctrl-C or equivalent
}
```

**TUI integration**: The liverun model receives the request (via a `tea.Msg` sent from the handler goroutine). The runview switches its main content area to a `uistep.Model` (new Bubble Tea model in `internal/uistep/`). The sidebar with workflow steps remains visible. The model renders:
- Markdown body (using existing tuistyle primitives)
- Action buttons (highlighted selection, keyboard navigation)
- Single-select input fields (arrow-key selection within options)

On user action, the result is sent back through the response channel, unblocking the executor.

**Cancellation semantics**:
- Ctrl-C during a UI step: handler returns `UIStepResult{Canceled: true}`; executor maps to `OutcomeAborted`; workflow handles per `continue_on_failure` rules.
- Program shutdown / context cancellation: handler returns an error; executor propagates as step failure.
- "No UI handler configured" (nil handler): immediate error — "UI steps require a TTY."
- Stdin/stdout not a TTY but handler is nil: same immediate error (the handler is only set when TUI mode is active).

**ANSI sanitization**: All runtime-interpolated values in body content and option labels are stripped of ANSI escape sequences before rendering. Static YAML content is not stripped (authors control their own content).

**Capture behavior**:
- `outcome_capture: <name>` → stores selected action outcome as `CapturedValue{Kind: CaptureString, Str: outcome}`
- `capture: <name>` with declared inputs → stores `CapturedValue{Kind: CaptureMap, Map: {input_id: selected_value, ...}}`

### Script Step Executor

**ProcessRunner extension**:

```go
RunScript(path string, stdin []byte, captureStdout bool, workdir string) (ProcessResult, error)
```

Implementation:
- `exec.Command(path)` directly (not `sh -c`)
- `c.Stdin = bytes.NewReader(stdin)` (empty reader if nil, never os.Stdin)
- Set `c.Dir` from effective step workdir
- Stdout/stderr tee pattern matches `RunAgent` (TUI streaming + capture)
- Non-zero exits: `ProcessResult{ExitCode: n}` with nil error (existing pattern)
- Spawn/setup failures: return error

**Path resolution**:
- Script path is a static literal in YAML (no interpolation allowed in the `script:` field itself)
- For builtin workflows: resolved relative to `sessionDir/bundled/<namespace>/`
- For user-authored workflows: resolved relative to the workflow YAML file's directory
- Load-time validation: reject absolute paths, `..` components
- Runtime validation: reject symlinks that escape the namespace directory

**Environment**: The spawned process receives `AGENT_RUNNER_BUNDLE_DIR` pointing at `sessionDir/bundled/<namespace>/` so scripts can locate sibling data files without `$0` path tricks.

**Script inputs**: `script_inputs` map values are interpolated (supporting typed resolution for non-string captures), then JSON-encoded as an object to stdin. If no `script_inputs` declared, stdin is empty.

**Capture**:
- Default (`capture_format: text` or unset): stdout captured as `CapturedValue{Kind: CaptureString}`
- `capture_format: json`: parse stdout (capped at 1 MiB, UTF-8 validated) as JSON at runtime:
  - JSON array of strings → `CapturedValue{Kind: CaptureList}`
  - JSON object of string values → `CapturedValue{Kind: CaptureMap}`
  - Invalid JSON, non-string elements, over 1 MiB, non-UTF-8 → runtime step failure with descriptive error (not a load-time validation error)

### Settings Refactor

```go
type OnboardingSettings struct {
    CompletedAt string `yaml:"completed_at,omitempty"`
    Dismissed   string `yaml:"dismissed,omitempty"`
}

type Settings struct {
    Theme      Theme              `yaml:"theme,omitempty"`
    Onboarding OnboardingSettings `yaml:"onboarding,omitempty"`
}
```

Custom `MarshalYAML` / `UnmarshalYAML` using `yaml.Node` internally to preserve unknown keys for forward-compatibility. `Load()` and `Save()` refactored from hand-parsing to struct-based marshal/unmarshal.

Atomic write pattern preserved (temp file + rename, mode 0o600, parent dir 0o755).

### First-Run Dispatcher

New `ensureOnboardingForTUI` function in `cmd/agent-runner/main.go`. Called after `ensureThemeForTUI` in `handleListBare` and `handleList` only — not in `handleInspect` or `handleResume` (prompting onboarding while inspecting or resuming a specific run is surprising).

Condition: `settings.Onboarding.CompletedAt == "" && settings.Onboarding.Dismissed == ""` and both stdin/stdout are TTYs.

When firing: launches `onboarding:welcome` via the standard `handleRun` path using `builtinworkflows.Resolve("onboarding:welcome")`. Same loader, runner, audit machinery as any explicit invocation.

### Bundled Asset Materialization

On builtin workflow run start, materialize the namespace's non-YAML embedded files into `sessionDir/bundled/<namespace>/`. Directory layout mirrors the embedded FS structure.

File modes:
- Scripts (.sh): 0o700
- Data files: 0o600
- Directories: 0o700

On resume: if the bundled directory is missing, re-materialize from the embedded FS before executing further steps.

Implementation: a new `materializeBundledAssets(sessionDir, namespace string) error` function in the runner package that walks the embedded FS subdirectory and writes non-YAML files.

### Internal write-profile Subcommand

`args[0] == "internal"` intercepted in `run()` before flag parsing, dispatching to `handleInternal(args[1:])`. The `write-profile` subcommand:

- Reads JSON from stdin: `{interactive_cli, interactive_model, headless_cli, headless_model, target_path}` (model fields may be empty string when the CLI does not support model selection)
- Loads existing file (if any) as `yaml.Node`
- Merges four agents into `profiles.default.agents`:
  - `interactive_base`: `{default_mode: interactive, cli: <chosen>, model: <chosen>}`
  - `headless_base`: `{default_mode: headless, cli: <chosen>, model: <chosen>}`
  - `planner`: `{extends: interactive_base}`
  - `implementor`: `{extends: headless_base}`
- Preserves all other agents, profile sets, and top-level keys
- Writes atomically (temp + rename, mode 0o600, parent dirs 0o755)

Treated as a supported hidden API: full test coverage (unit + integration), stable JSON contract, versioned alongside the bundled workflows that depend on it.

### Embed Extension

The existing `//go:embed *` already captures all files. Changes needed:
- New `ListAssets(namespace string) ([]string, error)` function returning non-YAML file paths within a namespace subdirectory
- New `ReadAsset(path string) ([]byte, error)` for reading non-YAML files (thin wrapper over `FS.ReadFile`)
- `List()` continues to return only YAML workflow refs (no behavior change for existing consumers)

### Onboarding Workflow Structure

Embedded under `workflows/onboarding/`:

**welcome.yaml** (Phase 1 + orchestration):
1. UI step: welcome screen with body + 3 actions (continue, not_now, dismiss)
2. Shell step: write `dismissed` timestamp (skip_if: outcome != dismiss)
3. Sub-workflow: `setup-agent-profile.yaml` (skip_if: outcome != continue)
4. Shell step: write `completed_at` timestamp (runs on successful setup return)

**setup-agent-profile.yaml** (Phase 2):
1. Script step: `detect-adapters.sh` → captures adapter list
2. UI step: pick interactive CLI (options from captured list)
3. Script step: `models-for-cli.sh` → captures model list for chosen CLI (may be empty)
4. UI step: pick interactive model (skip_if: model list is empty)
5. Script step: `detect-adapters.sh` → captures adapter list (for headless)
6. UI step: pick headless CLI
7. Script step: `models-for-cli.sh` → captures model list for chosen CLI
8. UI step: pick headless model (skip_if: model list is empty)
9. UI step: pick scope (global / project)
10. UI step: confirmation screen (show choices, confirm/cancel actions)
11. Script step: `write-profile.sh` (invokes `agent-runner internal write-profile`)

**Bundled scripts**:
- `detect-adapters.sh`: checks `$PATH` for known CLI binaries, outputs JSON array of found adapter names
- `models-for-cli.sh`: queries the chosen CLI for available models at runtime (e.g., adapter-specific list-models command); outputs JSON array of model names, or `[]` if the CLI does not support model listing
- `write-profile.sh`: thin wrapper that pipes JSON to `agent-runner internal write-profile`

## Decisions

| Decision | Rationale | Alternatives considered |
|----------|-----------|------------------------|
| CapturedValue union type | Compile-time safety; each consumer explicitly handles kinds via switch. Cleaner than map[string]any type assertions or JSON-encoded strings with sidecar metadata. | map[string]any (loses safety), JSON strings + type sidecar (awkward to consume) |
| UI step embedded in runview content area | Keeps sidebar visible for context; avoids suspend/resume flicker; leverages existing runview layout. | Standalone tea.Program (loses sidebar), helper binary (adds IPC complexity) |
| Channel-based runner↔TUI handoff | Natural fit for Go concurrency; runner goroutine blocks, TUI event loop remains responsive; easy to test with fake handlers. | Mutex+condvar (lower-level, harder to test), poll-based (slower, more complex) |
| Extend ProcessRunner with RunScript | Script steps are first-class process steps needing the same TUI streaming, output files, workdir, and capture behavior. One interface keeps all process concerns together. | Separate ScriptRunner (increases architecture surface, risks divergent behavior), direct os/exec (untestable without real processes) |
| Settings struct marshal with yaml.Node extras | Clean struct-based code; custom marshal/unmarshal preserves unknown keys for forward-compat. More up-front work than hand-parsing but pays off as settings grow. | yaml.Node merge only (fragile), separate onboarding file (splits user state) |
| Materialize bundled assets into session dir | Survives crash/resume without re-extraction from temp. Visible in inspect mode for debugging. Scripts can find siblings via AGENT_RUNNER_BUNDLE_DIR. | Temp dir extraction (cleanup edge cases, invisible on resume), embed.FS passthrough (each invocation does I/O) |
| Dispatcher limited to bare/list entry points | Onboarding prompt during --inspect or --resume of a specific run is surprising. Theme prompt already runs at these points but is instant; onboarding launches a full workflow. | All TUI entry points (too aggressive), only bare invocation (misses --list) |
| Theme first, then onboarding | Onboarding renders a TUI and needs a theme. Sequencing theme before onboarding guarantees consistent appearance. | Onboarding subsumes theme (couples unrelated concerns), single gate function (tight coupling) |
| ResolveTypedValue for typed consumers | UI `options:` needs `{{adapters}}` as a list. String interpolation correctly rejects non-strings, but typed consumers need a separate resolution path that returns CapturedValue directly. | Pre-flatten maps/lists (loses error detection), always allow any type (unsafe) |
| Internal write-profile as supported hidden API | Bundled workflows depend on it; it must be tested and stable. "Internal" signals it's not user-facing CLI, but it has a versioned JSON contract. | Shell-side YAML emission (unsafe, spec explicitly forbids), config package API called in-process (can't invoke from a script step) |
| Runtime model discovery over static list | Always current; no bundled JSON to maintain; gracefully handles CLIs without model listing by skipping the step. | Static curated models.json (goes stale), always show "default" option (noisy) |

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Typed captures touch state, audit, interpolation, loops, resume, pre-validation | Implement as first change with comprehensive tests. Backward-compat unmarshal ensures existing state files still load. String-only producers emit the same shape. |
| UI step TUI rendering varies across terminals | Manual verification on macOS Terminal, iTerm2, and common Linux terminals. Use existing tuistyle primitives that are already tested. |
| Bundled asset materialization increases session dir size | Only non-YAML files (scripts + one JSON data file). Total is < 10 KB for the onboarding namespace. Negligible. |
| ProcessRunner interface change breaks test fakes | All fakes in the codebase are in-repo; update them in the same PR. RunScript follows the same pattern as RunShell/RunAgent. |
| Settings refactor could lose unknown keys | Custom marshal/unmarshal with yaml.Node preserves unknown keys. Test with round-trip of settings containing extra keys. |
| Channel-based handoff deadlock if TUI exits unexpectedly | Handler returns error on context cancellation / program shutdown. Executor treats handler error as step failure. |
| Resume after crash with missing bundled dir | Re-materialize from embedded FS on resume if directory is absent. Embedded FS is always available in the binary. |

## Migration Plan

No user-facing migration needed. This is purely additive:
- New step types don't affect existing workflows (no YAML field conflicts)
- Typed captures are backward-compatible (existing string captures unchanged)
- Settings gains new keys without removing existing ones
- State files gain typed capture envelope; old state files are handled by compat unmarshal
- ProcessRunner gains RunScript; existing methods unchanged

**Rollout order** (implementation sequence):
1. Typed captures (model, state, interpolation, audit) — foundation everything else depends on
2. Settings refactor (struct marshal, onboarding keys)
3. Step model extensions (new fields, validation, StepType)
4. Script step executor + ProcessRunner.RunScript + bundled asset materialization
5. UI step executor + TUI integration (uistep model, handler wiring)
6. Internal write-profile subcommand
7. Onboarding workflow YAML + bundled scripts
8. First-run dispatcher wiring

**Rollback**: Each step is independently useful and testable. If the onboarding workflow has issues, the dispatcher can be disabled (single function removal) without affecting the primitives.

## UI Mockups

### Phase 1: Welcome Screen

```
┌─ Steps ──────────┬────────────────────────────────────────��────────┐
│                  │                                                 │
│  ● welcome       │  Welcome to Agent Runner                        │
│  ○ set-dismissed │                                                 │
│  ○ setup         │  Agent Runner orchestrates multi-step AI        │
│  ○ set-completed │  workflows by managing sessions, prompts, and   │
│                  │  tool access across different CLI adapters.      │
│                  │                                                 │
│                  │  This setup will help you:                      │
│                  │                                                 │
│                  │    • Choose your preferred CLI adapters          │
│                  │    • Select models for interactive and headless  │
│                  │      modes                                      │
│                  │    • Write an initial agent profile config       │
│                  │                                                 │
│                  │  It takes about 60 seconds.                     │
│                  │                                                 │
│                  │                                                 │
│                  │  ┌──────────────┐  ┌──────────┐  ┌─────────┐   │
│                  │  │  Continue ▶  │  │ Not now  │  │ Dismiss │   │
│                  │  └──────────────┘  └──────────┘  └─────────┘   │
│                  │                                                 │
│                  │  ←→ Navigate  Enter Select                     │
└──────────────────┴─────────────────────────────────────────────────┘
```

### Phase 2: Pick Interactive CLI

```
┌─ Steps ──────────────────┬─────────────────────────────────────���───┐
│                          │                                         │
│  ✓ detect-adapters       │  Interactive Agent — CLI Adapter        │
│  ● pick-interactive-cli  │                                         │
│  ○ models-interactive    │  Choose the CLI adapter for your        │
│  ○ pick-interactive-model│  interactive agent. This is used for    │
│  ○ detect-adapters-hl    │  planning and conversation tasks.       │
│  ○ pick-headless-cli     │                                         │
│  ○ models-headless       │                                         │
│  ○ pick-headless-model   │    ┌─────────────────────────────┐      │
│  ○ pick-scope            │    │  ▶ claude                   │      │
│  ○ confirm               │    │    codex                    │      │
│  ○ write-profile         │    │    copilot                  │      │
│                          │    └─────────────────────────────┘      │
│                          │                                         │
│                          │                                         │
│                          │  ↑↓ Navigate  Enter Select              │
└──────────────────────────┴─────────────────────────────────────────┘
```

### Phase 2: Pick Interactive Model

```
┌─ Steps ──────────────────┬─────────────────────────────────────────┐
│                          │                                         │
│  ✓ detect-adapters       │  Interactive Agent — Model              │
│  ✓ pick-interactive-cli  │                                         │
│  ✓ models-interactive    │  Choose the model for your interactive  │
│  ● pick-interactive-model│  agent (cli: claude).                   │
│  ○ detect-adapters-hl    │                                         │
│  ○ pick-headless-cli     │                                         │
│  ○ models-headless       │    ┌─────────────────────────────┐      │
│  ○ pick-headless-model   │    │  ▶ opus                     │      │
│  ○ pick-scope            │    │    sonnet                   │      │
│  ○ confirm               │    │    haiku                    │      │
│  ○ write-profile         │    └─────────────────────────────┘      │
│                          │                                         │
│                          │                                         │
│                          │  ↑↓ Navigate  Enter Select              │
└──────────────────────────┴─────────────────────────────────────────┘
```

### Phase 2: Pick Scope

```
┌─ Steps ──────────────────┬─────────────────────────────────────────┐
│                          │                                         │
│  ✓ detect-adapters       │  Config Scope                           │
│  ✓ pick-interactive-cli  │                                         │
│  ✓ models-interactive    │  Where should the profile be saved?     │
│  ✓ pick-interactive-model│                                         │
│  ✓ detect-adapters-hl    │                                         │
│  ✓ pick-headless-cli     │    ┌─────────────────────────────┐      │
│  ✓ models-headless       │    │  ▶ global                   │      │
│  ✓ pick-headless-model   │    │      ~/.agent-runner/       │      │
│  ● pick-scope            │    │    project                  │      │
│  ○ confirm               │    │      .agent-runner/         │      │
│  ○ write-profile         │    └─────────────────────────────┘      │
│                          │                                         │
│                          │                                         │
│                          │  ↑↓ Navigate  Enter Select              │
└──────────────────────────┴─────────────────────────────────────────┘
```

### Phase 2: Confirmation

```
┌─ Steps ──────────────────┬─────────────────────────────────────────┐
│                          │                                         │
│  ✓ detect-adapters       │  Confirm Agent Profile                  │
│  ✓ pick-interactive-cli  │                                         │
│  ✓ models-interactive    │  The following will be written to       │
│  ✓ pick-interactive-model│  ~/.agent-runner/config.yaml:           │
│  ✓ detect-adapters-hl    │                                         │
│  ✓ pick-headless-cli     │    interactive_base: claude / opus      │
│  ✓ models-headless       │    headless_base:    codex / gpt-5      │
│  ✓ pick-headless-model   │    planner:          extends interactive │
│  ✓ pick-scope            │    implementor:      extends headless   │
│  ● confirm               │                                         │
│  ○ write-profile         │                                         │
│                          │  ┌─────────────┐  ┌──────────┐          │
│                          │  │  Confirm ▶  │  │  Cancel  │          │
│                          │  └─────────────┘  └──────────┘          │
│                          │                                         │
│                          │  ←→ Navigate  Enter Select              │
└──────────────────────────┴─────────────────────────────────────────┘
```

## Open Questions

- Per-adapter model discovery commands: need to identify the correct CLI invocation for each adapter that lists available models (e.g., `claude models list`, or equivalent). Adapters without a list-models command will have model selection skipped.

## Resolved Questions

- **Overwrite-confirmation screen**: Separate UI step with `skip_if` when collision count is zero. A preceding script step (`check-collisions.sh`) detects existing entries.
- **`AGENT_RUNNER_BUNDLE_DIR` scope**: Only set for builtin namespace scripts. User-authored scripts resolve relative to their workflow file and don't need it.
- **`capture_format` applicability**: Only valid on script steps (with `capture:` set). Rejected at load time on shell or UI steps.
