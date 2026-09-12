package runview

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

// Fixture workflows used by tree- and audit-level tests. They intentionally
// do not load the real files under workflows/: tests should exercise
// BuildTree and ApplyEvent's logic, not drift with the shipping YAML.

// fixtureWorkflowsRoot is the fake absolute root every fixture path is
// anchored under. Tests only need the paths to be absolute and to preserve
// the "workflows/" / "openspec/" suffixes that the trusted-root check and
// a couple of path-suffix assertions care about — no files are written to
// disk.
const fixtureWorkflowsRoot = "/fixtures/workflows"

// fixturePath joins a relative workflow filename under the fake workflows
// root, matching the layout the real workflows use so BuildTree's path
// resolution lands on predictable paths.
func fixturePath(rel string) string {
	return filepath.Join(fixtureWorkflowsRoot, rel)
}

// fixtureImplementChange mirrors the shape of workflows/openspec/
// implement-change-v1.0.yaml closely enough to exercise every branch in
// BuildTree: a for-each loop with a single-sub-workflow body (AutoFlatten),
// followed by an agent step, a sub-workflow step, two shell steps, and a
// final default-mode agent step.
func fixtureImplementChange() model.Workflow {
	return model.Workflow{
		Name: "implement-change",
		Steps: []model.Step{
			{
				ID:   "implement-tasks",
				Loop: &model.Loop{Over: "tasks/*.md", As: "task_file"},
				Steps: []model.Step{
					{ID: "implement-single-task", Workflow: "../core/implement-task-v1.0.yaml"},
				},
			},
			{ID: "review-assumptions", Session: model.SessionResume, Prompt: "review"},
			{ID: "run-validator", Workflow: "../core/run-validator-v1.0.yaml"},
			{ID: "archive", Command: "echo archive"},
			{ID: "archive-verify", Command: "echo verify"},
			{ID: "finalize", Agent: "implementor", Session: model.SessionNew, Prompt: "finalize"},
		},
	}
}

// fixtureImplementTask mirrors workflows/core/implement-task-v1.0.yaml: an interactive
// agent, a resume agent, a sub-workflow, a shell step, another resume agent,
// and a trailing shell step — six children by design.
func fixtureImplementTask() model.Workflow {
	return model.Workflow{
		Name: "implement-task",
		Steps: []model.Step{
			{ID: "implement", Agent: "implementor", Session: model.SessionNew, Prompt: "implement the task"},
			{ID: "simplify", Session: model.SessionResume, Prompt: "simplify"},
			{ID: "run-validator", Workflow: "run-validator-v1.0.yaml"},
			{ID: "check-clean", Command: "test -z \"$(git status --porcelain)\""},
			{ID: "commit-leftovers", Session: model.SessionResume, Prompt: "commit leftovers"},
			{ID: "check-flag", Command: "test x = y"},
		},
	}
}

// fixtureChange mirrors the outer shape of workflows/openspec/change-v1.0.yaml —
// two sub-workflow steps.
func fixtureChange() model.Workflow {
	return model.Workflow{
		Name: "change",
		Steps: []model.Step{
			{ID: "plan", Workflow: "plan-change-v1.0.yaml"},
			{ID: "implement", Workflow: "implement-change-v1.0.yaml"},
		},
	}
}

// fixturePlanChange is a minimal stand-in for plan-change-v1.0.yaml. The nested
// sub-workflow test only ever drills into `implement`, but SubWorkflowLoader
// must still be able to resolve `plan-change-v1.0.yaml` if something asks for it.
func fixturePlanChange() model.Workflow {
	return model.Workflow{Name: "plan-change"}
}

// fixtureRepairPlanChange mirrors a plan workflow whose check declares an
// inline repair: an agent step that produces the guarded response, then the
// shell check that guards it.
func fixtureRepairPlanChange() model.Workflow {
	one := 1
	return model.Workflow{
		Name: "plan-change",
		Steps: []model.Step{
			{ID: "write-plan", Agent: "planner", Mode: model.ModeAutonomous, Prompt: "write the plan"},
			{
				ID: "check-plan", Command: "validate-plan",
				Repair: &model.Repair{Prompt: "fix the plan", Session: model.SessionResume, Max: &one},
			},
		},
	}
}

// fixtureRepairImplementChange mirrors the rerun form: an agent step that
// opens the PR, followed by the check that verifies it and reruns the agent.
func fixtureRepairImplementChange() model.Workflow {
	one := 1
	return model.Workflow{
		Name: "implement-change",
		Steps: []model.Step{
			{ID: "open-draft-pr", Agent: "implementor", Mode: model.ModeAutonomous, Prompt: "open the draft pr"},
			{
				ID: "verify-draft-pr", Command: "gh pr view --json isDraft",
				Repair: &model.Repair{Rerun: "open-draft-pr", Max: &one},
			},
			{ID: "finalize", Command: "echo done"},
		},
	}
}

// inProgressInlineRepairAuditFixture stops at the moment the inline repair
// agent starts, so the check is mid-attempt.
func inProgressInlineRepairAuditFixture() []string {
	full := repairedInlineAuditFixture()
	return full[:6]
}

// repairedInlineAuditFixture records a check that failed once and passed after
// an inline repair agent.
func repairedInlineAuditFixture() []string {
	return []string{
		`2026-09-01T00:00:00Z run_start {}`,
		`2026-09-01T00:00:01Z [write-plan] step_start {"mode":"autonomous","prompt":"write the plan"}`,
		`2026-09-01T00:00:02Z [write-plan] step_end {"outcome":"success","stdout":"plan written","identity":{"attempt":1}}`,
		`2026-09-01T00:00:03Z [check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:04Z [check-plan] repair_attempt_start {"attempt":1,"max":1,"form":"inline","target":"","exit_code":1,"stdout":"","stderr":"plan is missing a tasks section"}`,
		`2026-09-01T00:00:05Z [check-plan, attempt:1, repair] step_start {"mode":"autonomous","prompt":"fix the plan"}`,
		`2026-09-01T00:00:06Z [check-plan, attempt:1, repair] step_end {"outcome":"success","stdout":"added the tasks section","identity":{"attempt":1}}`,
		`2026-09-01T00:00:07Z [check-plan, attempt:1, check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:08Z [check-plan, attempt:1, check-plan] step_end {"outcome":"success","stdout":"plan ok"}`,
		`2026-09-01T00:00:09Z [check-plan] repair_attempt_end {"attempt":1,"form":"inline","outcome":"success","exit_code":0}`,
		`2026-09-01T00:00:10Z [check-plan] step_end {"outcome":"success","stdout":"plan ok","repair_form":"inline","repair_target":"","repair_max":1,"repair_attempts":1,"repair_blocked":false}`,
		`2026-09-01T00:00:11Z run_end {"outcome":"success"}`,
	}
}

// exhaustedInlineAuditFixture records a check whose two inline repair attempts
// both failed.
func exhaustedInlineAuditFixture() []string {
	return []string{
		`2026-09-01T00:00:00Z run_start {}`,
		`2026-09-01T00:00:01Z [write-plan] step_start {"mode":"autonomous","prompt":"write the plan"}`,
		`2026-09-01T00:00:02Z [write-plan] step_end {"outcome":"success","stdout":"plan written","identity":{"attempt":1}}`,
		`2026-09-01T00:00:03Z [check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:04Z [check-plan] repair_attempt_start {"attempt":1,"max":2,"form":"inline","target":"","exit_code":1,"stdout":"","stderr":"plan is missing a tasks section"}`,
		`2026-09-01T00:00:05Z [check-plan, attempt:1, repair] step_start {"mode":"autonomous","prompt":"fix the plan"}`,
		`2026-09-01T00:00:06Z [check-plan, attempt:1, repair] step_end {"outcome":"success","stdout":"tried once","identity":{"attempt":1}}`,
		`2026-09-01T00:00:07Z [check-plan, attempt:1, check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:08Z [check-plan, attempt:1, check-plan] step_end {"outcome":"failed","exit_code":1,"stderr":"still missing a tasks section"}`,
		`2026-09-01T00:00:09Z [check-plan] repair_attempt_end {"attempt":1,"form":"inline","outcome":"failed","exit_code":1}`,
		`2026-09-01T00:00:10Z [check-plan] repair_attempt_start {"attempt":2,"max":2,"form":"inline","target":"","exit_code":1,"stdout":"","stderr":"still missing a tasks section"}`,
		`2026-09-01T00:00:11Z [check-plan, attempt:2, repair] step_start {"mode":"autonomous","prompt":"fix the plan"}`,
		`2026-09-01T00:00:12Z [check-plan, attempt:2, repair] step_end {"outcome":"success","stdout":"tried twice","identity":{"attempt":1}}`,
		`2026-09-01T00:00:13Z [check-plan, attempt:2, check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:14Z [check-plan, attempt:2, check-plan] step_end {"outcome":"failed","exit_code":1,"stderr":"still missing a tasks section"}`,
		`2026-09-01T00:00:15Z [check-plan] repair_attempt_end {"attempt":2,"form":"inline","outcome":"failed","exit_code":1}`,
		`2026-09-01T00:00:16Z [check-plan] step_end {"outcome":"failed","exit_code":1,"stderr":"still missing a tasks section","repair_form":"inline","repair_target":"","repair_max":2,"repair_attempts":2,"repair_blocked":false,"guarded_prefix":"[write-plan]","guarded_attempt":1}`,
		`2026-09-01T00:00:17Z run_end {"outcome":"failed"}`,
	}
}

// blockedRerunAuditFixture records a rerun check that never started an attempt
// because the guarded agent declared REPAIR_BLOCKED.
func blockedRerunAuditFixture() []string {
	return []string{
		`2026-09-01T00:00:00Z run_start {}`,
		`2026-09-01T00:00:01Z [open-draft-pr] step_start {"mode":"autonomous","prompt":"open the draft pr"}`,
		`2026-09-01T00:00:02Z [open-draft-pr] step_end {"outcome":"success","stdout":"push rejected: token lacks workflow scope\nREPAIR_BLOCKED","identity":{"attempt":1}}`,
		`2026-09-01T00:00:03Z [verify-draft-pr] step_start {"command":"gh pr view --json isDraft"}`,
		`2026-09-01T00:00:04Z [verify-draft-pr] repair_blocked {"attempt":0,"response":"push rejected: token lacks workflow scope\nREPAIR_BLOCKED"}`,
		`2026-09-01T00:00:05Z [verify-draft-pr] step_end {"outcome":"failed","exit_code":1,"stderr":"no draft pull request found","repair_form":"rerun","repair_target":"open-draft-pr","repair_max":1,"repair_attempts":0,"repair_blocked":true,"guarded_prefix":"[open-draft-pr]","guarded_attempt":1}`,
		`2026-09-01T00:00:06Z run_end {"outcome":"failed"}`,
	}
}

// inProgressRerunAuditFixture records a live rerun attempt: the replayed
// target is still running.
func inProgressRerunAuditFixture() []string {
	return []string{
		`2026-09-01T00:00:00Z run_start {}`,
		`2026-09-01T00:00:01Z [open-draft-pr] step_start {"mode":"autonomous","prompt":"open the draft pr"}`,
		`2026-09-01T00:00:02Z [open-draft-pr] step_end {"outcome":"success","stdout":"no pr opened","identity":{"attempt":1}}`,
		`2026-09-01T00:00:03Z [verify-draft-pr] step_start {"command":"gh pr view --json isDraft"}`,
		`2026-09-01T00:00:04Z [verify-draft-pr] repair_attempt_start {"attempt":1,"max":1,"form":"rerun","target":"open-draft-pr","exit_code":1,"stdout":"","stderr":"no draft pull request found"}`,
		`2026-09-01T00:00:05Z [verify-draft-pr, attempt:1, open-draft-pr] step_start {"mode":"autonomous","prompt":"open the draft pr"}`,
	}
}

// recoveredRerunAuditFixture continues inProgressRerunAuditFixture through the
// replayed check passing.
func recoveredRerunAuditFixture() []string {
	return append(inProgressRerunAuditFixture(),
		`2026-09-01T00:00:06Z [verify-draft-pr, attempt:1, open-draft-pr] step_end {"outcome":"success","stdout":"pr opened","identity":{"attempt":1}}`,
		`2026-09-01T00:00:07Z [verify-draft-pr, attempt:1, verify-draft-pr] step_start {"command":"gh pr view --json isDraft"}`,
		`2026-09-01T00:00:08Z [verify-draft-pr, attempt:1, verify-draft-pr] step_end {"outcome":"success","stdout":"draft pr present"}`,
		`2026-09-01T00:00:09Z [verify-draft-pr] repair_attempt_end {"attempt":1,"form":"rerun","outcome":"success","exit_code":0}`,
		`2026-09-01T00:00:10Z [verify-draft-pr] step_end {"outcome":"success","stdout":"draft pr present","repair_form":"rerun","repair_target":"open-draft-pr","repair_max":1,"repair_attempts":1,"repair_blocked":false}`,
		`2026-09-01T00:00:11Z [finalize] step_start {"command":"echo done"}`,
	)
}

// resumedPlainCheckAuditFixture records a check with no repair that failed,
// and passed when the run was resumed. The earlier failure record must stay
// inspectable.
func resumedPlainCheckAuditFixture() []string {
	return []string{
		`2026-09-01T00:00:00Z run_start {}`,
		`2026-09-01T00:00:01Z [write-plan] step_start {"mode":"autonomous","prompt":"write the plan"}`,
		`2026-09-01T00:00:02Z [write-plan] step_end {"outcome":"success","stdout":"plan written","identity":{"attempt":1}}`,
		`2026-09-01T00:00:03Z [check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:04Z [check-plan] step_end {"outcome":"failed","exit_code":1,"stderr":"plan is missing a tasks section","guarded_prefix":"[write-plan]","guarded_attempt":1}`,
		`2026-09-01T00:00:05Z run_end {"outcome":"failed"}`,
		`2026-09-01T00:01:00Z run_start {}`,
		`2026-09-01T00:01:01Z [check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:01:02Z [check-plan] step_end {"outcome":"success","stdout":"plan ok"}`,
		`2026-09-01T00:01:03Z run_end {"outcome":"success"}`,
	}
}

// preRepairAuditFixture is a pre-change audit log: no repair events at all.
// Its rendering must not move by a single byte.
func preRepairAuditFixture() []string {
	return []string{
		`2026-09-01T00:00:00Z run_start {}`,
		`2026-09-01T00:00:01Z [write-plan] step_start {"mode":"autonomous","prompt":"write the plan"}`,
		`2026-09-01T00:00:02Z [write-plan] step_end {"outcome":"success","stdout":"plan written","identity":{"attempt":1}}`,
		`2026-09-01T00:00:03Z [check-plan] step_start {"command":"validate-plan"}`,
		`2026-09-01T00:00:04Z [check-plan] step_end {"outcome":"failed","exit_code":1,"stderr":"plan is missing a tasks section"}`,
		`2026-09-01T00:00:05Z run_end {"outcome":"failed"}`,
	}
}

// applyFixtureAudit replays recorded audit lines into a tree, skipping lines
// the parser rejects so a malformed fixture fails loudly in the assertions
// rather than silently.
func applyFixtureAudit(t *testing.T, tree *Tree, lines []string) {
	t.Helper()
	for _, line := range lines {
		event, err := ParseLine(line)
		if err != nil {
			t.Fatalf("fixture line %q: %v", line, err)
		}
		tree.ApplyEvent(event)
	}
}

// fixtureSubLoader returns a SubWorkflowLoader that resolves fixture
// sub-workflow paths by basename. Using the basename lets the caller use
// any absolute path (the trusted-root check operates on the whole path; we
// only need to look up which fixture it points at).
func fixtureSubLoader() func(string) (model.Workflow, error) {
	return func(path string) (model.Workflow, error) {
		switch filepath.Base(path) {
		case "implement-change-v1.0.yaml":
			return fixtureImplementChange(), nil
		case "implement-task-v1.0.yaml":
			return fixtureImplementTask(), nil
		case "change-v1.0.yaml":
			return fixtureChange(), nil
		case "plan-change-v1.0.yaml":
			return fixturePlanChange(), nil
		}
		return model.Workflow{}, fmt.Errorf("no fixture registered for %s", path)
	}
}
