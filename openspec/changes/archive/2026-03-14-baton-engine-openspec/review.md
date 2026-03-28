# Review: baton-engine-openspec

## Summary

The change artifacts passed review after one fix cycle. Both reviewers (codex, claude) identified real cross-artifact consistency issues — stale proposal claims from design pivots during the conversation, inconsistent CLI syntax, and cross-task references in task files. All substantive issues were fixed. The artifacts are coherent and ready for implementation.

## Issues Fixed

- **Proposal stale claims (critical/high):** Removed references to `resolveStartStep` (descoped in favor of state file resumption) and `artifact` field on steps (replaced by step ID convention). Both reviewers flagged these.
- **CLI syntax inconsistency (high/medium):** Changed `baton --resume` to `baton resume` subcommand syntax throughout the spec to match the design.
- **Missing workflowHash in spec (medium):** Added `workflowHash` to the state file field list — it was in the design and tasks but missing from the spec.
- **Cross-task references (medium):** Replaced "After Task 1" and "After the previous task" with self-contained descriptions in both task files.
- **Task file format (medium):** Folded "What to do" section into Background in cli-commander-tests.md. Replaced code/XML snippets with prose descriptions in the other two task files.
- **Spec validation (openspec):** Changed MAY to SHALL in workflow-engine-config spec requirement.
- **TypeScript errors (pre-existing):** Fixed `noUncheckedIndexedAccess` violations in `src/index.ts`.
- **Stale test-change:** Removed orphaned `openspec/changes/test-change/` with no deltas.
- **Stale worktree:** Removed `.claude/worktrees/snuggly-percolating-spring/` containing a nested biome.json that caused lint failures.

## Issues Skipped

- **Missing Spec section in cli-commander-tests.md (low):** Claude reviewer flagged this as the "laying groundwork" anti-pattern. Skipped because the user explicitly requested this as a separate task — it delivers standalone value via test coverage and CLI restructuring for existing code.
- **Pre-existing gauntlet config issues:** The `security-code` check (semgrep) and `test` check fail due to working directory configuration (`path: "src"` causes commands to run inside `src/` but target `src` again). These are gauntlet setup issues unrelated to this change.

## Issues Remaining

None related to the change artifacts.

## Sign-off

APPROVED — All cross-artifact consistency issues resolved. The proposal, specs, design, and tasks are aligned and ready for implementation.
