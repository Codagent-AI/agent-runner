## MODIFIED Requirements

### Requirement: Unavailable cost display in summary

Steps without a recorded cost, either CLI-reported or estimated, SHALL display `?` in the cost column, never `$0.00`. A container row whose descendants are only partially priced SHALL mark its rolled-up cost as partial.

#### Scenario: Unpriced step shows marker
- **WHEN** a step with no CLI-reported cost and no eligible estimate appears in the summary
- **THEN** its cost column shows `?` rather than a zero amount

#### Scenario: Ineligible Codex step shows marker
- **WHEN** a Codex step whose estimate was ineligible appears in the summary
- **THEN** its cost column shows `?` rather than a zero amount

#### Scenario: Partially priced container flagged
- **WHEN** a sub-workflow contains one step with reported cost and one without
- **THEN** the sub-workflow row shows the sum of the priced steps with a partial indicator

## ADDED Requirements

### Requirement: Estimated cost marker in summary

A cost cell whose value is, or includes, an estimate SHALL be marked as an estimate (for example `~$0.98`), so it is never read as reported spend. A step row with an estimated cost, a container row whose rolled-up cost includes any estimated descendant, and a Total row whose scope includes any estimate SHALL carry the marker. Cells whose costs are all CLI-reported SHALL render without it. The estimate marker and the partial indicator SHALL be independent and MAY appear together. The legend SHALL explain the estimate marker.

#### Scenario: Estimated step row
- **WHEN** a Codex step with an estimated cost of $0.98 appears in the summary
- **THEN** its cost column shows the amount with the estimate marker, such as `~$0.98`

#### Scenario: Reported step row
- **WHEN** a Claude step with a reported cost of $1.50 appears in the summary
- **THEN** its cost column shows `$1.50` without the estimate marker

#### Scenario: Container mixing reported and estimated costs
- **WHEN** a loop contains a Claude step with a reported $1.50 and a Codex step with an estimated $0.98
- **THEN** the loop row shows $2.48 with the estimate marker

#### Scenario: Estimated and partial together
- **WHEN** a sub-workflow contains an estimated Codex step and an unpriced step
- **THEN** its row shows the estimated sum with both the estimate marker and the partial indicator

#### Scenario: Total row includes estimates
- **WHEN** the root scope contains any estimated cost
- **THEN** the Total row's cost carries the estimate marker
