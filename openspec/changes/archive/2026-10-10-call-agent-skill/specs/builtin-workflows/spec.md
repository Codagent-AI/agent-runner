## ADDED Requirements

### Requirement: Call-capable OpenSpec v2 steps declare and use call_agent

The shipped OpenSpec v2 call-capable steps SHALL explicitly declare and use `call_agent`.
This applies to proposal review, approach review, task review, acceptance preparation, and targeted
re-acceptance. Each step SHALL declare `tools: [call_agent]` and direct the lead to use
`codagent:call-agent` for generic child prompting, invocation, failure, verification, and reporting
behavior. The workflow prompt SHALL retain its task-specific child skill, artifact and evidence paths,
review or test scope, read/write permissions, user approval gate, correction behavior, and maximum
number of calls. Declaring the tool MUST NOT force a conditional call to run when its workflow
condition is not met.

#### Scenario: Definition reviews declare the tool
- **WHEN** the built-in `define-change-v1.0.yaml` workflow loads
- **THEN** its `proposal` and `approach-review` steps declare `tools: [call_agent]` and invoke
  `codagent:call-agent` while retaining their separate review scopes and user approval gates

#### Scenario: Task review declares the tool
- **WHEN** `plan-change-v2.0.yaml` loads its pinned `review-tasks-v1.0.yaml` sub-workflow
- **THEN** that sub-workflow's task-review step declares `tools: [call_agent]`, invokes
  `codagent:call-agent`, and retains its immutable-definition boundary, correction rules, and
  two-call budget

#### Scenario: Acceptance preparation declares the tool
- **WHEN** the built-in `implement-change-v2.0.yaml` workflow loads
- **THEN** its `prepare-acceptance` step declares `tools: [call_agent]`, invokes
  `codagent:call-agent` through the run-scoped `acceptance-tester` session and retains its full,
  targeted, and evidence-only scopes, fix loop, durable evidence requirements, and three-call budget

#### Scenario: Targeted re-acceptance declares the tool
- **WHEN** the built-in `accept-change-v1.0.yaml` workflow loads
- **THEN** its `run-reacceptance-testing` step declares `tools: [call_agent]`, invokes
  `codagent:call-agent` through the same run-scoped `acceptance-tester` session and retains the
  user's opt-in, targeted scope, fix loop, status artifact, and three-call budget

#### Scenario: Post-fix acceptance resumes the original tester
- **WHEN** acceptance testing reports a defect and the lead fixes it
- **THEN** the next tester call resumes `acceptance-tester` with instructions to verify the prior
  finding and affected dependencies without starting a new full pass

#### Scenario: Declared conditional call remains optional
- **WHEN** targeted re-acceptance was not recommended or the user declined it
- **THEN** `run-reacceptance-testing` completes without invoking a child even though the tool was
  provisioned
