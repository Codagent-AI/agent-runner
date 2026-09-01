## MODIFIED Requirements

### Requirement: Detailed run view remains the post-completion default

When a live workflow run reaches a terminal state, the run-view TUI SHALL display the detailed run view rather than automatically opening the metrics summary. On ordinary successful completion, the detailed view SHALL focus the workflow's final top-level step and display `completed`. On successful completion with warning origins, it SHALL also focus the final top-level step, display `complete with warnings (N)`, and make `w warnings` available without automatically selecting a warning. On failure, it SHALL focus the failed step. The metrics summary SHALL remain available on demand.

The same ordinary or warning-qualified completion status SHALL remain visible when the saved run appears in the run list and when it is later opened from the list or through `--inspect`.

#### Scenario: Successful completion keeps the detailed view

- **WHEN** the last step of a live run completes successfully with no warning origins
- **THEN** the TUI displays the detailed run view focused on the final top-level step with status `completed`; the metrics summary is not auto-shown

#### Scenario: Warning completion keeps the detailed view

- **WHEN** the last step of a live run completes and one unresolved warning origin remains
- **THEN** the TUI displays the detailed run view focused on the final top-level step with status `complete with warnings (1)` and `w warnings` available; the metrics summary is not auto-shown

#### Scenario: Warning completion remains distinct in saved-run entry points

- **WHEN** a run completed with warnings and is displayed in the run list or opened later for inspection
- **THEN** its status remains `complete with warnings (N)` rather than degrading to ordinary completion

#### Scenario: Failure keeps the detailed view

- **WHEN** a step fails and the workflow halts
- **THEN** the TUI shows the detailed run view with the cursor on the failed step; the summary is not auto-shown
