## ADDED Requirements

### Requirement: Pending delivery failures remain independently durable

The pending local report SHALL retain a non-secret delivery error independently of audit-stage warnings. Successful retry SHALL clear only the reporting failure while retaining the original observations and audit-stage warning.

#### Scenario: Completion follows reporting failure
- **WHEN** external delivery fails before the audit workflow completes
- **THEN** the local report remains pending with its delivery error after completion

#### Scenario: Retry succeeds
- **WHEN** a pending report is later delivered successfully
- **THEN** its reporting error is cleared without rerunning audit models or changing observation identity

### Requirement: Unsafe optional notes are omitted

When a proposed optional note contains prohibited detail, validation SHALL omit that note while retaining the validated categorical observation and a reason-only diagnostic.

#### Scenario: Note contains prohibited detail
- **WHEN** a proposed note contains a local path, URL, secret-like value, or evidence excerpt
- **THEN** the observation remains valid and its optional note is omitted before any row is written
