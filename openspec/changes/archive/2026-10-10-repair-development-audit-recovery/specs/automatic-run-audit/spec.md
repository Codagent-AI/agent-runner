## ADDED Requirements

### Requirement: Reserved launches can be explicitly reconciled

A development-audit build SHALL provide `audit reconcile <source-run> --session <execution-session-id>` for an existing automatic audit reservation. It SHALL preserve the original audit identity, refuse an active source run, and never rerun the source workflow. Launching, started, and completed links SHALL be safe no-ops; failed or inconsistent reservations SHALL be rejected.

#### Scenario: Interrupted reservation is reconciled
- **WHEN** an automatic audit link is durably `reserved` after the source run is no longer active
- **THEN** explicit reconciliation launches that original audit identity
