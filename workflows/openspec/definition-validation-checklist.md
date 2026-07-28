# OpenSpec definition validation

Apply this checklist to the approved proposal, specifications, design, and test plan without changing their meaning, scope, behavior, design decisions, or testing strategy.

1. Run `openspec validate --type change "<change-name>"` and correct mechanical validation errors until it passes.
2. Run `openspec show "<change-name>" --json` and reconcile parsed requirements with the source delta specifications:
   - parsed and source requirement counts match;
   - a non-empty delta parses at least one requirement;
   - source headings map in order to the corresponding parsed normative text;
   - each parsed requirement contains a parsed scenario.
3. Dry-run the delta apply for each file under `openspec/changes/<change-name>/specs/<capability>/spec.md`:
   - `MODIFIED` and `REMOVED` requirement headers exist verbatim in the current main specification, ignoring whitespace;
   - `RENAMED` `FROM` headers exist in the current main specification;
   - when no current main specification exists, only `ADDED` requirements are valid.
4. Verify `openspec/changes/<change-name>/test-plan.md` is non-empty and structurally usable:
   - it includes coverage strategy, integration tests, end-to-end tests, agent acceptance tests, human-only testing, and a coverage map;
   - `INT-*`, `E2E-*`, `AT-*`, and `HT-*` identifiers are unique within their categories;
   - every identifier referenced by the coverage map exists in its corresponding section;
   - each `AT-*` states whether it is required or conditional, gives an activation condition when conditional, and records its public surface, expected result, evidence, effects and cleanup, and permitted substitutes;
   - human-only testing either says `None.` or defines each `HT-*` with why an agent cannot perform it.

Mechanically fix unambiguous formatting, placement, or header mismatches. If a resolution would require inventing behavior or changing approved semantics, leave it unresolved and report the exact semantic blocker.
