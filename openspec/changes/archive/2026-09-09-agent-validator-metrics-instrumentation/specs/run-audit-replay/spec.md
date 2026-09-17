## MODIFIED Requirements

### Requirement: Historical replay excludes evidence without durable session ownership

An explicit replay SHALL include quantitative records and other evidence only when Agent Runner can attribute them to the selected execution session or an earlier session relevant to its lineage. Detailed outputs, validation records, artifacts, narratives, native-session material, and other evidence without durable execution-session ownership SHALL be excluded from the replay snapshot and marked unavailable rather than exposed cumulatively or attributed by inference.

For v4 artifacts, replay filtering SHALL apply to authoritative measurement heads, invocation summaries, Runner attribution, delivery gaps, and derived metric views as well as legacy step/session records. Selected-session observations SHALL include only work originally attributable to that session; earlier attributable work MAY remain lineage evidence, and later-session work SHALL be excluded. An imported revision's recovery time SHALL not replace its original execution-session ownership. After explicit metrics recovery, a new audit replay SHALL use the current durably incorporated heads present at its snapshot boundary without changing any earlier audit report. Replay itself SHALL not export or acknowledge Validator telemetry or modify the source metrics.

#### Scenario: Later execution added ambiguous detail
- **WHEN** an earlier execution session is replayed after a later session has added outputs or artifacts that lack durable session ownership
- **THEN** the replay snapshot excludes those ambiguous details and reports the affected evidence categories as unavailable

#### Scenario: Session-owned metrics are replayed
- **WHEN** durable metrics identify the execution sessions that produced their records
- **THEN** the replay retains records through the selected session for selected-session evaluation and prior-session lineage while excluding later-session records

#### Scenario: Replay includes later recovery of earlier work
- **WHEN** metrics recovery durably imports a Validator completion belonging to an earlier session and that session is explicitly audited again
- **THEN** the new replay snapshot includes the current recovered head under its original session and owning leaf, with a new audit identity and unchanged prior audit reports

#### Scenario: Replay excludes later-session measurements
- **WHEN** an earlier session is replayed from a v4 artifact containing later-session native and Validator attempts and delivery contexts
- **THEN** later-session heads, invocation summaries, gaps, and rollup contributions do not enter the earlier session's evidence or totals

#### Scenario: Unresolved recovery remains incomplete in replay
- **WHEN** the selected session still has partial measurements or unresolved delivery at replay snapshot time
- **THEN** the replay preserves those limitations without initiating metrics retrieval, inventing complete totals, or mutating source evidence
