## ADDED Requirements

### Requirement: Agent-call access to named sessions

An agent call targeting `session: <name>` SHALL use the same declaration visibility, pinned agent profile, run-scoped named-session map, persistence, composition, and drift behavior as a workflow step targeting that name. Agent calls and workflow steps SHALL read and update the same named-session entry. A call-level `model` override SHALL apply to the invocation without changing the agent profile pinned by the declaration. The invocation SHALL use the CLI resolved from the declared profile.

#### Scenario: Call creates named session on first use
- **WHEN** an agent call targets a declared named session with no entry in the run-scoped named-session map
- **THEN** Agent Runner creates the CLI session and stores its ID under the declared name

#### Scenario: Call resumes workflow-created named session
- **WHEN** a workflow step previously created the named session targeted by an agent call
- **THEN** the call resumes the CLI session stored by the workflow step

#### Scenario: Workflow step resumes call-created named session
- **WHEN** an agent call previously created a named session and a later workflow step targets the same name
- **THEN** the workflow step resumes the CLI session stored by the call

#### Scenario: Call resolves declaration through composition
- **WHEN** a call made from a sub-workflow targets a named session declared within its visible workflow composition
- **THEN** Agent Runner resolves the declaration using the same composition rules as a workflow-step reference

#### Scenario: Call-created named session survives workflow resume
- **WHEN** an agent call creates a named session, the runner process exits, and the workflow is resumed
- **THEN** a later call or workflow-step reference resumes the persisted CLI session

#### Scenario: Agent drift behavior applies to call-created session
- **WHEN** a persisted named session created by a call has an agent profile that differs from the current declaration on workflow resume
- **THEN** Agent Runner trusts the persisted session ID and emits the existing agent-drift warning without recreating the session

#### Scenario: Invocation overrides do not change declaration
- **WHEN** an agent call targets a named session and supplies a valid `model` override
- **THEN** Agent Runner applies the override to that invocation while leaving the declaration's pinned agent profile and resolved CLI unchanged
