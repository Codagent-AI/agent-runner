## ADDED Requirements

### Requirement: Value credit separates detection from correction using step output and contributions

When step output or written-file contributions are available, the value auditor SHALL use them to assign credit. Finding a problem SHALL be credited to the step whose own output or contribution first reported it. Correcting the problem SHALL be credited to the step whose own contribution implemented the correction. The same outcome SHALL NOT be rated `unique` for both steps. Content that the evidence marks as inherited or unresolved SHALL NOT be credited to the step it was presented with. When output and contributions are unavailable, the auditor SHALL reflect that in confidence and evidence coverage instead of inferring credit only from which steps ran next to each other.

#### Scenario: Audit step finds a defect that a fix step corrects
- **WHEN** an audit step's output reports a defect and a later fix step's patch corrects that defect
- **THEN** the audit step is credited with detection, the fix step with correction, and the two are not both rated `unique` for the same outcome

#### Scenario: Inherited ledger content is not credited
- **WHEN** a step's whole-file context for a cumulative ledger includes rulings that earlier steps wrote
- **THEN** the step's judgment credits only its own delta and not the earlier rulings

#### Scenario: Neither output nor contributions are available
- **WHEN** a leaf has no available step output and no available written-file contributions
- **THEN** its observation does not record `complete` evidence coverage, and any credit inferred from neighbouring steps carries reduced confidence
