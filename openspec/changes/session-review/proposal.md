## Why

The implement-task workflow currently relies on a lightweight inline self-review (4 bullet points in the implement-and-validate skill) that doesn't systematically verify every spec scenario and Done When criterion. Assumptions and context gaps from implementation sessions vanish entirely — there is no feedback loop to improve task files or project context over time. Adding dedicated self-review and session-report steps to the workflow YAML makes quality assurance and retrospective learning structural rather than optional.

## What Changes

- Add a `self-review` agent step to `implement-task.yaml` between the `implement` and `run-validator` steps. This step reviews work against the task file's spec scenarios and Done When criteria, fixing any gaps before the validator runs.
- Add a `session-report` agent step to `implement-task.yaml` as the final step. This step audits the implementation session for assumptions and context gaps, producing a report for human review.
- The `implement-and-validate` SKILL.md's inline self-review section becomes redundant with the workflow-level step but is **not** removed in this change (skill and workflow are independent execution paths).

## Capabilities

### New Capabilities

- `self-review-step`: Defines the behavior of the self-review agent step within the implement-task workflow — when it runs, what context it receives (task file, implementation session), and how it interacts with surrounding steps (fixes feed into the validator).
- `session-report-step`: Defines the behavior of the session-report agent step within the implement-task workflow — when it runs, what context it receives, and the output format for human consumption.

### Modified Capabilities

_(none — no existing spec-level requirements change; this adds new steps without altering the behavior of existing ones)_

## Out of Scope

- Modifying the `implement-and-validate` SKILL.md — the skill is an independent execution path and its inline self-review is left as-is.
- Modifying `implement-change.yaml` — that workflow delegates to `implement-task.yaml` and inherits the new steps automatically.
- Changing the self-review or session-report skill definitions themselves — they are consumed as-is.
- Adding new workflow engine features — the existing step model (headless agent steps, session strategies, step ordering) supports this change without modification.

## Impact

- **Workflow**: `implement-task.yaml` gains two new steps, increasing per-task execution time (two additional agent invocations).
- **Session management**: The self-review step needs access to what was implemented (session resume or task file reference). The session-report step similarly needs session context to audit assumptions.
- **Output**: Session reports produce structured output (assumption audit table, context gaps list) that surfaces alongside existing workflow logs.
- **No API or dependency changes** — this is purely workflow configuration.
