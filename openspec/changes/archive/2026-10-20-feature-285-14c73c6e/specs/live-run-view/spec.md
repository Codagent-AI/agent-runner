## ADDED Requirements

### Requirement: Estimated costs during an active run

While a workflow runs, a completed Codex execution's estimated cost SHALL be available in its selected detail as soon as the execution completes, with the same estimate marker and label as the inspect view. Run-so-far totals in the summary SHALL include estimates and carry the estimate marker whenever they include one.

#### Scenario: Codex estimate shown mid-run
- **WHEN** a Codex step with an eligible estimate completes while later steps are still running
- **THEN** selecting it shows its estimated cost with the estimate marker without waiting for run completion

#### Scenario: Run-so-far total includes an estimate
- **WHEN** the run-so-far summary includes a completed Codex step's estimated cost
- **THEN** the total includes that amount and carries the estimate marker
