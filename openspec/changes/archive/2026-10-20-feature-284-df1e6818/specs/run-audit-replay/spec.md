## MODIFIED Requirements

### Requirement: Historical replay excludes evidence without durable session ownership

An explicit replay SHALL include quantitative records and other evidence only when Agent Runner can attribute them to the selected execution session or an earlier session relevant to its lineage. Detailed outputs, validation records, artifacts, narratives, native-session material, and other evidence without durable execution-session ownership SHALL be excluded from the replay snapshot and marked unavailable rather than exposed cumulatively or attributed by inference.

`audit.log` events carry their execution session and therefore have durable ownership. The replay snapshot SHALL include the `audit.log` events of the selected session and of earlier sessions in its lineage, and SHALL exclude events from later sessions. It SHALL also include the Runner-retained written-file versions, deltas, and working-tree patches referenced by those included `step_end` events, and bounded patches for commits attributed to the included sessions' steps. Run-directory files that are not referenced by an included event's written-file record SHALL remain excluded.

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

#### Scenario: Replay of a run recorded before written-file retention
- **WHEN** a session recorded before step written-file retention existed is replayed
- **THEN** the replay snapshot includes that session's `audit.log` events, its leaves include their recorded step output, and their written-file evidence is reported unavailable because it was not recorded

#### Scenario: Replay excludes a later session's step output
- **WHEN** an earlier session is replayed after a resumed later session re-ran the same step and recorded new output
- **THEN** the replay snapshot contains only the earlier session's output for that step, and the later session's output and retained files are excluded

#### Scenario: Replay retains overwritten round evidence
- **WHEN** a session's acceptance round 1 wrote a findings file that round 2 later overwrote, and the session is replayed
- **THEN** the replay snapshot contains round 1's retained version and delta attributed to round 1's step
