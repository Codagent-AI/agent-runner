## ADDED Requirements

### Requirement: Call-capable builtin steps declare and use call_agent

Each shipped builtin step that delegates to a child agent through Agent Runner SHALL explicitly declare
`tools: [call_agent]` and direct the lead to use `codagent:call-agent` for generic child prompting,
invocation, failure, verification, and reporting behavior. This SHALL apply to the `proposal` and
`approach-review` steps of `core:define-change`, the `review-tasks` step of `core:review-tasks`, the
`run-reacceptance-testing` and `recover-reacceptance-testing` steps of `core:accept-change`, and the
`review-plan` steps of `openspec:simple-change` and `spec-driven:simple-change`. The `review` step of
`core:complete-simple-change` SHALL also declare `tools: [call_agent]` so the lead can resume the
run-scoped `flow-tester` session for targeted verification. Each workflow prompt SHALL retain its
task-specific child skill, artifact and evidence paths, review or test scope, read/write permissions,
user approval gate, correction behavior, and maximum number of calls. Declaring the tool MUST NOT force
a conditional call to run when its workflow condition is not met.

#### Scenario: Definition reviews declare the tool
- **WHEN** the built-in `core:define-change` workflow loads
- **THEN** its `proposal` and `approach-review` steps declare `tools: [call_agent]` and invoke
  `codagent:call-agent` with a fresh `agent: crosscheck` while retaining their separate review scopes
  and user approval gates

#### Scenario: Task review declares the tool
- **WHEN** `core:plan-change` loads its pinned `core:review-tasks` sub-workflow
- **THEN** that sub-workflow's `review-tasks` step declares `tools: [call_agent]`, invokes
  `codagent:call-agent` with a fresh `agent: crosscheck`, and retains its immutable-definition
  boundary, correction rules, and two-call budget

#### Scenario: Targeted re-acceptance declares the tool
- **WHEN** the built-in `core:accept-change` workflow loads
- **THEN** its `run-reacceptance-testing` step declares `tools: [call_agent]`, invokes
  `codagent:call-agent` through the run-scoped `acceptance-tester` session, and retains the user's
  opt-in, targeted scope, fix loop, status artifact, and three-call budget

#### Scenario: Post-fix acceptance resumes the original tester
- **WHEN** re-acceptance testing reports a defect and the lead fixes it
- **THEN** the next tester call resumes `acceptance-tester` with instructions to reproduce the prior
  finding, verify the fix, and explore what the diff since its last pass moved, without re-running
  its previous pass

#### Scenario: Simple-change plan review declares the tool
- **WHEN** the built-in `openspec:simple-change` or `spec-driven:simple-change` workflow loads
- **THEN** its `review-plan` step declares `tools: [call_agent]` and invokes `codagent:call-agent`
  with a fresh `agent: crosscheck` for one read-only artifact review behind a user confirmation gate

#### Scenario: Declared conditional call remains optional
- **WHEN** targeted re-acceptance was not recommended or the user declined it
- **THEN** `run-reacceptance-testing` completes without invoking a child even though the tool was
  provisioned
