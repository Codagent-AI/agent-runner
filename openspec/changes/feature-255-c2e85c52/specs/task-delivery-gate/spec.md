## MODIFIED Requirements

### Requirement: Reporting external delivery

When `verify-task-commit` accepts external delivery, its output SHALL:
- state that the task was delivered outside the run repository;
- name the resolved external repository path, each verified commit ID, and a remote-tracking ref that contains the commits;
- show agent-reported fields it did not verify, such as the pull request URL and branch name, labeled as reported and unverified;
- state that pushed state was judged from the external clone's local remote-tracking refs, without contacting the remote;
- state that this run's validator did not cover the external work, without claiming that any particular validator review, such as task-compliance, ran.

This output SHALL appear as the step's output in the audit log and run views. The record SHALL remain in the session directory after the run completes.

#### Scenario: Accepted output names the delivery
- **WHEN** `verify-task-commit` accepts a record that lists two commits and a pull request URL
- **THEN** the step output states external delivery, lists the repository path, both commit IDs, and a containing remote-tracking ref, shows the pull request URL labeled as reported and unverified, and states that the validator did not cover the external work

#### Scenario: Output does not claim a task-compliance review
- **WHEN** `verify-task-commit` accepts external delivery in a run where task-compliance was not activated
- **THEN** the step output does not state or imply that a task-compliance review ran

#### Scenario: Output visible in audit
- **WHEN** a run's task is accepted through external delivery
- **THEN** that step's recorded output in the run's audit log contains the external delivery statement

#### Scenario: Record retained after the run
- **WHEN** a run with an externally delivered task completes
- **THEN** that task's external delivery record is still present in the run's session directory
