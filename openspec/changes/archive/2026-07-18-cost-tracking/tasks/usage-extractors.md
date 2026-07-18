# Task: Usage extraction seam and five CLI extractors

## Goal

Add the optional `UsageExtractor` adapter capability, hook it into the agent step executor, and implement extraction for all five CLI adapters (Claude, Codex, OpenCode, Copilot, Cursor). This turns the existing default unavailable usage records into real token usage and CLI-reported cost flowing into audit events and `run-metrics.json`.

## Background

You MUST read these files before starting:

- `openspec/changes/cost-tracking/design.md` — sections 3 (adapter seam), 4 (per-CLI table), 5 (cumulative attribution context) and the Risks list govern this task.
- `openspec/changes/cost-tracking/specs/cli-adapter/spec.md`
- `openspec/changes/cost-tracking/specs/agent-usage-collection/spec.md`
- `openspec/changes/cost-tracking/specs/cost-capture/spec.md`
- `internal/cli/adapter.go` — the optional-capability pattern to follow (`OutputFilter`, `HeadlessResultFilter`, `StdoutWrapper`, `StderrWrapper`), the `lineBufferedWriter` JSONL helper, and the adapter registry.
- `internal/cli/claude.go`, `internal/cli/codex.go`, `internal/cli/opencode.go`, `internal/cli/copilot.go`, `internal/cli/cursor.go`.
- `internal/cli/adapter_test.go` — the existing fixture-based test pattern; Codex `turn.completed` fixtures show usage objects that the current parser in `codex.go` discards.
- `internal/exec/agent.go` — `runAgentProcess` (headless stdout is captured, `HeadlessResultFilter` applied), `OutputFilter`/capture handling, and `emitAgentEnd`.

### Current state you build on

The metrics types (`model.UsageRecord`, `model.TokenCounts`, canonical category constants, `UnavailableReason` values) exist in `internal/model`. The agent executor already attaches usage records to `step_end` events: `unavailable`/`unsupported-adapter` for headless steps, `unavailable`/`pty-context` for PTY-backed contexts. The metrics collector in `internal/metrics` attributes cumulative usage via per-session baselines; **extractors are stateless and must never compute deltas themselves** — for cumulative CLIs they fill `RawCumulative` and leave `Tokens` empty.

### The seam (from the design)

```go
type UsageExtraction struct {
    Usage            model.UsageRecord
    EstimatedCostUSD *float64
}

// UsageExtractor is implemented by adapters whose CLI emits structured usage.
type UsageExtractor interface {
    // ExtractUsage parses the raw headless stdout (pre-OutputFilter).
    ExtractUsage(rawStdout string) (UsageExtraction, error)
}
```

Error taxonomy: `error != nil` means malformed output → the executor records `unavailable`/`parse-failure`. A nil error whose `Usage.Status` is unavailable with reason `no-usage-event` means well-formed output that contained no usage event. Extraction never fails the step.

Executor hookup: in `ExecuteAgentStep`, after `runAgentProcess` returns and before `emitAgentEnd`, autonomous-headless contexts only. Type-assert the adapter to `UsageExtractor`; adapters without it keep the `unsupported-adapter` unavailable record. Attach the resulting `UsageRecord` and cost to the `step_end` event as typed values (the existing attachment point).

### Per-CLI extraction (authoritative table from the design)

| Adapter | Headless flags added | Usage source | Vendor → canonical mapping | Event semantics | Cost |
|---|---|---|---|---|---|
| claude | `--output-format stream-json --verbose` | final `result` event `usage` | `input_tokens→input`, `cache_read_input_tokens→cached_input`, `cache_creation_input_tokens→cache_write`, `output_tokens→output` | single final snapshot; last `result` wins | `total_cost_usd` (USD) |
| codex | none (already `--json`) | `turn.completed` `usage` | `input_tokens→input`, `cached_input_tokens→cached_input`, `output_tokens→output`, `reasoning_output_tokens→reasoning` | **cumulative** snapshot per event; last wins; report in `RawCumulative` only | none |
| opencode | none (already `--format json`) | `step_finish` `part.tokens` | `input→input`, `output→output`, `reasoning→reasoning`, `cache.read→cached_input`, `cache.write→cache_write` | per-step increments; **summed** across events | `part.cost` (USD, summed) |
| copilot | `--output-format json` (headless only) | token metric events in JSONL output (per fixture) | per recorded fixtures | expected increments; summed (confirm via fixtures) | none captured (GitHub AI Credits, not USD, not converted) |
| cursor | none (already `--output-format stream-json`) | `usage` on result events | per recorded fixtures | best-effort; published result schema omits usage | none |

Canonical category keys are the `model` constants (`input`, `cached_input`, `cache_write`, `output`, `reasoning`); unknown vendor categories are preserved as `other:<vendor-key>`, never dropped. Every record carries provenance: `CLI` (adapter name), `Provider`/`Model` when the CLI reports them, `Source` (e.g. `claude:result-event`, `codex:turn.completed`), and `Completeness` (`partial` when the CLI reported only a subset of the categories its adapter expects).

### Adapter specifics

**Claude** (the largest change): headless invocations switch from plain text to `--output-format stream-json --verbose` — added in `BuildArgs` for the autonomous-headless invocation context ONLY; interactive args unchanged. The adapter must implement:

- `StdoutWrapper` — stream assistant text deltas live so the TUI keeps showing output mid-step.
- `OutputFilter` — yield the final `result` event's plain text for capture variables and display; the result text must not be displayed a second time if its content already streamed.
- `UsageExtractor` — read `usage` and `total_cost_usd` from the final `result` event.

Tests must assert: capture text equals the plain-text response (not the JSON envelope), streamed display text, no double display, interactive args untouched, and usage/cost extraction from a recorded stream-json fixture.

**Codex**: reuse the existing `turn.completed` parsing in `codex.go` and stop discarding `usage`. Values are cumulative session totals: put them in `RawCumulative`, leave `Tokens` empty, `Source: "codex:turn.completed"`. No cost.

**OpenCode**: sum `part.tokens` across `step_finish` events and sum `part.cost` (USD). A known race can drop the final stdout event: zero `step_finish` events → `no-usage-event` unavailable; if some events arrived, sum what was received (never fabricate the missing tail).

**Copilot** adds `--output-format json` to headless invocations only (interactive args unchanged), which puts it under the `Structured headless output preserves plain-text capture` requirement: its adapter must implement `OutputFilter` to extract the agent's plain-text response from the JSONL for capture variables and display, and `UsageExtractor` to read the token metric events. Copilot's `cost` field is GitHub AI Credits, not USD — never captured, cost stays nil.

**Cursor** (fixture-first, like Copilot): record real CLI JSONL output as testdata fixtures by running the CLI if available. If it cannot be run, fall back to the research notes in `openspec/changes/cost-tracking/proposal.md` (Cursor: a `usage` object with `inputTokens`/`outputTokens` in recent versions) and clearly note the assumption in the commit message. Cursor is best-effort: an absent `usage` object → `no-usage-event` unavailable, never an error.

Both adapters follow the fixture-first pattern: record the fixture first, then implement against it. If a CLI is unavailable, fall back to proposal research notes and note the assumption in the commit message.

### Conventions

TDD (failing fixture-driven test first per adapter), tests next to the package, `google/go-cmp`, local stubs over mocking frameworks, `make fmt`, `make test`, `make lint`. Commit style: `type: lowercase description` (e.g. `feat: add usage extraction to cli adapters`).

## Spec

### Requirement: Adapter usage extraction

The adapter contract SHALL gain an optional usage-extraction capability. After an autonomous-headless agent step's CLI process exits, the runner SHALL invoke the adapter's usage extraction with the captured process output, and the adapter SHALL return the structured usage record — token categories, reported model, measurement source, and any CLI-reported USD cost — or an explicit unavailable result. Adapters that do not implement the capability SHALL yield an unavailable usage record.

An adapter's extraction SHALL report only what its CLI actually provides: token categories the CLI does not emit are absent, and cost is included only when the CLI reports a USD value (per `cost-capture`). Which adapters support extraction, and what each extracts from where, is a design/implementation concern.

Extraction failures SHALL NOT fail the step: a step whose CLI exited successfully but whose usage cannot be parsed completes normally with unavailable usage.

#### Scenario: Runner invokes extraction after headless exit
- **WHEN** an autonomous-headless agent step's CLI process exits and the adapter implements usage extraction
- **THEN** the runner obtains the step's usage record from the adapter's parse of the captured output

#### Scenario: Adapter without extraction yields unavailable
- **WHEN** an autonomous-headless step runs with an adapter that does not implement usage extraction
- **THEN** the step's usage record is an explicit unavailable state

#### Scenario: Adapter supports usage but not cost
- **WHEN** an adapter's CLI reports token usage but no USD cost
- **THEN** the extracted usage record carries the token categories and no cost value

#### Scenario: Extraction failure does not fail the step
- **WHEN** a CLI exits with code 0 but its output cannot be parsed for usage
- **THEN** the step's outcome is unchanged (success) and its usage record is unavailable

### Requirement: Structured headless output preserves plain-text capture

When an adapter must request a structured output format in its autonomous-headless invocation args for usage data to appear on stdout, the adapter SHALL add that flag to headless invocations only, and SHALL extract the agent's plain-text response from the structured output for capture variables and display. Interactive invocation args SHALL be unchanged.

#### Scenario: Adapter requests structured output for headless runs
- **WHEN** an adapter whose CLI only reports usage in a structured output mode builds args for an autonomous-headless step
- **THEN** the args include the CLI's structured-output flag alongside the existing headless flags

#### Scenario: Capture variable receives plain text
- **WHEN** an autonomous-headless step using such an adapter has `capture:` set and completes
- **THEN** the captured variable contains the agent's plain-text response, not the structured envelope

#### Scenario: Interactive args unchanged
- **WHEN** such an adapter builds args for an interactive step
- **THEN** no structured-output flag is added (interactive sessions are unaffected)

### Requirement: Usage collection on autonomous-headless agent steps

After an autonomous-headless agent step's CLI process exits, Agent Runner SHALL obtain a token-usage record for that step by extracting structured usage data from the CLI's captured stdout via the step's CLI adapter. Extraction SHALL be attempted for every autonomous-headless agent step regardless of which CLI it uses and regardless of the step's exit code: usage and cost successfully extracted from a failed step's output SHALL be retained and included in run-level aggregates, since those tokens were consumed regardless of outcome.

#### Scenario: Adapter supports usage extraction
- **WHEN** an autonomous-headless agent step completes, its adapter supports usage extraction, and the CLI's structured output contains usage data
- **THEN** the step's usage record contains the token counts the CLI reported

#### Scenario: Adapter does not support usage extraction
- **WHEN** an autonomous-headless agent step completes and its adapter does not support usage extraction
- **THEN** the step's usage record is recorded as unavailable (not zero)

#### Scenario: Failed step retains extracted usage
- **WHEN** an autonomous-headless agent step exits with a nonzero code but its structured output contains valid usage data
- **THEN** the step's usage record contains the reported counts (and any reported cost is captured per `cost-capture`), and the step's usage and cost are included in run-level aggregates

### Requirement: Distinct token categories

Usage records SHALL represent token counts in distinct categories — input, cached input, cache writes, output, and reasoning output — as provided by the CLI. A category the CLI does not report SHALL be recorded as absent, not as zero. Agent Runner SHALL NOT collapse categories into a single total at collection time.

#### Scenario: Categories preserved as reported
- **WHEN** a CLI reports input, cached-input, output, and reasoning-output counts
- **THEN** the usage record stores each category separately with its reported value

#### Scenario: Unreported category is absent, not zero
- **WHEN** a CLI's output provides no count for a token category (for example, no cache-write count)
- **THEN** the usage record marks that category as absent rather than recording `0`

### Requirement: Unavailable usage is explicit

When usage cannot be collected for an agent step, Agent Runner SHALL record an explicit unavailable state with the reason. Missing usage SHALL never be represented as zero tokens. Situations that produce an unavailable record include: PTY-backed invocation contexts (interactive and autonomous-interactive), structured-output parse failures, missing usage events in otherwise valid output, and adapters that do not support extraction.

#### Scenario: PTY-backed agent step reports unavailable
- **WHEN** an agent step runs in an interactive or autonomous-interactive context (no stdout captured)
- **THEN** the step's usage record is an explicit unavailable state with a reason indicating the invocation context

#### Scenario: Parse failure reports unavailable
- **WHEN** an autonomous-headless agent step completes but its stdout cannot be parsed as the expected structured format
- **THEN** the step's usage record is an explicit unavailable state, the step's outcome is otherwise unaffected, and no zero counts are recorded

#### Scenario: Missing usage event reports unavailable
- **WHEN** an autonomous-headless agent step's structured output is otherwise valid but ends without the event that carries usage data
- **THEN** the step's usage record is an explicit unavailable state

### Requirement: Usage record provenance

Every usage record SHALL retain provenance alongside the token counts: the CLI (adapter) that produced it, the provider where available, the actual model reported by the CLI where available, the measurement source (e.g. which output event supplied the data), and a completeness indicator distinguishing full, partial, and unavailable measurements.

#### Scenario: Provenance recorded with usage
- **WHEN** a usage record is collected from a completed agent step
- **THEN** the record includes the CLI name, the provider and reported model (when the CLI provides them), the measurement source, and a completeness indicator

#### Scenario: Partial measurement flagged
- **WHEN** a CLI reports only a subset of the token categories its adapter expects it to provide
- **THEN** the record's completeness indicator reflects a partial measurement

### Requirement: CLI-reported cost captured verbatim

Agent Runner SHALL record a step's cost as `estimated_api_cost_usd` only when the step's CLI itself reports a cost denominated in USD, and SHALL record the CLI's value verbatim. Agent Runner SHALL NOT compute cost from token counts: it maintains no pricing catalog, performs no token-times-rate math, fetches no prices, and converts no non-USD denominations.

#### Scenario: CLI reports a USD cost
- **WHEN** an autonomous-headless agent step completes and its CLI's structured output reports a USD-denominated cost
- **THEN** the step's `estimated_api_cost_usd` equals the reported value

#### Scenario: CLI reports no cost
- **WHEN** an autonomous-headless agent step completes and its CLI's output contains no cost field
- **THEN** the step's `estimated_api_cost_usd` is null

#### Scenario: Non-USD cost units are not converted
- **WHEN** an autonomous-headless agent step completes and its CLI reports cost only in a non-USD unit (such as a proprietary credit)
- **THEN** the step's `estimated_api_cost_usd` is null and no unit conversion is performed

### Requirement: Cost separated from raw usage

The captured cost SHALL be stored as a distinct field alongside — not merged into — the raw usage record (token categories and provenance). Consumers SHALL be able to read token counts independently of whether a cost was reported.

#### Scenario: Tokens present, cost absent
- **WHEN** an agent step's CLI reports token usage but no cost
- **THEN** the step's record contains the full token-category counts and a null cost, and both are independently readable

### Requirement: Agent step-specific data (relevant scenarios)

The `step_end` SHALL include exit code, discovered session ID, the step's token-usage record, and `estimated_api_cost_usd`. The usage record on `step_end` SHALL follow the `agent-usage-collection` capability: distinct token categories with provenance and completeness, or an explicit unavailable state. `estimated_api_cost_usd` SHALL follow the `cost-capture` capability: the CLI-reported USD value, or null. Unavailable usage and absent cost SHALL be emitted as explicit null/unavailable values, never as zeros.

(This requirement in `specs/audit-log-entries/spec.md` also covers pre-existing `step_start` model-resolution behavior; those scenarios are already implemented and must keep passing.)

#### Scenario: Agent step end includes usage and cost
- **WHEN** an autonomous-headless agent step completes with collected usage and a CLI-reported cost
- **THEN** the `step_end` entry includes the token-usage record (categories, provenance, completeness) and `estimated_api_cost_usd`

#### Scenario: Agent step end with unavailable usage
- **WHEN** an agent step completes but usage could not be collected (PTY-backed context or parse failure)
- **THEN** the `step_end` entry carries an explicit unavailable usage state and a null `estimated_api_cost_usd`; no zero counts are emitted

## Done When

Fixture-driven extraction tests pass for all five adapters; Claude adapter tests additionally cover capture text, live streaming, no double display, and unchanged interactive args; executor tests cover the extraction hookup (supported adapter, unsupported adapter, parse failure, PTY skip, failed-step retention). End to end, a headless Claude, Codex, or OpenCode step records usage (and cost where reported) in `step_end` audit entries and `run-metrics.json`. `make test` and `make lint` are clean.
