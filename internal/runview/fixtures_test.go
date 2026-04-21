package runview

import (
	"fmt"
	"path/filepath"

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
// implement-change.yaml closely enough to exercise every branch in
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
					{ID: "implement-single-task", Workflow: "../core/implement-task.yaml"},
				},
			},
			{ID: "review-assumptions", Session: model.SessionResume, Prompt: "review"},
			{ID: "run-validator", Workflow: "../core/run-validator.yaml"},
			{ID: "archive", Command: "echo archive"},
			{ID: "archive-verify", Command: "echo verify"},
			{ID: "finalize", Agent: "implementor", Session: model.SessionNew, Prompt: "finalize"},
		},
	}
}

// fixtureImplementTask mirrors workflows/core/implement-task.yaml: an interactive
// agent, a resume agent, a sub-workflow, a shell step, another resume agent,
// and a trailing shell step — six children by design.
func fixtureImplementTask() model.Workflow {
	return model.Workflow{
		Name: "implement-task",
		Steps: []model.Step{
			{ID: "implement", Agent: "implementor", Session: model.SessionNew, Prompt: "implement the task"},
			{ID: "simplify", Session: model.SessionResume, Prompt: "simplify"},
			{ID: "run-validator", Workflow: "run-validator.yaml"},
			{ID: "check-clean", Command: "test -z \"$(git status --porcelain)\""},
			{ID: "commit-leftovers", Session: model.SessionResume, Prompt: "commit leftovers"},
			{ID: "check-flag", Command: "test x = y"},
		},
	}
}

// fixtureChange mirrors the outer shape of workflows/openspec/change.yaml —
// two sub-workflow steps.
func fixtureChange() model.Workflow {
	return model.Workflow{
		Name: "change",
		Steps: []model.Step{
			{ID: "plan", Workflow: "plan-change.yaml"},
			{ID: "implement", Workflow: "implement-change.yaml"},
		},
	}
}

// fixturePlanChange is a minimal stand-in for plan-change.yaml. The nested
// sub-workflow test only ever drills into `implement`, but SubWorkflowLoader
// must still be able to resolve `plan-change.yaml` if something asks for it.
func fixturePlanChange() model.Workflow {
	return model.Workflow{Name: "plan-change"}
}

// fixtureSubLoader returns a SubWorkflowLoader that resolves fixture
// sub-workflow paths by basename. Using the basename lets the caller use
// any absolute path (the trusted-root check operates on the whole path; we
// only need to look up which fixture it points at).
func fixtureSubLoader() func(string) (model.Workflow, error) {
	return func(path string) (model.Workflow, error) {
		switch filepath.Base(path) {
		case "implement-change.yaml":
			return fixtureImplementChange(), nil
		case "implement-task.yaml":
			return fixtureImplementTask(), nil
		case "change.yaml":
			return fixtureChange(), nil
		case "plan-change.yaml":
			return fixturePlanChange(), nil
		}
		return model.Workflow{}, fmt.Errorf("no fixture registered for %s", path)
	}
}
