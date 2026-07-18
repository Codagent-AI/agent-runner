# cli-adapter Specification (delta)

## ADDED Requirements

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
