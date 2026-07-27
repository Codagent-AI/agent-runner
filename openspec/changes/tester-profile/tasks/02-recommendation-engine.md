# Task: Build Recommendation Engine

## Goal

Create a pure, deterministic four-role recommendation engine that selects exact discovered model identifiers or safe CLI defaults, prefers independent known model families for paired roles, and exposes enough explanation metadata for native setup.

## Background

Add `internal/profilerecommend` as an I/O-free domain package. It must not run subprocesses, read files or settings, or depend on Bubble Tea. Define a compact immutable discovery snapshot that preserves detected-CLI order, each CLI's discovered model order, and any per-CLI discovery error. Model canonical roles, selected CLI and optional exact model identifier, recognized family/tier, pair relationships, fallback state, and diversity limitations.

Lead and Crosscheck share one pinned CLI-order definition; Tester uses that order independently, and Implementor owns its separate order. Ranking is CLI-major after any diversity filter. Within a CLI, apply family precedence, preferred tier, other recognized tier, then the model-less CLI default. Discovery order breaks ties. Concrete returned identifiers must be preserved exactly.

Keep family/tier recognition bounded to the approved Claude Opus/Sonnet and boundary-safe GPT Sol/Terra rules. Do not build a comprehensive model catalog and do not specially recognize Gemini. Multi-provider CLI defaults have unknown family; concrete unclassified identifiers are `other`; neither can prove diversity. Expose unclassified models for customization but never automatically select the first merely because discovery returned it first.

For the shared `Diversity fallback is explained` scenario, this pure package owns detecting the limitation, applying normal precedence, avoiding a false diversity claim, and returning metadata that identifies the affected pair. `tasks/03-recommendation-first-setup.md` owns rendering that metadata visibly in the recommendation summary.

Use table-driven tests in the new package for policy ordering, paired-role recomputation, unknown-family behavior, exact-identifier preservation, accepted GPT shapes, false-positive boundaries, version comparison, equal/unparseable version ordering, discovery errors, empty results, and the worked recommendations from `design.md`.

## Spec

### Requirement: Four-role profile recommendations

The native setup profile editor SHALL build a concrete recommendation for `lead`, `crosscheck`, `implementor`, and `tester` from the CLI adapters detected on the host and the models discovered through those adapters. Each recommendation SHALL identify a CLI and either a recognized concrete model or the adapter default when no recognized tier is available.

The editor SHALL order recommendation candidates as follows:

- `lead`: CLI precedence `claude`, `codex`, `opencode`, `copilot`, `cursor`; family precedence Claude, OpenAI GPT, other; flagship model preference.
- `crosscheck`: the same pinned CLI precedence as `lead`; family precedence OpenAI GPT, Claude, other; flagship model preference.
- `implementor`: CLI precedence `codex`, `cursor`, `opencode`, `claude`, `copilot`; family precedence OpenAI GPT, Claude, other; balanced model preference.
- `tester`: the same pinned CLI precedence as `lead`; family precedence Claude, OpenAI GPT, other; balanced model preference.

After applying any diversity filter, the editor SHALL evaluate CLIs in the role's pinned CLI precedence order. Within each CLI, it SHALL apply the role's family precedence, then its preferred-tier and fallback rules. Discovery order SHALL break otherwise equal candidate rankings.

When the selected lead has a recognized family and at least one candidate has a known, different family, the editor SHALL exclude same-family, `other`, and unknown candidates from Crosscheck's automatic recommendation before applying normal precedence. For Tester, the editor SHALL apply the same rule against the selected Implementor. A concrete model SHALL be treated as Claude or OpenAI GPT only when its CLI or boundary-delimited identifier establishes that family. A model-less default from `claude` SHALL be treated as Claude, a model-less default from `codex` SHALL be treated as OpenAI GPT, and a model-less default from a multi-provider CLI SHALL have unknown family. `other` and unknown candidates SHALL remain available during customization and SHALL remain eligible for automatic recommendation when known diversity cannot be established, but they SHALL NOT establish diversity. When diversity cannot be established, the editor SHALL apply normal precedence and explain the limitation on the recommendation surface.

The maintained power-tier policy SHALL remain bounded to:

- Claude flagship: a discovered identifier containing the whole token `opus`
- Claude balanced: a discovered identifier containing the whole token `sonnet`
- OpenAI GPT flagship: a discovered identifier containing a boundary-safe `gpt` marker or `gpt` immediately followed by a digit, plus a boundary-safe `sol` marker
- OpenAI GPT balanced: a discovered identifier containing a boundary-safe `gpt` marker or `gpt` immediately followed by a digit, plus a boundary-safe `terra` marker

GPT matching SHALL be case-insensitive, SHALL treat common provider and punctuation separators as token boundaries, and SHALL NOT match `sol` or `terra` as substrings of larger tokens. When multiple GPT identifiers match one tier, the editor SHALL select the highest numeric version immediately following the GPT marker; equal or unparseable versions SHALL retain discovery order. The editor SHALL persist the exact discovered identifier rather than a normalized alias or wildcard.

For a role whose preferred recognized tier is unavailable, the editor SHALL prefer the other recognized tier in the selected family, then the CLI default with the model unset. Models outside the bounded tier policy SHALL remain available in discovery order during customization but SHALL NOT be selected automatically merely because they were returned first.

#### Scenario: Claude and Codex produce the standard recommendation
- **WHEN** `claude` exposes `opus` and `sonnet` and `codex` exposes `gpt-5.6-sol` and `gpt-5.6-terra`
- **THEN** the editor recommends `claude` with `opus` for lead, `codex` with `gpt-5.6-sol` for crosscheck, `codex` with `gpt-5.6-terra` for implementor, and `claude` with `sonnet` for tester

#### Scenario: Crosscheck differs from lead when possible
- **WHEN** the selected lead family is Claude and at least one OpenAI GPT candidate is available
- **THEN** the editor recommends a crosscheck candidate whose known family is OpenAI GPT according to crosscheck precedence

#### Scenario: Tester differs from implementor when possible
- **WHEN** the selected implementor family is OpenAI GPT and at least one Claude candidate is available
- **THEN** the editor recommends a non-GPT tester candidate according to tester precedence

#### Scenario: One multi-provider CLI supplies independent families
- **WHEN** only one supported CLI is detected and it exposes both Claude and OpenAI GPT models
- **THEN** the editor SHALL recommend that CLI for paired roles while recommending different model families according to role precedence

#### Scenario: Diversity fallback is explained
- **WHEN** every eligible crosscheck or tester candidate has the paired creator's family or an unknown or unclassified family
- **THEN** the editor applies normal precedence without claiming family diversity and visibly explains which pair could not be diversified

#### Scenario: Multi-provider CLI default has unknown family
- **WHEN** the selected lead or implementor uses a model-less default from OpenCode, Copilot, or Cursor
- **THEN** the editor does not infer a family from the CLI name
- **AND** it claims diversity only if the creator and evaluator later have concrete, recognized, different families

#### Scenario: CLI precedence is evaluated before family precedence
- **WHEN** the user selects an OpenCode default with unknown family for lead, Claude exposes `opus`, and Cursor exposes `gpt-5.7-sol`
- **THEN** Crosscheck recommends Claude with `opus` because Claude precedes Cursor in Crosscheck's CLI order even though Crosscheck prefers the GPT family within a CLI

#### Scenario: Balanced model is unavailable
- **WHEN** a balanced role selects a family whose balanced recognized alias is unavailable but whose flagship recognized alias is available
- **THEN** the editor recommends the flagship alias

#### Scenario: Latest flexible GPT tier match is selected
- **WHEN** a CLI discovers `openai:gpt_5_7_terra`, `gpt-5.6-terra`, and `gpt-5.7-sol-latest`
- **THEN** a balanced GPT role selects the exact identifier `openai:gpt_5_7_terra`
- **AND** a flagship GPT role selects the exact identifier `gpt-5.7-sol-latest`

#### Scenario: GPT marker may be immediately followed by a version
- **WHEN** a CLI discovers `gpt5.7-sol-latest`
- **THEN** the identifier is classified as a GPT flagship tier model with version 5.7

#### Scenario: Tier markers require safe boundaries
- **WHEN** a CLI discovers `solar-model`, `terrain-large`, and `claude-sol`
- **THEN** none of those identifiers is classified as a GPT Sol or Terra tier

#### Scenario: Recognized models are unavailable
- **WHEN** a selected CLI returns models but none match a recognized tier
- **THEN** the editor recommends the CLI default with its model unset
- **AND** preserves every returned model in discovery order for customization

#### Scenario: Model discovery fails for one CLI
- **WHEN** model discovery fails for a detected CLI
- **THEN** the editor keeps that CLI eligible with its model unset, displays the discovery limitation, and continues building the four-role recommendation

#### Scenario: No models are discoverable
- **WHEN** a recommended CLI returns no models
- **THEN** the recommendation identifies that CLI's adapter default and leaves its model unset

## Done When

- `internal/profilerecommend` exposes a stable, pure recommendation API over immutable discovery input and contains no subprocess, filesystem, settings, or TUI behavior.
- Every engine-owned policy and scenario portion above has focused table-driven coverage, including exact worked examples, accepted/rejected identifier shapes, and diversity-limitation metadata naming the affected pair.
- Recommendations are deterministic regardless of concurrent discovery completion order because the input preserves detector and per-CLI source order explicitly.
- Selection returns only exact discovered identifiers or an unset model representing the adapter default, with explanation metadata sufficient for setup summary and fallback copy.
- `go test ./internal/profilerecommend` passes, followed by `make fmt`, `make test`, and `make lint`.
