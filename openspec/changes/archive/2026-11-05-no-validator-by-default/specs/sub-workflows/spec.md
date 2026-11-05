## MODIFIED Requirements

### Requirement: Sub-workflow invocation

A step with a `workflow` field SHALL load and execute the exact referenced workflow file. The resolved reference MUST use a valid versioned workflow filename and MUST NOT be replaced by a newer available version. The step MUST NOT have `prompt`, `command`, or `mode` — it delegates entirely to the sub-workflow. The sub-workflow executes in the same process as the parent. A target that is an absolute filesystem path SHALL load that file from disk, whether the parent is a built-in or a file-based workflow, both during pre-validation and at execution; it is not resolved inside the embedded built-in catalog.

#### Scenario: Sub-workflow executes successfully
- **WHEN** a step has `workflow: workflows/run-validator-v1.0.yaml` and the referenced file exists
- **THEN** Agent Runner loads that exact sub-workflow version, executes its steps, and continues with the next step in the parent

#### Scenario: Sub-workflow file not found
- **WHEN** a step has `workflow: workflows/missing-v1.0.yaml` and the file does not exist
- **THEN** Agent Runner fails with a descriptive error naming the missing versioned file

#### Scenario: Unversioned sub-workflow rejected
- **WHEN** a step has `workflow: workflows/run-validator.yaml`
- **THEN** Agent Runner fails with the actionable versioned-filename error

#### Scenario: Sub-workflow step is mutually exclusive with prompt/command/mode
- **WHEN** a step has both `workflow` and `prompt` (or `command` or `mode`)
- **THEN** Agent Runner fails at load time with a validation error

#### Scenario: Absolute target beneath a built-in parent
- **WHEN** a built-in workflow step's target resolves to `/home/user/project/.agent-runner/workflows/my-validation-v1.0.yaml` and that file exists
- **THEN** Agent Runner loads that file from disk and executes it

#### Scenario: Missing absolute target beneath a built-in parent
- **WHEN** a built-in workflow step's target resolves to an absolute path whose file does not exist
- **THEN** Agent Runner fails with a descriptive error naming that path, not an embedded-catalog lookup error
