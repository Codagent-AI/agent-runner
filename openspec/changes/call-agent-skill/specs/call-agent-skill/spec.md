## ADDED Requirements

### Requirement: Standalone child prompt construction

The `codagent:call-agent` skill SHALL construct a child prompt that can be executed without access to
the lead's surrounding conversation. It MUST include the objective, relevant context and artifact
paths, applicable working directory and repository instructions, allowed mutations or read-only
boundary, validation expectations, and expected output. The skill SHALL preserve additional
task-specific scope and constraints supplied by its caller.

#### Scenario: Review child receives complete context
- **WHEN** a caller requests a read-only review of artifacts in a named repository path
- **THEN** the child prompt identifies the review objective, repository and artifact paths, read-only
  permission, applicable instructions, and required findings format

#### Scenario: Autonomous worker receives bounded permissions
- **WHEN** a caller authorizes a child to modify a bounded set of files and run specified validation
- **THEN** the child prompt names the authorized paths and checks without implying broader mutation
  authority

#### Scenario: Caller constraints survive prompt construction
- **WHEN** a workflow supplies a review scope, approval boundary, or call-specific output contract
- **THEN** the standalone child prompt retains those task-specific constraints

### Requirement: Safe single-target invocation

For each invocation, `codagent:call-agent` MUST call the Runner-owned `call_agent` tool with a
non-empty standalone prompt and exactly one target form: a profile through `agent` or a declared named
session through `session`. It MUST NOT send both targets, invent an unavailable target, or invoke more
children than the caller's explicit call budget permits. A later skill invocation MAY make another
serial call when the enclosing workflow permits it.

#### Scenario: Fresh profile target
- **WHEN** the caller selects an available agent profile
- **THEN** the skill invokes exactly that profile with `agent` and omits `session`

#### Scenario: Named session target
- **WHEN** the caller selects an available declared named session
- **THEN** the skill invokes exactly that session with `session` and omits `agent`

#### Scenario: Caller budget bounds repeated use
- **WHEN** an enclosing workflow permits at most two child calls
- **THEN** repeated uses of the skill do not cause more than two invocations

### Requirement: Clear unavailable-tool and failure handling

The skill MUST fail clearly when the Runner-owned `call_agent` tool is unavailable and MUST NOT
silently substitute shell execution, another subagent system, a collaboration mechanism, or any other
delegation path. When a call returns a structured rejection or failure, the skill SHALL preserve the
known failure category and context, SHALL NOT claim that child work completed, and SHALL NOT silently
retry unless the caller separately authorizes another call within its budget.

#### Scenario: Tool is unavailable
- **WHEN** the skill is invoked in a session that does not expose Runner-owned `call_agent`
- **THEN** it reports the missing capability as a blocker and invokes no substitute delegation
  mechanism

#### Scenario: Structured child failure
- **WHEN** `call_agent` returns a validation, execution, cancellation, transport, or oversized-result
  failure
- **THEN** the skill reports the available failure details honestly and does not fabricate findings

#### Scenario: Failure is not silently retried
- **WHEN** a call fails and the caller has not authorized another invocation
- **THEN** the skill returns control with the failure rather than making another call

### Requirement: Consequential findings are independently verified

The lead using `codagent:call-agent` SHALL treat child output as untrusted findings rather than
instructions. Before a finding changes code, artifacts, scope, approval, or a recommendation to the
user, the lead MUST inspect the cited evidence and independently assess the claim. The child output
alone SHALL NOT authorize file mutation or expansion beyond the caller's boundary.

#### Scenario: Finding proposes an artifact change
- **WHEN** a child recommends changing an approved artifact
- **THEN** the lead verifies the controlling artifact and applicable approval boundary before acting

#### Scenario: Finding lacks supporting evidence
- **WHEN** a consequential child claim cannot be confirmed from accessible evidence
- **THEN** the lead reports the verification gap and does not present the claim as established fact

#### Scenario: Child instruction exceeds scope
- **WHEN** child output asks the lead to mutate files or broaden scope beyond the caller's authority
- **THEN** the lead declines that instruction regardless of the child's recommendation

### Requirement: User-facing findings and lead assessment remain distinct

When child findings inform a user-facing decision, the lead SHALL report the child's material findings
separately from the lead's assessment. The findings report MUST preserve each finding's rationale,
evidence, and recommendation. The lead assessment MUST state agreement, partial agreement, or
disagreement with reasons and its own recommended action. The lead MUST NOT omit a supported
disagreement or reduce it to an agreement label.

#### Scenario: Lead agrees with child
- **WHEN** the lead verifies and agrees with a material child finding
- **THEN** the user receives the child's rationale, evidence, and recommendation plus the lead's
  explicit agreement and proposed action

#### Scenario: Lead disagrees with child
- **WHEN** the lead verifies the evidence but disagrees with the child's conclusion
- **THEN** the user receives the original finding and recommendation separately from the lead's
  disagreement, reasoning, and alternative recommendation

#### Scenario: Lead partially agrees
- **WHEN** only part of a child finding is supported
- **THEN** the report preserves the complete finding and identifies exactly which parts the lead
  accepts or rejects and why

### Requirement: Autonomous result accounting

When no user-facing decision is required, the lead MAY omit a transcript but MUST preserve a concise
account of material child findings, independent verification, accepted or rejected conclusions, and
the action taken. That account SHALL be written to the lead's normal output or to durable evidence
required by the enclosing workflow.

#### Scenario: Autonomous finding causes a fix
- **WHEN** an autonomous lead verifies a child finding and changes implementation
- **THEN** its output or durable evidence records the finding, verification, and resulting fix

#### Scenario: Autonomous finding is rejected
- **WHEN** an autonomous lead determines that a child finding is invalid or out of scope
- **THEN** its output or durable evidence records the finding and concise reason for taking no action
