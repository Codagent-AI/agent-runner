## MODIFIED Requirements

### Requirement: Skip rule for fresh runs (builtins only)

For fresh runs only, pre-validation SHALL be skipped when the resolved latest workflow path is an embedded builtin workflow (any path matched by `builtinworkflows.IsRef`), with one exception: every reachable sub-workflow target whose `workflow:` field depends on a parameter value the caller supplied SHALL still be resolved and validated before any step runs, together with that target's own composition. The build-time check of the builtins cannot cover values that only the caller supplies.

Every other resolved workflow path — including versioned workflows under `<cwd>/.agent-runner/workflows/`, versioned workflows under `~/.agent-runner/workflows/`, and any other resolution outcome — SHALL pre-validate on every fresh run.

The skip rule rests on the assumption that builtins are validated at the agent-runner repo's build time by a "validate all builtins" mechanism scoped to that project — preferably a Go test that iterates every embedded workflow version and runs the pre-validation pipeline on each, alternatively a hidden / dev-only CLI flag. The mechanism does not need to ship in the released binary. Downstream projects MAY configure an analogous author-time check on their own `.agent-runner/workflows/` (e.g., wiring `agent-runner --validate <versioned-relpath>` into CI), but the runner does NOT skip pre-validation on the assumption that they did. `--validate` does not honor the skip rule.

#### Scenario: Builtin invocation skips pre-validation
- **WHEN** `agent-runner core:finalize-pr` is invoked
- **THEN** pre-validation is skipped and the runner proceeds directly to `runner.PrepareRun` with the resolved latest embedded version

#### Scenario: Project workflow invocation pre-validates
- **WHEN** the cwd is a project root containing `.agent-runner/workflows/deploy-v1.0.yaml` and `agent-runner deploy` is invoked
- **THEN** pre-validation runs the full pipeline before the run starts

#### Scenario: Global user workflow invocation pre-validates
- **WHEN** the cwd contains no `.agent-runner/workflows/scratch-v1.0.yaml` but `~/.agent-runner/workflows/scratch-v1.0.yaml` exists, and `agent-runner scratch` is invoked
- **THEN** pre-validation runs the full pipeline before the run starts

#### Scenario: Caller-supplied sub-workflow target is checked for a builtin run
- **WHEN** `agent-runner openspec:change change_name=foo validation_workflow=/tmp/missing-v1.0.yaml` is invoked
- **THEN** the run fails before any step executes, with an error naming the missing file and the referencing step

#### Scenario: Builtin defaults are not re-validated
- **WHEN** `agent-runner openspec:change change_name=foo` is invoked without `validation_workflow`
- **THEN** pre-validation is skipped as for any builtin run
