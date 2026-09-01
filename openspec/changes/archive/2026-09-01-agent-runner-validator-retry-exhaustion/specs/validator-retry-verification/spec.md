## ADDED Requirements

### Requirement: Every validator repair is verified

The built-in validator workflow SHALL permit at most three repair invocations. Every successful repair invocation, including the third, MUST be followed by another validator invocation before the validator workflow chooses its terminal status. A validator pass SHALL complete the validator workflow without warnings and SHALL prevent any further repair invocation.

#### Scenario: Initial validation passes

- **WHEN** the first validator invocation passes
- **THEN** the validator workflow completes without invoking repair and without producing a warning

#### Scenario: Earlier repair is verified successfully

- **WHEN** validation fails, a repair succeeds, and the following validator invocation passes before the repair budget is exhausted
- **THEN** the validator workflow completes without warnings and does not invoke another repair

#### Scenario: Third repair receives follow-up validation

- **WHEN** validation fails on the third repair opportunity and the third repair invocation succeeds
- **THEN** Agent Runner invokes the validator once more before deciding the validator workflow's terminal status

### Requirement: Unresolved final verification is non-blocking and visible

The validator invocation after the third repair SHALL be verification-only. If it fails, Agent Runner SHALL NOT invoke a fourth repair. The validator retry step SHALL terminate with warning status, preserve the final validator failure and output as its underlying evidence, and allow the parent workflow to continue. If the final verification passes, neither the validator step nor the completed run SHALL retain a warning from earlier recovered attempts.

#### Scenario: Final verification passes

- **WHEN** the third repair succeeds and its required follow-up validation passes
- **THEN** the validator workflow completes normally, the parent continues, and the run is not marked as having warnings from the recovered validator failures

#### Scenario: Final verification remains failing

- **WHEN** the third repair succeeds and its required follow-up validation fails
- **THEN** no fourth repair is invoked
- **AND** the validator retry step terminates with warning status while retaining the final validator failure and output
- **AND** the parent workflow continues to its next step

#### Scenario: Warning does not invoke downstream remediation

- **WHEN** final validator verification produces a warning
- **THEN** Agent Runner does not automatically invoke a lead, assumptions-review step, repair agent, or acknowledgment gate because of that warning
