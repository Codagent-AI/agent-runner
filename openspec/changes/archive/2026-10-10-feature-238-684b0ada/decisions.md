# Decisions

## propose

- **Decision:** Go. Reconcile, then archive all seven changes as one spec-only change.
  - Alternatives: archive as written (rejected: overwrites newer requirements and fails on REMOVED entries for capabilities that no longer exist); delete the folders without archiving (rejected: contradicts the issue and loses the unsynced requirements).
  - Decision-bearing: no (Paul set the direction in the issue).
- **Decision:** Leave the `cli-adapter` autonomous-interactive yolo scenario as it is on `main`. Drop it from the `make-pty-great` delta and do not rewrite it to Claude `auto` mode.
  - Alternatives: rewrite it to `auto` mode now (rejected: PR #232 is still open, so `main` code still applies the headless yolo flags, and #232 already edits that spec itself); re-assert it as written (rejected: conflicts with #232).
  - Decision-bearing: yes. This assumes #232 owns that requirement whichever PR merges first.
- **Decision:** Archive `make-evals-great`, which holds only `.openspec.yaml`, with `--skip-specs`.
  - Alternatives: delete the folder (rejected: the issue says archive).
  - Decision-bearing: no.
- **Decision:** Mark `start-run` tasks complete after confirming that its packages and flow exist on `main`.
  - Alternatives: archive with unchecked tasks (rejected: misleading record); stop to ask (rejected: the issue states its implementation is finished).
  - Decision-bearing: no.
- **Decision:** Apply deltas oldest change first, and edit the delta files in place before each archive.
  - Alternatives: hand-edit the main specs and archive with `--skip-specs` (rejected: the archive would no longer record what was applied).
  - Decision-bearing: no.
- **Decision:** Do not fix spec/code mismatches found during reconciliation. Code wins, and any requirement found unimplemented is recorded for follow-up.
  - Alternatives: implement the missing behavior (rejected: the issue says no code changes).
  - Decision-bearing: no.

## proposal-review

- **PR-001 (significant): applied.** Confirmed in the installed OpenSpec `dist/core/specs-apply.js`: a `MODIFIED` block fails to archive if it leaves out any scenario already in the main spec. Also confirmed that `async-mcp`'s `step-control-channel` delta conflicts with the main spec's scenario "Lost agent-call client cancels leased execution", which `internal/control/control.go` contradicts. The proposal now does two things. It drops the whole `make-pty-great` "Adapters honor autonomous permission mode" `MODIFIED` block, not one scenario. It also allows reconciling a main spec directly and then archiving with `--skip-specs` when an archive cannot replace superseded scenarios, and records each such case here.
  - Alternatives: keep obsolete scenarios in deltas to pass the check (rejected: preserves contradictions); look for another archive path (rejected: none is supported by the installed CLI).
  - Decision-bearing: no.
- **PR-002 (minor): applied.** The final check is now `openspec validate --specs --strict --no-interactive`, which must exit successfully. Bare `openspec validate --strict` selects no targets when run without interaction.
  - Decision-bearing: no.

## spec

- **Decision:** This change's `specs/` carries one consolidated set of reconciled deltas. The seven old changes are archived as authored with `--skip-specs`. This replaces the proposal's earlier plan to edit each old change's deltas in place and archive it normally.
  - Alternatives:
    - Edit the old deltas in place and archive each (rejected): the hand-synced main specs make `ADDED` and `MODIFIED` fail, the scenario guard blocks retiring stale scenarios, and applying the same requirement twice (once from an old change, once from this change) would collide.
    - Hand-edit the main specs and give this change no deltas (rejected): the reconciliation could not be reviewed or validated as deltas.
  - Decision-bearing: no. It is an archiving mechanism within the issue's direction; the proposal was updated to match.
- **Decision:** Retire scenarios that cannot stay true by removing the requirement and adding a correctly named replacement: `step-control-channel` "Fresh authenticated attempt" becomes "Fresh authenticated attempt credential", and `agent-calls` "Cancellation propagation" becomes "Agent-call cancellation propagation". Where a scenario name stays truthful, keep it and correct its body.
  - Alternatives: keep false scenario names to satisfy the archive guard (rejected by PR-001).
  - Decision-bearing: no.
- **Decision:** Fold `workflow-definition-view` into `view-run`, and put the New tab rendering, keybinding, and search requirements into `new-tab-layout` rather than `list-runs`.
  - Alternatives: create a separate `workflow-definition-view` spec (rejected: `view-run` already owns the definition preview); keep the New tab requirements in `list-runs` (rejected: `new-tab-layout` is the newer owner).
  - Decision-bearing: no.
- **Decision:** Create a `call-agent-skill` main spec in this repo, written from the current `SKILL.md` in Codagent-AI/agent-skills.
  - Alternatives: omit it because the skill lives in another repo (rejected: this repo installs the skill through `internal/agentplugin`, its built-in workflows depend on it, and the old changes intended this spec).
  - Decision-bearing: no.
- **Decision:** Specify agent-call waiting as implemented: `call_agent` waits for the child with no time limit by default, and Cursor parents get a 45-second wait limit, after which they poll with `get_agent_call`. The async-mcp design's "return as soon as accepted" is not specified.
  - Alternatives: specify the design's behavior (rejected: the code wins).
  - Decision-bearing: no.
- **Decision:** Where the main spec's `MODIFIED` block had to be rewritten anyway, correct it fully to match the code, even beyond the old delta. Cases: the full `audit-log-entries` event list; `interactive-terminal-handoff` "Full suspension forwarding" now reflects SIGTTIN/SIGTTOU recovery.
  - Alternatives: copy only the old delta's content (rejected: the block would still contradict the code).
  - Decision-bearing: no.
- **Decision:** Leave the two `cli-adapter` requirements that PR #232 edits untouched. The make-pty-great scenarios missing from them are recorded as follow-up 6.
  - Alternatives: add the scenarios now (rejected: they would conflict textually with #232).
  - Decision-bearing: no.
- **Decision:** Record unimplemented clauses and code/spec mismatches outside this scope as follow-ups in `reconciliation.md`, and do not fix or specify them.
  - Alternatives: fix the code (rejected: the issue says spec only).
  - Decision-bearing: no.

## design

- **Decision:** Implementation only checks off `start-run`'s tasks and archives the seven old changes with `--skip-specs`. The factory's archive step applies this change's deltas.
  - Alternatives: archive this change during implementation (rejected: the factory archive step expects to perform that archive itself, and the specs would be applied twice).
  - Decision-bearing: no.
- **Decision:** Accept `Purpose: TBD` on the three new main specs.
  - Alternatives: pre-create stub main specs with a real purpose (rejected: tested, and an empty Requirements section fails `openspec validate --specs --strict`).
  - Decision-bearing: no.
- **Decision:** Verify with an in-tree check that `openspec/specs` has no diff, plus a temp-copy dry run of the factory archive followed by `openspec validate --specs --strict --no-interactive`.
  - Alternatives: run the archive in the real tree (rejected: it would preempt the factory step).
  - Decision-bearing: no.

## test-plan

- **Decision:** No new integration or end-to-end tests, and no human-only tests. Verification is OpenSpec validation, the archive dry run, the existing `go test ./...`, and an exploratory pass that checks the reconciled requirements against the code, existing tests, and the TUI under a PTY.
  - Alternatives: add tests for the specified behavior (rejected: the behavior already exists and is tested, the change is spec-only, and repository rules forbid new suites).
  - Decision-bearing: no.
- **Decision:** The acceptance pass may not launch real agent CLIs or run `openspec archive` in the real tree. It uses package tests and code reading as substitutes for live agent-call behavior.
  - Alternatives: live agent sessions (rejected: cost, personal credentials, and no added fidelity for spec text review).
  - Decision-bearing: no.

## approach-review

- **AR-001 (high): applied.** Confirmed: OpenSpec's apply step checks only that requirement and scenario names exist, so newer body text is silently overwritten. Changes:
  - `reconciliation.md` records the baseline (`main` at `629bd3d4`).
  - The design adds a required drift check: `git diff <baseline>` must be empty for every touched main spec, and none of the new capability folders may exist yet. Any drift is re-reconciled against the then-current code and newer text, and the refresh is recorded.
  - The full dry-run diff must be reviewed against `reconciliation.md`.
  - The test plan names this as the content check. The design states the residual risk of a resumed run that skips implementation.
  - Alternatives: rely on archive failures (rejected: shown not to detect drift); change the archive tooling or workflow (rejected: out of scope).
  - Decision-bearing: no.
- **AR-002 (medium): applied.** `agent-calls` "Parent session discovery excludes called children" now separates two things. The Runner collects and forwards child IDs (scenario "Exclusions are forwarded to parent discovery"). Exclusion and failing closed are scoped to Cursor, and the text says other adapters are not obliged. The other adapters stay as follow-up 4.
  - Alternatives: keep the general wording (rejected: it specifies behavior that doesn't exist).
  - Decision-bearing: no.
- **AR-003 (medium): applied.** Confirmed in `newtab.go:116` and the existing tests. The main `new-tab-layout` requirement "Workflow groups render with header and description" is removed and replaced by two requirements:
  - "Workflow group headers": the header scenarios unchanged, plus "Upward navigation from the first workflow skips the leading header".
  - "Plan with an agent entry": the initial cursor, Down and Up navigation, Enter/`r` starting intake, and the entry staying visible when the filter has no matches.
  - The search-filter requirement names that entry as the first selectable row. The OpenSpec apply rules make removing and re-adding the only way to drop a false scenario name.
  - Alternatives: leave it as a follow-up (rejected: it leaves two incompatible contracts in a capability this change edits).
  - Decision-bearing: no.
- **AR-004 (medium): applied.** "Universal completion surface" is now scoped to adapters that support the interactive context. OpenCode is removed from the pre-approving list, and the new scenario "CLI without interactive support fails before spawn" documents OpenCode's rejection (`opencode.go:156-165`, `exec/agent.go:484-493`).
  - Decision-bearing: no.
- **AR-005 (medium): applied.** `lightweight-audit-reporting` now states the deterministic note checks the code applies: over 280 characters, line breaks, `://`, `/`, `\`, `ghp_`, `sk-`, `token=`. It says these are the only automated filtering and that other content rests on the judge's instructions (verified in the `value_audit.go` prompt). "Note contains prohibited detail" now lists those triggers, and the reporter's MUST NOT list excludes note text that passes the checks. Semantic filtering is recorded as unimplemented.
  - Alternatives: keep the broader guarantee (rejected: unimplemented).
  - Decision-bearing: no.
- Re-verified after these edits:
  - `openspec validate feature-238-684b0ada --strict` passes.
  - A temp-copy run of the full sequence (the seven old changes archived with `--skip-specs`, then this change) applies +44 / ~17 / -3.
  - All 83 main specs pass strict validation.

## tasks

- **Decision:** One task covers the whole change: check off start-run, archive the seven old changes with `--skip-specs`, run the baseline drift check, verify (validation, the temp-copy archive dry run with a diff review, and `make test`), and commit.
  - Alternatives: split into separate tasks (rejected: the step asks for exactly one task, and the work is one atomic file move).
  - Decision-bearing: no.

## acceptance-fix (round 0)

- **Finding 1, `nested_agent_end` missing from the `audit-log-entries` event list: applied as a record fix; the spec content is unchanged.**
  - The approved spec deliberately listed only event types that production code writes.
  - `nested_agent_end` has had no emitter since `4f04880b` (2026-09-08); `internal/metrics/collector.go` only reads it, from older audit logs.
  - The defect was that `reconciliation.md` claimed the list matched `internal/audit/types.go` without noting this omission. The row now records it with evidence.
  - Whether the spec should also name read-only legacy types is left as a decision in the acceptance ledger.
  - Alternatives: add `nested_agent_end` to the list (not chosen silently, because it changes approved spec scope; it is offered as an option in the ledger).
  - Decision-bearing: yes (left to the human).
- **CI `TestExternalUserLoop/pending-reply` failure: not fixed here.**
  - It is a pre-existing flake in an unrelated test: the same test failed on `factory/feature-231-fccced8f` in run 38001979089 (subtest `pending-request`), it passes locally on this SHA, and this branch changes no Go code.
  - A rerun is not permitted by the token. The next push triggers CI again.
  - Recorded in the acceptance ledger.
  - Decision-bearing: no.
