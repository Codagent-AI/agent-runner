package runview

import (
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestBuildTree_ImplementChange(t *testing.T) {
	wf := fixtureImplementChange()
	tree := BuildTree(&wf, fixturePath("openspec/implement-change.yaml"))

	if got := tree.Root.ID; got != "implement-change" {
		t.Errorf("root ID: want implement-change, got %q", got)
	}
	if tree.Root.Type != NodeRoot {
		t.Errorf("root type: want NodeRoot, got %v", tree.Root.Type)
	}
	if got := len(tree.Root.Children); got != 6 {
		t.Fatalf("root children: want 6, got %d", got)
	}

	loop := tree.Root.Children[0]
	if loop.ID != "implement-tasks" || loop.Type != NodeLoop {
		t.Errorf("want loop 'implement-tasks', got %q type=%v", loop.ID, loop.Type)
	}
	if loop.StaticLoopOver == "" || loop.StaticLoopAs != "task_file" {
		t.Errorf("loop over/as mismatch: over=%q as=%q", loop.StaticLoopOver, loop.StaticLoopAs)
	}
	if !loop.AutoFlatten {
		t.Errorf("loop with single sub-workflow body should have AutoFlatten")
	}
	if len(loop.Body) != 1 || loop.Body[0].Type != NodeSubWorkflow {
		t.Fatalf("loop body must be a single sub-workflow, got %d items", len(loop.Body))
	}
	if loop.Body[0].ID != "implement-single-task" || loop.Body[0].StaticWorkflow != "../core/implement-task.yaml" {
		t.Errorf("sub-workflow body mismatch: id=%q workflow=%q",
			loop.Body[0].ID, loop.Body[0].StaticWorkflow)
	}

	review := tree.Root.Children[1]
	if review.ID != "review-assumptions" {
		t.Errorf("want review-assumptions at index 1, got %q", review.ID)
	}

	validator := tree.Root.Children[2]
	if validator.ID != "run-validator" || validator.Type != NodeSubWorkflow {
		t.Errorf("want run-validator sub-workflow at index 2, got id=%q type=%v", validator.ID, validator.Type)
	}

	archive := tree.Root.Children[3]
	if archive.ID != "archive" || archive.Type != NodeShell {
		t.Errorf("want archive shell, got id=%q type=%v", archive.ID, archive.Type)
	}
	if archive.StaticCommand == "" {
		t.Errorf("expected static command on archive")
	}

	finalize := tree.Root.Children[5]
	if finalize.ID != "finalize" {
		t.Fatalf("want finalize at index 5, got %q", finalize.ID)
	}
	// Without explicit mode, default is interactive.
	if finalize.Type != NodeInteractiveAgent {
		t.Errorf("finalize default type: want NodeInteractiveAgent, got %v", finalize.Type)
	}

	for _, c := range tree.Root.Children {
		if c.Status != StatusPending {
			t.Errorf("child %q: want pending, got %v", c.ID, c.Status)
		}
	}
}

func TestBuildTree_ImplementTask(t *testing.T) {
	wf := fixtureImplementTask()
	tree := BuildTree(&wf, fixturePath("core/implement-task.yaml"))

	if tree.Root.ID != "implement-task" {
		t.Errorf("root ID: got %q", tree.Root.ID)
	}
	if got := len(tree.Root.Children); got != 6 {
		t.Fatalf("want 6 children, got %d", got)
	}

	implement := tree.Root.Children[0]
	if implement.ID != "implement" || implement.Type != NodeInteractiveAgent {
		t.Errorf("implement: id=%q type=%v", implement.ID, implement.Type)
	}
	if implement.StaticPrompt == "" {
		t.Errorf("expected static prompt")
	}
	if implement.StaticAgent != "implementor" {
		t.Errorf("agent: want implementor, got %q", implement.StaticAgent)
	}

	subwf := tree.Root.Children[2]
	if subwf.ID != "run-validator" || subwf.Type != NodeSubWorkflow {
		t.Errorf("run-validator: id=%q type=%v", subwf.ID, subwf.Type)
	}
	if subwf.SubLoaded {
		t.Errorf("sub-workflow body should be lazy-loaded, not eager")
	}
	if len(subwf.Children) != 0 {
		t.Errorf("sub-workflow children should start empty")
	}

	shell := tree.Root.Children[3]
	if shell.ID != "check-clean" || shell.Type != NodeShell {
		t.Errorf("check-clean: id=%q type=%v", shell.ID, shell.Type)
	}
}

func TestBuildTreeClassifiesScriptAndUISteps(t *testing.T) {
	wf := &model.Workflow{
		Name: "onboarding",
		Steps: []model.Step{
			{ID: "detect", Script: "detect-adapters.sh", Capture: "detected"},
			{
				ID:      "pick",
				Mode:    model.ModeUI,
				Title:   "Pick",
				Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
			},
		},
	}

	tree := BuildTree(wf, "workflow.yaml")
	script := tree.Root.Children[0]
	ui := tree.Root.Children[1]

	if script.Type != NodeScript {
		t.Fatalf("script step type = %v, want NodeScript", script.Type)
	}
	if script.StaticScript != "detect-adapters.sh" {
		t.Fatalf("script StaticScript = %q, want detect-adapters.sh", script.StaticScript)
	}
	if script.CaptureName != "detected" {
		t.Fatalf("script CaptureName = %q, want detected", script.CaptureName)
	}
	if ui.Type == NodeRoot {
		t.Fatalf("ui step type = NodeRoot, want a concrete ui type")
	}
	if typeGlyph(script.Type) == "" {
		t.Fatal("script type glyph is empty")
	}
	if typeGlyph(ui.Type) == "" {
		t.Fatal("ui type glyph is empty")
	}
	if typeGlyph(ui.Type) == typeGlyph(script.Type) {
		t.Fatal("ui and script glyphs should be visually distinct")
	}
}

func TestEnsureIteration_CreatesAndSeedsBody(t *testing.T) {
	wf := fixtureImplementChange()
	tree := BuildTree(&wf, fixturePath("openspec/implement-change.yaml"))
	loop := tree.Root.Children[0]

	iter := ensureIteration(loop, 0)
	if iter == nil || iter.Type != NodeIteration {
		t.Fatalf("ensureIteration returned bad node: %+v", iter)
	}
	if iter.IterationIndex != 0 {
		t.Errorf("iter index: want 0, got %d", iter.IterationIndex)
	}
	if len(iter.Children) != 1 {
		t.Fatalf("iter should have 1 child cloned from body, got %d", len(iter.Children))
	}
	if iter.FlattenTarget == nil {
		t.Errorf("iteration of single-sub-workflow loop should have FlattenTarget set")
	}
	if iter.FlattenTarget != iter.Children[0] {
		t.Errorf("FlattenTarget should point to iteration's sub-workflow child")
	}

	// Repeated call returns the same iteration.
	again := ensureIteration(loop, 0)
	if again != iter {
		t.Errorf("ensureIteration should be idempotent; got new node")
	}

	iter2 := ensureIteration(loop, 1)
	if iter2 == iter {
		t.Errorf("different index should yield a different node")
	}
}

func TestDrilldown_AutoFlatten(t *testing.T) {
	wf := fixtureImplementChange()
	tree := BuildTree(&wf, fixturePath("openspec/implement-change.yaml"))
	loop := tree.Root.Children[0]
	iter := ensureIteration(loop, 2)

	target := iter.Drilldown()
	if target == iter {
		t.Errorf("auto-flattened iteration should drilldown to FlattenTarget, not self")
	}
	if target != iter.Children[0] {
		t.Errorf("drilldown target mismatch")
	}
}

func TestDrilldown_NoFlattenOnShellBodyLoop(t *testing.T) {
	maxN := 3
	s := &model.Step{
		ID: "counted",
		Loop: &model.Loop{
			Max: &maxN,
		},
		Steps: []model.Step{
			{ID: "only-shell", Command: "echo hi"},
		},
	}
	loop := buildStepNode(s, nil)
	if loop.AutoFlatten {
		t.Fatalf("loop whose body is a shell step must not auto-flatten")
	}
	iter := ensureIteration(loop, 0)
	if iter.FlattenTarget != nil {
		t.Errorf("iteration must not have FlattenTarget when loop body is a shell step")
	}
	if iter.Drilldown() != iter {
		t.Errorf("drilldown should return self when no flatten target")
	}
}
