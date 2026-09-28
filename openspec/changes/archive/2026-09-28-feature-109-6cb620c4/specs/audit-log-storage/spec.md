## MODIFIED Requirements

### Requirement: Log persistence

Agent Runner SHALL NOT rotate, truncate, or delete a run's audit log independently of its run. A run's audit log SHALL be deleted only when its entire run directory is removed by run retention (see the `run-retention` capability). When run retention is disabled, audit logs SHALL never be automatically deleted.

#### Scenario: Logs accumulate
- **WHEN** a workflow is run 100 times within the retention limits
- **THEN** 100 log files exist, one in each run's session directory

#### Scenario: Log removed with its run
- **WHEN** run retention removes a run directory
- **THEN** that run's `audit.log` is removed with it and no other run's audit log is affected

#### Scenario: Retention disabled keeps every log
- **WHEN** `run_retention.enabled` is `false` in `~/.agent-runner/settings.yaml` and a workflow is run 100 times over several months
- **THEN** 100 log files exist and none is automatically deleted
