## ADDED Requirements

### Requirement: Estimated cost in selected detail

When a completed agent or call execution's cost is an estimate, its selected detail SHALL show the amount with the same estimate marker the summary uses and SHALL label it as an API-equivalent estimate rather than reported spend. A CLI-reported cost SHALL keep its current presentation. When a Codex execution's estimate is ineligible, its cost SHALL show `?`, never `$0.00`, with the ineligibility reason when known. Step-list rows that show cost SHALL apply the same marker rules.

#### Scenario: Estimated cost in detail
- **WHEN** the user selects a completed Codex step with an estimated cost of $0.98
- **THEN** the detail shows `~$0.98` labeled as an estimate beside its usage and duration

#### Scenario: Reported cost in detail unchanged
- **WHEN** the user selects a completed Claude step with a reported cost of $1.50
- **THEN** the detail shows `$1.50` without the estimate marker or label

#### Scenario: Ineligible estimate in detail
- **WHEN** the user selects a completed Codex step whose estimate was ineligible with reason `model-unpriced`
- **THEN** the cost shows `?` with that reason, not `$0.00`
