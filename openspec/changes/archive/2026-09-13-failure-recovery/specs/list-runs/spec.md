# Capability: list-runs

## Purpose
Define run-list navigation, run actions, filtering, and transitions into run inspection.

## ADDED Requirements

### Requirement: Failed run rows show the failure reason

A failed run row in the list SHALL display the run's classified failure reason after its status, truncated with an ellipsis to the available column width. Runs with no failure record SHALL show the existing failure reason string.

#### Scenario: Blocked failure in list
- **WHEN** a run failed at `verify-draft-pr` with a blocked declaration
- **THEN** its row shows `failed` followed by `verify-draft-pr failed: … blocked: push rejected: token lacks workflow scope`, truncated to fit

#### Scenario: Reason truncates without breaking columns
- **WHEN** the failure reason is longer than the available width
- **THEN** it is cut with an ellipsis and the other columns keep their positions
