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

### Requirement: Audit issue bodies are durable and repairable

GitHub issue publication SHALL pass the redacted body through the GitHub CLI's
explicit stdin body-file option. A development-only repair operation SHALL
restore the intended redacted body and markers only when the locally recorded
issue is an auto-audit issue whose remote body is exactly the historical `-`
placeholder.

#### Scenario: Historical placeholder issue is repaired
- **WHEN** a locally recorded created finding points to an `[auto-audit]` GitHub issue with body `-`
- **THEN** the repair operation replaces that body with the redacted finding body and durable markers

#### Scenario: Edited issue is protected
- **WHEN** the linked issue has a body other than `-` or is not an auto-audit issue
- **THEN** the repair operation leaves it unchanged
