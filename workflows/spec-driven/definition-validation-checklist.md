# Spec-driven definition validation

Apply this checklist to the approved proposal, specifications, design, and test plan without changing their meaning, scope, behavior, design decisions, or testing strategy.

1. Verify `<change-dir>/proposal.md`, `<change-dir>/design.md`, and `<change-dir>/test-plan.md` exist and are non-empty, and at least one non-empty specification matches `<change-dir>/specs/*/spec.md`.
2. Check every specification mechanically:
   - each requirement uses a `### Requirement:` heading and normative SHALL or MUST language;
   - every requirement has at least one `#### Scenario:` with WHEN and THEN behavior;
   - delta section headings and requirement names are internally consistent;
   - no placeholder or deferred marker remains unresolved.
3. Verify `<change-dir>/test-plan.md` is non-empty and structurally usable:
   - it includes coverage strategy, integration tests, end-to-end tests, an acceptance testing envelope, human-only testing, and a coverage map;
   - `INT-*`, `E2E-*`, and `HT-*` identifiers are unique within their categories;
   - every identifier referenced by the coverage map exists in its corresponding section;
   - the acceptance testing envelope records available environments, credentials, authorized effects and cleanup, what is off limits, and permitted substitutes;
   - the envelope does not enumerate acceptance test cases, which the exploratory acceptance pass derives from the change itself;
   - human-only testing either says `None.` or defines each `HT-*` with why an agent cannot perform it.

Mechanically fix unambiguous formatting, placement, or heading problems. If a resolution would require inventing behavior or changing approved semantics, leave it unresolved and report the exact semantic blocker.
