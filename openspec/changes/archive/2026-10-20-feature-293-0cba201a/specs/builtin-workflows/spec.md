## MODIFIED Requirements

### Requirement: Verify-change workflow

The hidden `core:verify-change` workflow SHALL hold the verification tail of a structured change: assumption review, simplification, final validation, a draft pull request, bounded acceptance-test rounds, and the acceptance handoff. It SHALL require `change_name`, `change_dir`, `change_label`, and `artifact_validation_instruction`, and SHALL accept `skip_validator` (default `false`), `acceptance_rounds` (default `3`), `skip_implementation_review` (default `false`), `stop_unchanged_acceptance` (default `false`), and `acceptance_fix_scope` (default: the scope instruction below). It SHALL validate `change_name`, `skip_validator`, `stop_unchanged_acceptance`, `skip_implementation_review`, and `acceptance_fix_scope` itself so it is valid standalone, and SHALL declare the `lead-agent` (`lead`) and `acceptance-tester` (`tester`) sessions identically to `core:implement-change`, which SHALL compose it after its task-index verification so both share those named sessions. The simplification step SHALL run in a new session of the `implementor` agent profile rather than the `lead-agent` session, SHALL review only the change's own diff, and SHALL NOT run Agent Validator or push. It SHALL commit behavior-preserving cleanups and any defect fixes separately with the `[simplify]` prefix, and SHALL record unfixed defects and findings needing a product, scope, or design decision in `acceptance-assumptions.md`, replacing the `No unresolved assumptions or context gaps.` statement when it adds the first entry. When `skip_implementation_review` is `true`, the assumption-review and simplification steps SHALL NOT run, and the check that `acceptance-assumptions.md` exists and is non-empty SHALL still run; the value SHALL be accepted only as `true` or `false`.

`acceptance_fix_scope` SHALL set which findings the lead resolves in each acceptance round's fix step. The fix prompt SHALL state the value where it states that scope, in place of any built-in scope wording. The default SHALL be exactly `Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems.`, so a caller that omits the param gets the same fix prompt as before the param existed. A caller-supplied value SHALL change only the scope sentence. The rest of the fix prompt SHALL stay the same: the triage into a fix, an escalation, or an assumptions-ledger entry; the rule that a reproduced violation of the approved specs is not dismissed as "not a defect"; preserving approved behavior, scope, and design; regression coverage, test limits, evidence, and commits; the retest reason; and recording human-only decisions in the ledger. No other step's prompt SHALL change with the value. A value that is empty or consists only of ASCII whitespace (space, tab, line feed, vertical tab, form feed, or carriage return) SHALL fail the workflow before any agent step runs, with an error naming `acceptance_fix_scope`, whether or not `jq` is installed. Any other character, including non-ASCII whitespace or literal backslash text such as `\f`, SHALL make the value non-blank.

No step of `core:verify-change` SHALL use the Runner-owned `call_agent` tool; the independent tester is an ordinary workflow step in the `acceptance-tester` session. No step prompt SHALL run Agent Validator, directly or through a skill; validation SHALL run only as the `core:run-validator` workflow in its own step. The draft pull request SHALL be pushed and created or updated directly with `git` and `gh`.

Each acceptance round SHALL create `{{session_dir}}/scratch/acceptance-test` and pass it to the tester as `scratch_dir` for temporary files, clones, build outputs, and servers' working directories; the Runner leaves the folder in place, and it is removed with the run's session folder.

Each acceptance round SHALL clear the previous round's `acceptance-round-status.txt` and `acceptance-handoff.md`, keeping `exploration-log.md`, `acceptance-tested-revision.txt`, and `acceptance-findings.md` as the tester's history and diff base, run the tester, then run a deterministic convergence gate. The gate SHALL pass only when the tester's `acceptance-round-status.txt` ends with `READY <local HEAD SHA>`, `acceptance-tested-revision.txt` names local `HEAD`, `exploration-log.md` and `acceptance-handoff.md` in the evidence directory exist and are non-empty, and no tracked file has uncommitted changes. A passing gate SHALL end the rounds. Otherwise, except in the final round, the lead SHALL fix the findings without running Agent Validator or pushing, the `core:run-validator` workflow SHALL run unless `skip_validator` is `true`, a push step SHALL push the branch without force-pushing, and the shared draft-pull-request check that `verify-draft-pr` also uses SHALL verify, with retries, that the single open draft pull request's head equals local `HEAD`. The acceptance-round `core:run-validator` workflow SHALL record its result in `acceptance-validator-result.txt` in the session output directory, overwriting it each round, and a failing validator there SHALL NOT block the rounds or convergence. The final round SHALL end after its gate so no fix is left untested. When `stop_unchanged_acceptance` is `true`, a check directly after the lead's fix SHALL end the rounds, before validation and the push, when local `HEAD` equals the SHA in `acceptance-tested-revision.txt`, no tracked file has uncommitted changes, and the lead wrote no non-empty `acceptance-retest-reason.txt` in the session output directory. That file SHALL be kept for the next round's tester, whose prompt names it as the reason to recheck despite an unchanged revision, and SHALL be cleared before the lead's next fix. The value SHALL be accepted only as `true` or `false`, and when it is `false` the check SHALL NOT run. After the rounds, `acceptance-preparation-status.txt` SHALL record `ACCEPTANCE_COMPLETE` when the gate passes and `ACCEPTANCE_FAILED` otherwise, and on failure the workflow SHALL write a short `acceptance-handoff.md` giving local `HEAD`, the reasons, and pointers to the findings, the assumptions ledger, and any tester-written handoff, which it keeps as `acceptance-handoff-tester.md`. Writing the status SHALL be safe to rerun: a rerun SHALL NOT replace the kept tester handoff with a handoff the workflow generated. When the final handoff check fails, including on a resume that lands on it, its repair SHALL rerun the status step once rather than invoke an agent.

#### Scenario: Acceptance converges after a fix
- **WHEN** the tester reports defects in the first round and readiness for the fixed head in the second
- **THEN** the lead fixes, validation runs, the fix is pushed, the second round's gate passes, and the status is `ACCEPTANCE_COMPLETE`

#### Scenario: Rounds exhausted
- **WHEN** the tester never reports readiness within `acceptance_rounds` rounds
- **THEN** the tester runs `acceptance_rounds` times, the fix, validation, and push steps run only between rounds, the status is `ACCEPTANCE_FAILED`, and `acceptance-handoff.md` points to the open findings

#### Scenario: Red validator after an acceptance fix is recorded without blocking
- **WHEN** Agent Validator still fails after the lead fixes an acceptance round's findings and `skip_validator` is `false`
- **THEN** `acceptance-validator-result.txt` in the session output directory starts with `FAIL` and lists the failing checks, and the fix is still pushed and the next round still runs

#### Scenario: Stale evidence does not converge
- **WHEN** the tester's `READY` status names an earlier revision than local `HEAD`
- **THEN** the gate does not pass

#### Scenario: Simplification runs in a fresh implementor session
- **WHEN** `core:verify-change` reaches its simplification step
- **THEN** the step starts a new session with the user's `implementor` profile, and the `lead-agent` session's history does not include it

#### Scenario: Simplification records an out-of-scope defect
- **WHEN** the simplification pass finds a defect in code the change did not touch
- **THEN** it leaves that code unchanged and `acceptance-assumptions.md` lists the defect in place of the no-unresolved-assumptions statement

#### Scenario: An acceptance fix that changes nothing ends the rounds
- **WHEN** `stop_unchanged_acceptance` is `true` and the lead's acceptance fix commits nothing and writes no retest reason
- **THEN** no further tester round, validation, or push runs, the status is `ACCEPTANCE_FAILED`, and `acceptance-handoff.md` points to the open findings

#### Scenario: A retest reason keeps the rounds going
- **WHEN** `stop_unchanged_acceptance` is `true` and the lead changes no tracked file but writes `acceptance-retest-reason.txt`
- **THEN** the next tester round runs with that file present, and the file is removed before the lead's next fix

#### Scenario: Caller skips the implementation review
- **WHEN** `core:verify-change` runs with `skip_implementation_review=true`
- **THEN** neither the assumption review nor the simplification pass runs, and the run fails at the assumptions handoff check unless the caller already wrote a non-empty `acceptance-assumptions.md`

#### Scenario: Default acceptance-fix scope is unchanged
- **WHEN** a caller runs `core:verify-change` without `acceptance_fix_scope` and an acceptance round's fix step runs
- **THEN** the lead's fix prompt tells it to resolve every high-confidence finding it can, including implementation, CI, PR-state, tooling, setup, and environmental problems, and is otherwise identical to the prompt before the param existed

#### Scenario: Caller narrows the acceptance-fix scope
- **WHEN** a caller passes `acceptance_fix_scope` telling the lead to fix only failures the change caused and to record unrelated CI, tooling, environmental, or `.github/workflows` problems in the assumptions ledger
- **THEN** the lead's fix prompt contains that instruction in place of the default scope sentence and does not contain the default wording, and the rest of the fix guidance, including the rule against dismissing a reproduced spec violation, is still present

#### Scenario: Scope affects only the acceptance-fix step
- **WHEN** a caller passes a non-default `acceptance_fix_scope`
- **THEN** the prompts of the assumption review, the simplification pass, the acceptance tester, and the acceptance-push repair are the same as with the default

#### Scenario: Empty acceptance-fix scope is rejected
- **WHEN** a caller passes `acceptance_fix_scope` as an empty value or one made only of ASCII whitespace, such as spaces, a form feed, or a vertical tab
- **THEN** the workflow fails before the assumption review or any other agent step runs, and the error names `acceptance_fix_scope`
