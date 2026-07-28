## Context

Agent Runner currently exposes built-in `planner`, `reviewer`, and `implementor` profiles. Native setup configures only planner and implementor, discovers models only after each CLI selection, and writes a fixed two-role payload. Acceptance testing therefore reuses Reviewer even though planning scrutiny and acceptance-flow testing need different role configuration.

The role names also obscure their responsibilities. The interactive planning agent acts as the user-facing lead, while the second planning agent independently challenges the lead's artifacts and checks for omissions. Calling that second role Reviewer is easily confused with implementation-code review, which belongs to Agent Validator.

This change crosses configuration loading, built-in defaults, native setup, model discovery, profile persistence, embedded workflow YAML, and product documentation. It must keep existing `planner` and `reviewer` configurations and workflow references usable, avoid a large maintained model catalog, preserve deterministic recommendations despite concurrent discovery, and retain the existing setup persistence and cancellation boundaries.

## Goals / Non-Goals

**Goals:**

- Establish canonical `lead`, `crosscheck`, `implementor`, and `tester` profiles.
- Preserve `planner` and `reviewer` as deprecated, undated compatibility aliases.
- Give setup one deterministic four-role recommendation derived from the CLIs and models available on the host.
- Prefer independent known model families for Lead/Crosscheck and Implementor/Tester.
- Select Claude aliases and the newest discovered GPT Sol/Terra tier without hard-coding a GPT version.
- Keep recommendation policy pure, centralized, and independently testable.
- Keep setup responsive by discovering models concurrently once per session.
- Persist direct canonical role entries without disturbing unrelated configuration.
- Route acceptance testing through Tester while preserving existing acceptance retry, evidence, and session behavior.
- Explain role boundaries, ordering rationale, examples, and compatibility in product documentation.

**Non-Goals:**

- Crosscheck does not review implementation code or replace Agent Validator.
- The recommendation engine will not maintain a comprehensive model catalog.
- Gemini receives no special family recognition or preference.
- Unclassified discovered models will not be guessed as automatic recommendations.
- Legacy configuration files will not be rewritten merely because they are loaded.
- This change does not schedule removal of the legacy names.
- Existing autonomous-backend settings will not be migrated automatically.
- Backend copy will not make provider-billing claims.
- Acceptance workflow retry limits, verification scopes, evidence handling, and fresh-session behavior will not change.

## Approach

### Pure recommendation engine

Add `internal/profilerecommend` as a pure domain package. It owns role definitions, pairing, precedence, model classification, tier matching, deterministic selection, and explanation metadata. It accepts an immutable discovery snapshot and performs no subprocess, filesystem, settings, or TUI work.

The package models:

- the canonical role;
- an ordered list of detected CLIs;
- the models, discovery order, and optional discovery error for each CLI;
- a selected CLI and optional exact model identifier;
- recognized family and power tier;
- fallback or diversity limitations to display in setup.

Lead and Crosscheck reference the same CLI-order definition rather than duplicate slices that can drift. Tester uses that order independently. Implementor owns its separate order.

Keeping this policy out of `internal/onboarding/native` avoids embedding product ranking and family classification in a Bubble Tea state machine that already owns rendering, transitions, plugin installation, settings, and persistence. Putting the policy in `internal/cli` was rejected because role-specific recommendation policy is not an execution-adapter responsibility.

### Concurrent immutable discovery

Native setup begins in a dedicated loading stage. Its Bubble Tea `Init` command:

1. detects supported CLIs;
2. starts model discovery for every detected CLI concurrently;
3. stores results by CLI while retaining detector and per-CLI discovery order; and
4. returns one aggregate message after all queries complete.

At most the five supported CLI queries run concurrently, and each retains the existing subprocess timeout. Completion order never affects the aggregate snapshot or recommendation order.

The snapshot remains immutable for the setup session. Accept-all, customization, and Lead/Crosscheck or Implementor/Tester recomputation reuse it without another subprocess. A user whose installed models change while setup is open must cancel and restart setup. This trade-off keeps the recommendation summary and later customization consistent.

Sequential discovery was rejected because five independent timeouts could multiply startup latency. Incremental per-CLI Bubble Tea messages were rejected because the summary is not actionable until the full snapshot exists and partial-message bookkeeping adds state complexity.

### Recommendation policy

The role policies are:

| Role | CLI precedence | Family preference | Tier | Diversity relationship |
| --- | --- | --- | --- | --- |
| Lead | Claude, Codex, OpenCode, Copilot, Cursor | Claude, GPT, other | Flagship | Establishes the family Crosscheck avoids |
| Crosscheck | Same pinned order as Lead | GPT, Claude, other | Flagship | Prefer a known family different from Lead |
| Implementor | Codex, Cursor, OpenCode, Claude, Copilot | GPT, Claude, other | Balanced | Establishes the family Tester avoids |
| Tester | Same pinned order as Lead | Claude, GPT, other | Balanced | Prefer a known family different from Implementor |

For Crosscheck and Tester, the engine first checks whether at least one candidate has a recognized family different from its paired creator. If so, same-family and unclassified candidates are excluded for that recommendation before normal precedence. If the creator is unknown or no known different candidate exists, normal precedence applies and the result explains that diversity could not be established. Manual customization may select a same-family model.

After diversity filtering, ranking is CLI-major: the engine walks the role's pinned CLI order, applies family precedence and tier fallback within each CLI, and uses discovery order for otherwise equal candidates. This preserves the product and cost intent of the CLI order while allowing multi-provider CLIs to prefer the role-appropriate family.

The practical ordering rationale is:

- Lead prioritizes strong interactive reasoning.
- Crosscheck shares Lead's CLI preferences while family filtering supplies an independent planning perspective.
- Implementor prioritizes Codex, then Cursor and OpenCode for strong, less expensive implementation options.
- Claude is lower for implementation because of cost.
- Copilot is last for implementation because of its increased cost.
- Tester uses a balanced tier and prefers independence from Implementor.

These are overridable product defaults, not universal model-quality rankings.

### Family and tier matching

The selected model value is always an exact identifier returned by discovery. Matchers choose among discovered identifiers; wildcard or normalized values are never written to configuration.

Dedicated CLI defaults establish family only for single-provider adapters:

- Claude default → Claude family
- Codex default → GPT family
- OpenCode, Copilot, or Cursor default → unknown family

For a concrete identifier on a multi-provider CLI, case-insensitive, boundary-delimited Claude or GPT markers may establish the family. Concrete identifiers without a recognized family remain `other`; model-less multi-provider defaults remain `unknown`. Neither `other` nor `unknown` proves diversity.

Claude tier matching recognizes the whole token `opus` as flagship and `sonnet` as balanced. GPT tier matching:

1. lowercases only for comparison while preserving the original identifier;
2. treats `/`, `:`, `@`, `-`, `_`, `.`, whitespace, and similar punctuation as boundaries;
3. requires a whole `gpt` marker or `gpt` immediately followed by a digit;
4. requires a whole `sol` or `terra` marker rather than substrings such as `solar` or `terrain`;
5. parses numeric components immediately following GPT; and
6. selects the highest numeric version, preserving discovery order for equal or unparseable versions.

Examples of accepted GPT shapes include `gpt-5.7-sol`, `openai/gpt-5.7-sol`, `openai:gpt_5_7_terra`, `gpt5.7-sol-latest`, and `vendor/openai-gpt-5.7-codex-terra`. `claude-sol`, `solar-model`, and `terrain-large` are not GPT tier matches.

Within a selected CLI, recommendation fallback is:

1. preferred recognized tier;
2. other recognized tier;
3. CLI default with model unset.

Every unclassified discovered model remains visible in discovery order during customization. The engine does not automatically select the first one because provider output may be alphabetical or otherwise unrelated to quality.

When a recommendation uses the CLI default despite discovered unclassified models, the customization model screen prepends and focuses a synthetic `Use CLI default` option, followed by every discovered model. Selecting that option preserves the unset model. Empty or failed discovery continues to use the dedicated default-model explanation surface.

### Native setup state and progress

Native setup adds recommendation-loading and recommendation-summary stages, then uses a generic role cursor for Lead, Crosscheck, Implementor, and Tester customization. Generic CLI, model, and model-default sub-states replace duplicated per-role state.

Accept-all freezes the four displayed selections. Customize iterates Lead, Crosscheck, Implementor, then Tester. Finalizing Lead recomputes Crosscheck from the immutable snapshot; finalizing Implementor similarly recomputes Tester.

Wizard progress derives from a semantic step plan rather than fixed arithmetic:

- recommendation summary;
- optionally one step for each of the four roles;
- autonomous backend;
- autonomous permission mode;
- applicable plugin, scope, overwrite, and demo steps.

Loading has no progress counter. A role's CLI, model, and default sub-states all map to the same semantic step. Overwrite contributes a step only when collisions exist.

The summary and role copy distinguish responsibilities. Crosscheck challenges Lead's proposal, specification, design, task, and assumption work and looks for omissions. Agent Validator performs implementation-code checks and reviews. Tester exercises implemented acceptance flows.

### Typed profile persistence

Expand `internal/profilewrite.Request` into an explicit four-role request containing a CLI and optional model for each canonical role plus the target path. The writer derives modes instead of accepting them:

- Lead: interactive
- Crosscheck, Implementor, Tester: autonomous

The existing shared YAML-node merge and atomic write path remains authoritative. It writes direct `lead`, `crosscheck`, `implementor`, and `tester` entries, omits empty model fields, preserves unmanaged agents and unrelated configuration, and maintains the existing directory and file modes.

Collision detection treats canonical entries and legacy `planner` and `reviewer` entries as managed. After explicit overwrite confirmation, the writer removes the two legacy aliases from the selected profile layer and writes the canonical four-role shape. Loading configuration alone never mutates a file.

The internal `write-profile` command remains a wrapper over this shared writer and accepts the new typed four-role payload. The hidden internal payload is not a compatibility surface for the legacy two-role JSON shape.

### Canonical names and legacy compatibility

Configuration loading canonicalizes names within each layer before normal global/project merging:

- `planner` → `lead`
- `reviewer` → `crosscheck`

The same canonicalizer applies to profile entries, `extends` targets, workflow agent references, and runtime `call_agent` targets. This ensures a legacy user override affects a canonical built-in workflow and a legacy workflow reference uses a canonical user profile.

If one profile layer declares both names from an alias pair, validation fails with an actionable ambiguity error rather than silently choosing one. Higher-layer synonyms continue to override lower-layer values after canonicalization.

Canonicalization returns structured deprecation metadata rather than printing from the config package. The top-level command/run path emits each distinct warning at most once, for example:

`agent profile "planner" is deprecated; use "lead"`

The warnings are non-fatal, include no removal date, and do not imply a scheduled removal. Non-TUI command paths write them to stderr. Workflow and TUI paths surface them in user-visible run output and retain them with the run's diagnostic or audit information so they are not lost when direct process logging is suppressed. A future removal would require a separate breaking change.

### Built-ins, workflows, and documentation

The in-memory default profile set contains seven canonical agents: both bases, Lead, Crosscheck, Implementor, Tester, and Summarizer. Crosscheck extends the autonomous base. Tester is a direct autonomous Claude/Sonnet profile so an existing user override of `autonomous_base` cannot combine a non-Claude CLI with the Sonnet model.

Built-in workflow agent references move from Planner to Lead and from planning Reviewer to Crosscheck. Initial acceptance in `implement-change2` and requested targeted re-acceptance in `accept-change` use Tester. Those YAML edits do not alter call freshness, retries, verification scope, or evidence handling.

`docs/agent-profiles.md` becomes the authoritative explanation of:

- all four canonical roles;
- Crosscheck versus Agent Validator;
- role CLI order, family pairing, power tier, and quality/cost rationale;
- the worked recommendation examples below;
- customization and legacy alias behavior.

`docs/setup.md` describes discovery, recommendation summary, accept-all, customization, and headless backend recommendation. Other documentation and examples use canonical names while compatibility guidance documents the legacy aliases.

### Worked recommendations

With all five supported CLIs installed, Claude exposing Opus/Sonnet, and Codex exposing GPT 5.6 Sol/Terra:

| Role | Recommendation |
| --- | --- |
| Lead | Claude / Opus |
| Crosscheck | Codex / `gpt-5.6-sol` |
| Implementor | Codex / `gpt-5.6-terra` |
| Tester | Claude / Sonnet |

If the user changes Lead to Codex:

| Role | Recommendation |
| --- | --- |
| Lead | Codex / `gpt-5.6-sol` |
| Crosscheck | Claude / Opus |
| Implementor | Codex / `gpt-5.6-terra` |
| Tester | Claude / Sonnet |

Crosscheck recomputes away from Lead's GPT family; the Implementor/Tester pair is unchanged.

If Claude and Codex are absent while OpenCode, Copilot, and Cursor are present, and no remaining discovered model matches a recognized tier:

| Role | Recommendation |
| --- | --- |
| Lead | OpenCode / CLI default |
| Crosscheck | OpenCode / CLI default |
| Implementor | Cursor / CLI default |
| Tester | OpenCode / CLI default |

The summary explains that diversity could not be established because those CLI-default model families are unknown.

## Decisions

### Separate pure policy from TUI orchestration

Use `internal/profilerecommend` and keep I/O in native setup. Expanding explicit TUI helpers was rejected because it couples ranking policy to rendering. A general setup service was rejected as unnecessary indirection.

### Discover once and concurrently

Use one immutable aggregate snapshot. Sequential discovery was rejected for latency; re-querying during customization was rejected for latency and inconsistent state.

### Use canonical names with real resolver compatibility

Normalize aliases rather than adding built-in alias profiles. Built-in aliases alone would not make a legacy user override affect a canonical workflow. Same-layer conflicts are errors; silently preferring the canonical name was rejected because it discards configuration without notice.

### Warn without scheduling removal

Legacy aliases remain supported and warn once per alias per command/run. Immediate removal would violate the compatibility goal; a dated deprecation was rejected because no removal release is planned.

### Prefer safe defaults over unclassified guesses

When no recognized tier exists, use the CLI default and expose discovered models only for customization. Selecting the first unclassified result was rejected because discovery order is not a trustworthy quality signal.

### Keep model policy bounded but version-flexible

Recognize Claude Opus/Sonnet and structurally match GPT Sol/Terra while selecting the newest numeric version. Fixed GPT versions would require regular maintenance; a comprehensive catalog would expand scope and become stale.

## Risks / Trade-offs

- [Flexible GPT matching selects a false positive] → Require boundary-safe GPT and tier markers, compare numeric version components, preserve the exact identifier, and test accepted and rejected shapes.
- [Provider output changes] → Retain every model for customization and fall back to the CLI default instead of guessing.
- [Concurrent discovery becomes nondeterministic] → Store results by CLI, preserve source order explicitly, and recommend only after the aggregate completes.
- [Concurrent subprocesses increase short-lived resource use] → Bound concurrency to detected supported CLIs and retain per-query timeouts.
- [Legacy aliases create ambiguous configuration] → Reject same-layer alias pairs with actionable errors and normalize before cross-layer precedence.
- [Deprecation warnings become noisy] → Return structured warning metadata and deduplicate per alias at the command/run boundary.
- [Cost-based ordering becomes stale] → Centralize policy tables, document the rationale as practical rather than universal, and keep every role customizable.
- [Generic role-driven TUI refactor changes progress behavior] → Test semantic steps and every branch instead of asserting stage ordinals.
- [Older binaries do not understand canonical files written by new setup] → Never rewrite on load, migrate only after confirmation, and document the manual reverse rename for downgrade.

## Migration Plan

1. Add pure recommendation and alias-normalization tests before production changes.
2. Introduce canonical built-in Lead, Crosscheck, and Tester profiles and legacy resolution aliases.
3. Update native discovery, state, recommendation, progress, and four-role persistence.
4. Update embedded workflows to canonical names and route acceptance calls through Tester.
5. Update product documentation and examples with canonical names, ordering rationale, worked recommendations, and compatibility guidance.
6. Validate focused packages, run formatting, full tests, lint, and OpenSpec validation.

No file migration runs automatically. Existing legacy configs remain usable with warnings. Explicit native setup overwrite migrates the selected profile layer to canonical entries. To roll back after such migration, users can rename `lead` to `planner` and `crosscheck` to `reviewer` in that config before running an older binary.

## Open Questions

None.
