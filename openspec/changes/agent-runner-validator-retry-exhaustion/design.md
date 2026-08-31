## Context

The current validator retry loop contains validation followed by repair in each of three iterations. When validation fails in the third iteration, the repair can succeed but the loop then exhausts without validating the repaired code. Generic loop exhaustion is correctly blocking when a break condition is never reached, so changing counted-loop semantics would affect unrelated workflows.

Agent Runner currently distinguishes raw execution outcomes such as success, failure, and exhaustion, but it has no durable terminal warning status or successful-with-warnings completion state. The run view must identify warnings after the live process exits and when a saved run is opened later.

## Approach

Keep raw executor outcomes and generic loop behavior intact. Add a `warn_on_failure: true` step modifier that designates a failed or exhausted step as a non-blocking warning. The validator retry container will use that policy only for its unresolved final verification; intermediate validator failures remain ordinary retry evidence and a later pass resolves them.

Persist both the original outcome and warning classification. Propagate warning presence through otherwise successful containing steps and to the completed run, while retaining the exact originating leaf for navigation. A warning-bearing run remains success-class for exit status and completed-state handling, including being ineligible for workflow resume, but its displayed completion state is `complete with warnings (N)`.

Reshape the validator sequence to allow at most three repair invocations and to follow each with validation. The last validator invocation is verification-only: a pass completes cleanly; a failure produces the warning and no fourth repair.

## Decisions and Rationale

### Warning is explicit and terminal

Only a failure deliberately designated with `warn_on_failure: true` becomes a warning. The modifier itself makes that terminal failure non-blocking. Existing uses of failure continuation do not automatically make every transient or branch-control failure visible at run completion. This prevents a validator failure that later passes from tainting the final status.

### Outcome and status remain distinct

Warning presentation does not erase whether the underlying execution failed or exhausted. Selected detail and durable evidence continue to show that raw outcome and its output, alongside the warning status and a short explanation that the workflow continued.

### Warning propagation preserves its origin

Otherwise successful ancestor containers reflect that they contain warnings so collapsed trees remain understandable. The warning count and `w` navigation enumerate originating warning steps only, not every ancestor, avoiding duplicate warnings. Navigation follows durable execution order, wraps after the last warning, returns to root scope as needed, expands ancestry, and selects the exact origin leaf.

### Completion with warnings is successful but distinct

`complete with warnings (N)` is persisted and displayed in the live terminal view, run list, and later inspection. It is completed and non-resumable, and the CLI exits successfully. The final executed top-level step remains selected on completion; users opt into warning inspection with `w`.

### Validator warnings do not trigger another agent

The final warning is informational. It exposes the last validator result and points the user to the unresolved evidence, but it does not invoke a lead, alter assumptions-review prompts, start another repair, or require acknowledgment.

## Risks and Trade-offs

- Adding a successful-with-warnings completion state touches execution, persistence, discovery, and rendering; tests must cover all entry paths so saved runs do not degrade to ordinary completion.
- Warning ancestry can over-count if containers and origins are treated alike; the model must count and navigate only origins.
- Historical runs without warning metadata must continue to render exactly as before.
- The validator workflow must cap repairs rather than validator invocations; otherwise the original unverifiable-final-repair defect can recur.

## Verification Strategy

Use TDD. First add a regression that fails validation on the last repair opportunity, succeeds in repair, and proves a follow-up validator invocation occurs. Cover both follow-up outcomes: a pass yields ordinary completion, while a failure launches no further repair, continues subsequent workflow steps, and yields `complete with warnings (1)`. Add focused tests for warning persistence, live and historical rendering, run-list status, exact-origin selection, repeated `w` cycling, ancestor expansion, and the no-warning behavior of recovered failures.
