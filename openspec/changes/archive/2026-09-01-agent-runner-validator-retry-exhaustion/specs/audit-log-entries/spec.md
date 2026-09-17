## ADDED Requirements

### Requirement: Durable warning status evidence

Audit evidence SHALL preserve warning status separately from raw execution outcome. A terminal event for an originating warning step SHALL identify status `warning` while retaining the original `failed` or `exhausted` outcome and the step's normal diagnostic fields. Warning evidence SHALL retain the complete audit prefix needed to resolve the exact originating step. A successful run end SHALL record whether the run completed with warnings and the count of originating warnings.

#### Scenario: Failed warning step preserves outcome

- **WHEN** a failed step terminates as a non-blocking warning
- **THEN** its terminal audit evidence records warning status and the underlying failed outcome without discarding its error or output fields

#### Scenario: Exhausted warning loop preserves outcome

- **WHEN** an exhausted loop terminates as a non-blocking warning
- **THEN** its terminal audit evidence records warning status, exhausted outcome, iteration totals, break state, and its exact prefix

#### Scenario: Completed run records warning count

- **WHEN** a run reaches successful completion with two originating warning steps
- **THEN** its run-end evidence identifies successful completion with warnings and records warning count 2

#### Scenario: Clean run records no warnings

- **WHEN** all earlier failures are recovered and the run completes with no terminal warning origins
- **THEN** its run-end evidence identifies ordinary successful completion with warning count zero or no warning qualifier

#### Scenario: Historical reconstruction does not require current workflow files

- **WHEN** a saved run completed with warnings and its original workflow definition is no longer available
- **THEN** the audit evidence is sufficient to reconstruct the completion status and exact warning origins
