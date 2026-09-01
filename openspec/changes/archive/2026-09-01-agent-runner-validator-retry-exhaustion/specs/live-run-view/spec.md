## MODIFIED Requirements

### Requirement: TUI stays open after workflow completion

When the workflow reaches a terminal state, the run-view TUI SHALL remain active until explicit user exit.

On ordinary successful completion, the TUI SHALL keep the detailed run view and focus the final top-level step; the summary is available through `s`. On successful completion with warning origins, it SHALL retain the same landing behavior and show breadcrumb status `complete with warnings (N)`, with `w warnings` available to inspect the origins. It SHALL NOT automatically move selection to a warning. On failure, the TUI SHALL remain in detail, expand the failed leaf's ancestry in the root tree, and select the failed leaf without creating a manual drill scope. If multiple failed leaves share the greatest depth, the TUI SHALL select the leaf whose failure was recorded most recently, falling back to workflow order when durable failure ordering is unavailable. The summary SHALL remain available through `s`.

Once execution is terminal, detailed behavior SHALL match an inactive historical run opened through `--inspect`, including manual tree navigation, warning navigation, optional drill scope, selected-detail scrolling, resume, and the legend.

#### Scenario: Successful completion keeps detailed view

- **WHEN** the last step in the workflow completes successfully and the run has no warning origins
- **THEN** the TUI remains open displaying the detailed run view focused on the final top-level step, with the breadcrumb status showing `completed`; the summary is not auto-shown

#### Scenario: Completion with warnings keeps final-step landing

- **WHEN** the workflow completes successfully with two warning origins
- **THEN** the TUI remains in detail focused on the final top-level step, displays `complete with warnings (2)`, advertises `w warnings`, and does not auto-select a warning

#### Scenario: Failure keeps TUI open in detailed view

- **WHEN** an execution fails and the workflow halts
- **THEN** the TUI remains in detail, expands the failed ancestry, selects the failed leaf, and retains root manual scope

#### Scenario: Equally deep failures select the latest failure

- **WHEN** terminal failure contains multiple failed leaves at the greatest depth
- **THEN** the TUI selects the most recently recorded failure, or the first in workflow order when durable failure ordering is unavailable

#### Scenario: Post-completion navigation matches inspect mode

- **WHEN** the workflow is terminal and the user opens or remains in detail
- **THEN** navigation, warning navigation, and selected-detail behavior match an inactive `view-run`

#### Scenario: Resume action available after completion

- **WHEN** a terminal run has a selected resumable agent execution and the user invokes resume
- **THEN** the existing `view-run` resume behavior applies
