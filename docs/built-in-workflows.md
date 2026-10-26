---
title: Built-In Workflows
group: Reference
order: 7
description: Embedded workflow namespaces and workflow purposes.
---

# Built-In Workflows

Agent Runner embeds namespaced, versioned workflow files into the binary. Use
the TUI or `agent-runner -list` to browse one version-neutral row per logical
workflow.

Launch built-ins with logical names such as `openspec:change`; Agent Runner
selects the latest embedded version. Exact embedded references such as
`builtin:openspec/change-v3.0.yaml` are available through read-only debug
inspection, not fresh execution.

## Namespaces

| Namespace | Description |
| --- | --- |
| `core` | General-purpose sub-workflows invoked by other workflows and skills. |
| `openspec` | Change planning and implementation using OpenSpec. |
| `spec-driven` | Spec-driven planning and implementation workflows with no OpenSpec dependency. |
| `onboarding` | Guided tours and demos for new users. |

## Core

| Workflow | Purpose |
| --- | --- |
| `core:accept-change` | Run the shared human acceptance, refinement, and re-testing phases for a structured change. |
| `core:check-planning-artifacts` | Verify required files and optional specifications in a planning-artifact directory. |
| `core:commit-change-plan` | Commit one structured change plan and advance its Validator baseline. |
| `core:commit-definition` | Commit an approved definition for a workflow without a planning step (skipped for spec-driven changes), then require a clean worktree before implementation. Used by `core:implement-change`. |
| `core:complete-simple-change` | Implement, flow-test, and interactively review a shared small-change plan. |
| `core:debug` | Debug a failed Agent Runner run and optionally file an issue. |
| `core:define-change` | Run the shared proposal, specification, design, test-plan, and approach-review phases. |
| `core:finalize-pr` | Push PR, wait for CI, fix failures, and repeat until green, with a maximum of three fix cycles. |
| `core:implement-change` | Implement an existing approved change in one `implementor` session. The implementor writes a checklist of how every behavior in the definition, including its error, failure, and cleanup cases, will be verified, in `<change_dir>/checklist.md`; a script checks that the step changed only that file and commits it. The implementor implements against it, saves each check's output under the run's `output/evidence/`, and marks each row `verified`, `failed`, or `not verified`; a script checks that the checklist is complete before the crosscheck's audit, an inspection of the code and evidence that runs nothing, and again after the implementor resolves the findings, and an incomplete checklist goes back to the implementor, who can ask the lead. The prompts defer the checklist to the `codagent:verify-behavior` skill and the implementation and testing guidance to the `codagent:implement-change` skill (both in Agent Skills). The skills' three test tiers apply: affected unit tests while working; a broader run of the agent's choosing, which this workflow asks for once after the build and once after the audit's fixes; and nothing beyond that, which is left to CI. Uncertainty goes to the `lead` through `call_agent` as it arises, and the lead records its rulings in `acceptance-assumptions.md`. Ends with `core:verify-change`, `skip_implementation_review=true`, and `stop_unchanged_acceptance=true`. The task-based predecessor, `core/implement-change-v1.0.yaml`, is deprecated; it stays embedded for the v1.0 and v2.0 change workflows, which reference it by file path. |
| `core:implement-task` | Implement a single task with an agent step followed by a validator retry loop. Its optional `task_compliance` parameter defaults to `true` for direct callers; built-in change workflows explicitly set it to `false`. The task must leave a commit in the run repository, or, when its work was delivered as pushed commits in another local repository, pass through an external delivery record that the gate verifies against that repository's git objects. Pushed state is judged from that clone's local remote-tracking refs without contacting the remote. |
| `core:plan-change` | Run the shared definition validation, task planning, and task-review phases. Its optional `skip_validator` parameter (default `false`) commits the plan without advancing the Agent Validator baseline. |
| `core:review-proposal` | Run adversarial proposal review, lead response, and up to three discussion rounds. |
| `core:review-tasks` | Independently review and autonomously correct a structured task plan. |
| `core:run-validator` | Run Agent Validator with a counted retry loop and fix-on-failure step. Task-compliance requires `task_compliance: "true"` and a non-empty `task_file`; the default is plain validation. |
| `core:verify-change` | Review implementation assumptions, validate, open a draft PR, and run bounded acceptance-test, fix, and revalidation rounds. Its optional `skip_implementation_review` parameter (default `false`, accepted only as `true` or `false`) skips the assumption review and the simplification pass for a caller that already produced `acceptance-assumptions.md` and its own maintainability pass; the check that the assumptions handoff exists still runs. Its optional `stop_unchanged_acceptance` parameter (default `false`) ends the acceptance rounds when the lead's fix leaves the tested revision unchanged and writes no `acceptance-retest-reason.txt`, since another tester round could only repeat the last. Its optional `acceptance_fix_scope` parameter defaults to "Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems." and replaces only that scope sentence in the acceptance-fix prompt. An empty or ASCII-whitespace-only value is rejected before any agent step. |

Some `core:*` workflows are hidden from normal browsing because they are intended to be invoked by higher-level workflows. The shared change-lifecycle workflows accept an artifact directory and validation instructions; the OpenSpec and spec-driven namespaces provide those backend-specific values.

`core:finalize-pr` waits for CI and review feedback in bundled scripts, then resumes the lead agent when fixes are needed. If a CI wait script fails, the lead agent gets one repair attempt to resolve workspace causes such as a missing pull request; the script reruns to decide the outcome. A wait that still fails ends the fix loop and proceeds to final verification, whose wait has its own repair attempt. Its optional `review_bots` parameter accepts comma-separated bot logins to wait for after each push.

## OpenSpec

| Workflow | Purpose |
| --- | --- |
| `openspec:archive-change` | Move an approved change into the archive, verify the commit, and advance the Validator baseline. |
| `openspec:change` | Define, implement against an audited checklist, validate, prepare evidence, support human acceptance, archive, and finalize a feature change. |
| `openspec:implement-change` | Implement reviewed task files, validate the result, open a draft PR, and prepare exploratory acceptance evidence. |
| `openspec:plan-change` | Validate an approved proposal, specs, design, and test plan, then create and review implementation tasks. |
| `openspec:scaffold` | Bootstrap a brand new OpenSpec project, configure validation, and optionally publish it to GitHub. |
| `openspec:simple-change` | Plan, independently crosscheck, implement, validate, flow-test, review, and archive a small branch-local change without creating a PR. |

The `openspec:change` workflow is a full development flow: it collaboratively defines the proposal,
specifications, design, and test plan; autonomously implements the change through `core:implement-change`
(a checklist-driven implementation with a crosscheck audit, with no task breakdown); validates and
prepares acceptance evidence; supports human review; and continues through PR finalization. Its v1.0 and
v2.0 task-based versions remain available by exact reference.
`openspec:simple-change` keeps one lightweight plan and one implementation unit for smaller changes.
It adds a separate independent plan-crosscheck step and a lightweight Tester flow-check step before
human review. The user—not a status-file protocol—decides when testing and refinements are complete. The
workflow does not push, create a pull request, wait for CI, or finalize the branch, so several
independently archived small changes can accumulate on one feature branch.
`openspec:scaffold` runs `openspec init --tools none` so the project has OpenSpec directories without
installing OpenSpec agent skills or slash commands.

You can keep the OpenSpec project in another repository through an `openspec` symlink.
Add `/openspec` (with the leading slash) to `.git/info/exclude` so dirty-tree checks and
`git add -A` ignore the symlink. Plan-commit steps skip the commit; commit the external
repository by hand, and its changes will not appear in this repository's PR. The archive
step reports success without verifying a commit. Agents can only write through the
symlink in YOLO mode.

## Spec-Driven

| Workflow | Purpose |
| --- | --- |
| `spec-driven:change` | Define, implement against an audited checklist, prepare acceptance evidence, support human acceptance, and finalize a feature change without OpenSpec. |
| `spec-driven:implement-change` | Implement reviewed task files, validate the result, open a draft PR, and prepare exploratory acceptance evidence. |
| `spec-driven:plan-change` | Validate an approved proposal, specs, design, and test plan, then create and review implementation tasks without OpenSpec. |
| `spec-driven:scaffold` | Bootstrap a brand new project, configure validation, and optionally publish it to GitHub. |
| `spec-driven:simple-change` | Plan, independently crosscheck, implement, validate, flow-test, and review a small branch-local change without OpenSpec. |

The latest `spec-driven:change` workflow mirrors the full OpenSpec change lifecycle except for OpenSpec-specific artifact handling, CLI validation, and archival. It requires `change_name` and `change_dir`; that directory is expected to live outside the project repository, and the workflow does not commit those external artifacts. `spec-driven:plan-change` and `spec-driven:implement-change` accept the same explicit external artifact directory when run independently. `spec-driven:simple-change` asks the planning session to write artifacts outside the project repository, discovers that directory, uses it for crosscheck and validation, then passes it through the shared implementation, flow-testing, and review phase. The `scaffold` variant is for new project setup.

## Onboarding

| Workflow | Purpose |
| --- | --- |
| `onboarding:advanced` | Introduce advanced Agent Runner concepts and open the help agent. |
| `onboarding:guided-workflow` | Guide the user through planning, tutoring, and autonomous implementation during onboarding. |
| `onboarding:help` | Open an interactive Agent Runner help agent backed by bundled onboarding documentation. |
| `onboarding:onboarding` | Run the optional Agent Runner workflow demo chain. |
| `onboarding:step-types-demo` | Demonstrate Agent Runner workflow step types during onboarding. |
| `onboarding:validator` | Demonstrate Agent Validator setup and the validation feedback loop during onboarding. |

Onboarding workflows are used by first launch and help flows. Several are hidden from normal browsing because they are designed as steps in the onboarding chain.

The `onboarding:validator` workflow includes an internal Agent Validator init step. It passes configured Agent Runner CLI names through to Agent Validator when project agent profiles are available.
