# Spec-driven simple-change validation

Apply this checklist to a small change plan without expanding its meaning, scope, behavior, design decisions, or testing strategy.

1. Verify `<change-dir>/proposal.md` and `<change-dir>/tasks.md` exist and are non-empty, and at least one non-empty specification matches `<change-dir>/specs/*/spec.md`.
2. Check every specification mechanically:
   - each requirement uses a `### Requirement:` heading and normative SHALL or MUST language;
   - every requirement has at least one `#### Scenario:` with WHEN and THEN behavior;
   - delta section headings and requirement names are internally consistent;
   - no placeholder or deferred marker remains unresolved.
3. Verify `<change-dir>/tasks.md` is a focused, actionable implementation plan consistent with the proposal and specifications.
4. If `<change-dir>/design.md` exists, verify it is non-empty and does not contradict the proposal or specifications.

Mechanically fix unambiguous formatting, placement, or heading problems. If a resolution would require inventing behavior or changing approved semantics, leave it unresolved and report the exact semantic blocker.
