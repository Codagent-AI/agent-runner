# Spec-driven definition validation

Apply this checklist to the approved proposal, specifications, design, and test plan without changing their meaning, scope, behavior, design decisions, or testing strategy.

1. Verify `<change-dir>/proposal.md`, `<change-dir>/design.md`, and `<change-dir>/test-plan.md` exist and are non-empty, and at least one non-empty specification matches `<change-dir>/specs/*/spec.md`.
2. Check every specification mechanically:
   - each requirement uses a `### Requirement:` heading and normative SHALL or MUST language;
   - every requirement has at least one `#### Scenario:` with WHEN and THEN behavior;
   - delta section headings and requirement names are internally consistent;
   - no placeholder or deferred marker remains unresolved.
3. Verify `<change-dir>/test-plan.md` is non-empty and structurally usable:
   - it includes coverage strategy, integration tests, end-to-end tests, agent acceptance tests, human-only testing, and a coverage map;
   - `INT-*`, `E2E-*`, `AT-*`, and `HT-*` identifiers are unique within their categories;
   - every identifier referenced by the coverage map exists in its corresponding section;
   - each `AT-*` states whether it is required or conditional, gives an activation condition when conditional, and records its public surface, expected result, evidence, effects and cleanup, and permitted substitutes;
   - human-only testing either says `None.` or defines each `HT-*` with why an agent cannot perform it.

Mechanically fix unambiguous formatting, placement, or heading problems. If a resolution would require inventing behavior or changing approved semantics, leave it unresolved and report the exact semantic blocker.
