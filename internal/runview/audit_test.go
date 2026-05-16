package runview

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		wantTS  string
		wantPre string
		wantTyp string
		want    map[string]any
	}{
		{
			name:    "prefixed event",
			line:    `2024-01-01T00:00:00Z [archive] step_start {"command":"echo hi"}`,
			wantTS:  "2024-01-01T00:00:00Z",
			wantPre: "[archive]",
			wantTyp: "step_start",
			want:    map[string]any{"command": "echo hi"},
		},
		{
			name:    "root event without prefix",
			line:    `2024-01-01T00:00:00Z run_start {}`,
			wantTS:  "2024-01-01T00:00:00Z",
			wantPre: "",
			wantTyp: "run_start",
			want:    map[string]any{},
		},
		{
			name:    "deeply nested prefix",
			line:    `2024-01-01T00:00:00Z [task-loop:0, verify, sub:verify-task, check] step_end {"outcome":"success","duration_ms":42}`,
			wantTS:  "2024-01-01T00:00:00Z",
			wantPre: "[task-loop:0, verify, sub:verify-task, check]",
			wantTyp: "step_end",
			want:    map[string]any{"outcome": "success", "duration_ms": float64(42)},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseLine(c.line)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got.Timestamp != c.wantTS {
				t.Errorf("ts: want %q got %q", c.wantTS, got.Timestamp)
			}
			if got.Prefix != c.wantPre {
				t.Errorf("prefix: want %q got %q", c.wantPre, got.Prefix)
			}
			if got.Type != c.wantTyp {
				t.Errorf("type: want %q got %q", c.wantTyp, got.Type)
			}
			for k, v := range c.want {
				if got.Data[k] != v {
					t.Errorf("data[%q]: want %v got %v", k, v, got.Data[k])
				}
			}
		})
	}
}

func TestParsePrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   []prefixToken
	}{
		{
			name:   "empty",
			prefix: "",
			want:   nil,
		},
		{
			name:   "single step",
			prefix: "[archive]",
			want:   []prefixToken{{stepID: "archive"}},
		},
		{
			name:   "iteration token",
			prefix: "[task-loop:2]",
			want:   []prefixToken{{stepID: "task-loop", iteration: iptr(2)}},
		},
		{
			name:   "sub-workflow token",
			prefix: "[verify, sub:verify-task, check]",
			want: []prefixToken{
				{stepID: "verify"},
				{subName: "verify-task"},
				{stepID: "check"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsePrefix(c.prefix)
			if len(got) != len(c.want) {
				t.Fatalf("len: want %d got %d (%v)", len(c.want), len(got), got)
			}
			for i := range got {
				if got[i].stepID != c.want[i].stepID {
					t.Errorf("token %d stepID: want %q got %q", i, c.want[i].stepID, got[i].stepID)
				}
				if got[i].subName != c.want[i].subName {
					t.Errorf("token %d sub: want %q got %q", i, c.want[i].subName, got[i].subName)
				}
				if (got[i].iteration == nil) != (c.want[i].iteration == nil) {
					t.Errorf("token %d iter presence mismatch", i)
				} else if got[i].iteration != nil && *got[i].iteration != *c.want[i].iteration {
					t.Errorf("token %d iter: want %d got %d", i, *c.want[i].iteration, *got[i].iteration)
				}
			}
		})
	}
}

func iptr(v int) *int { return &v }

// buildImplementChangeTree builds an implement-change tree from an in-memory
// fixture and wires up a fixture-backed SubWorkflowLoader so tests never read
// the shipping workflows/ YAML.
func buildImplementChangeTree(t *testing.T) *Tree {
	t.Helper()
	wf := fixtureImplementChange()
	tree := BuildTree(&wf, fixturePath("openspec/implement-change.yaml"))
	tree.SubWorkflowLoader = fixtureSubLoader()
	return tree
}

func TestFilterAuditEventsForWorkflowState_DropsFutureEventsFromOldWorkflowHash(t *testing.T) {
	wf := model.Workflow{
		Name: "onboarding",
		Steps: []model.Step{
			{ID: "step-types-demo", Workflow: "step-types-demo.yaml"},
			{ID: "guided-workflow", Workflow: "guided-workflow.yaml"},
			{ID: "validator", Workflow: "validator.yaml"},
			{ID: "advanced", Workflow: "advanced.yaml"},
			{ID: "set-completed", Command: "agent-runner internal write-setting onboarding.completed_at now"},
		},
	}
	tree := BuildTree(&wf, fixturePath("onboarding/onboarding.yaml"))
	events := []RawEvent{
		{Type: "run_start", Data: map[string]any{"workflow_hash": "old"}},
		{Prefix: "[step-types-demo]", Type: "step_start", Data: map[string]any{}},
		{Prefix: "[step-types-demo]", Type: "step_end", Data: map[string]any{"outcome": "success"}},
		{Prefix: "[set-completed]", Type: "step_start", Data: map[string]any{"command": "agent-runner internal write-setting onboarding.completed_at now"}},
		{Prefix: "[set-completed]", Type: "step_end", Data: map[string]any{"outcome": "success", "exit_code": float64(0)}},
		{Type: "run_end", Data: map[string]any{"outcome": "success"}},
		{Type: "run_start", Data: map[string]any{"workflow_hash": "new"}},
		{Prefix: "[guided-workflow]", Type: "step_start", Data: map[string]any{}},
	}

	for _, event := range filterAuditEventsForWorkflowState(events, "new", tree.Root, "guided-workflow") {
		tree.ApplyEvent(event)
	}

	if got := childByID(tree.Root, "step-types-demo").Status; got != StatusSuccess {
		t.Fatalf("completed step before resume point should be retained, got %v", got)
	}
	if got := childByID(tree.Root, "guided-workflow").Status; got != StatusInProgress {
		t.Fatalf("current step from matching hash should be retained, got %v", got)
	}
	if got := childByID(tree.Root, "set-completed").Status; got != StatusPending {
		t.Fatalf("future step from old workflow hash should be ignored, got %v", got)
	}
}

func TestApplyEvent_StepStartStepEnd(t *testing.T) {
	tree := buildImplementChangeTree(t)
	tree.ApplyEvent(RawEvent{
		Prefix: "[archive]",
		Type:   "step_start",
		Data:   map[string]any{"command": "openspec archive view-run"},
	})
	archive := childByID(tree.Root, "archive")
	if archive.Status != StatusInProgress {
		t.Errorf("status: want in-progress, got %v", archive.Status)
	}
	if archive.InterpolatedCommand != "openspec archive view-run" {
		t.Errorf("interpolated command not set: %q", archive.InterpolatedCommand)
	}

	tree.ApplyEvent(RawEvent{
		Prefix: "[archive]",
		Type:   "step_end",
		Data:   map[string]any{"outcome": "success", "exit_code": float64(0), "duration_ms": float64(2400), "stderr": ""},
	})
	if archive.Status != StatusSuccess {
		t.Errorf("status: want success, got %v", archive.Status)
	}
	if archive.ExitCode == nil || *archive.ExitCode != 0 {
		t.Errorf("exit code not recorded: %v", archive.ExitCode)
	}
	if archive.DurationMs == nil || *archive.DurationMs != 2400 {
		t.Errorf("duration not recorded: %v", archive.DurationMs)
	}
}

// TestApplyEvent_StepStartAfterFailure covers resume-after-failure: when a
// prior run's step_end set the node to StatusFailed, a subsequent step_start
// for the same node must flip it back to StatusInProgress so the TUI renders
// the blinking "running" indicator instead of the stale failure X. Same for
// an aborted prior run: Aborted must be cleared so the indicator blinks.
func TestApplyEvent_StepStartAfterFailure(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
	}{
		{"after failed", "failed"},
		{"after aborted", "aborted"},
		{"after success", "success"},
		{"after skipped", "skipped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := buildImplementChangeTree(t)
			tree.ApplyEvent(RawEvent{
				Prefix: "[archive]",
				Type:   "step_end",
				Data:   map[string]any{"outcome": c.outcome},
			})
			tree.ApplyEvent(RawEvent{
				Prefix: "[archive]",
				Type:   "step_start",
				Data:   map[string]any{"command": "openspec archive view-run"},
			})
			arch := childByID(tree.Root, "archive")
			if arch.Status != StatusInProgress {
				t.Errorf("status after restart: want in-progress, got %v", arch.Status)
			}
			if arch.Aborted {
				t.Errorf("Aborted flag should be cleared on restart, still true")
			}
			if arch.Outcome != "" {
				t.Errorf("Outcome should be cleared on restart, got %q", arch.Outcome)
			}
		})
	}
}

func TestApplyEvent_RunStartAfterFailureClearsRootFailure(t *testing.T) {
	tree := buildImplementChangeTree(t)
	tree.ApplyEvent(RawEvent{
		Type: "run_end",
		Data: map[string]any{"outcome": "failed"},
	})
	if tree.Root.Status != StatusFailed {
		t.Fatalf("precondition: root status = %v, want failed", tree.Root.Status)
	}

	tree.ApplyEvent(RawEvent{
		Type: "run_start",
		Data: map[string]any{},
	})

	if tree.Root.Status != StatusInProgress {
		t.Fatalf("root status after restart = %v, want in-progress", tree.Root.Status)
	}
	if tree.Root.Outcome != "" {
		t.Fatalf("root outcome after restart = %q, want cleared", tree.Root.Outcome)
	}
	if tree.Root.Aborted {
		t.Fatal("root aborted flag should be cleared on restart")
	}
}

func TestApplyEvent_StatusMapping(t *testing.T) {
	cases := []struct {
		outcome string
		want    NodeStatus
		aborted bool
	}{
		{"success", StatusSuccess, false},
		{"exhausted", StatusSuccess, false},
		{"failed", StatusFailed, false},
		{"skipped", StatusSkipped, false},
		{"aborted", StatusInProgress, true},
	}
	for _, c := range cases {
		t.Run(c.outcome, func(t *testing.T) {
			tree := buildImplementChangeTree(t)
			tree.ApplyEvent(RawEvent{
				Prefix: "[archive]",
				Type:   "step_end",
				Data:   map[string]any{"outcome": c.outcome},
			})
			arch := childByID(tree.Root, "archive")
			if arch.Status != c.want {
				t.Errorf("outcome %s: want status %v got %v", c.outcome, c.want, arch.Status)
			}
			if arch.Outcome != c.outcome {
				t.Errorf("outcome %s: raw outcome = %q", c.outcome, arch.Outcome)
			}
			if arch.Aborted != c.aborted {
				t.Errorf("outcome %s: want aborted=%v got %v", c.outcome, c.aborted, arch.Aborted)
			}
		})
	}
}

func TestApplyEvent_IterationStart_CreatesChildWithBinding(t *testing.T) {
	tree := buildImplementChangeTree(t)
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks]",
		Type:   "step_start",
		Data: map[string]any{
			"loop_type":        "for-each",
			"glob_pattern":     "tasks/*.md",
			"resolved_matches": []any{"tasks/01.md", "tasks/02.md", "tasks/03.md"},
		},
	})
	loop := childByID(tree.Root, "implement-tasks")
	if loop.LoopType != "for-each" {
		t.Errorf("loop type: got %q", loop.LoopType)
	}
	if len(loop.LoopMatches) != 3 {
		t.Errorf("loop matches: want 3, got %d", len(loop.LoopMatches))
	}
	// step_start pre-creates placeholder iterations for every known match so
	// pending iterations are visible in the step list from the start.
	if len(loop.Children) != 3 {
		t.Fatalf("want 3 pre-created iterations after step_start, got %d", len(loop.Children))
	}
	for i, want := range []string{"tasks/01.md", "tasks/02.md", "tasks/03.md"} {
		if loop.Children[i].BindingValue != want {
			t.Errorf("iter %d binding: got %q, want %q", i, loop.Children[i].BindingValue, want)
		}
		if loop.Children[i].Status != StatusPending {
			t.Errorf("iter %d status: want pending, got %v", i, loop.Children[i].Status)
		}
	}

	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks:0]",
		Type:   "iteration_start",
		Data: map[string]any{
			"iteration": float64(0),
			"loop_var":  map[string]any{"task_file": "tasks/01.md"},
		},
	})
	if len(loop.Children) != 3 {
		t.Fatalf("iter count should stay 3 after iteration_start, got %d", len(loop.Children))
	}
	iter0 := loop.Children[0]
	if iter0.IterationIndex != 0 {
		t.Errorf("iter index: got %d", iter0.IterationIndex)
	}
	if iter0.BindingValue != "tasks/01.md" {
		t.Errorf("binding: got %q", iter0.BindingValue)
	}
	if iter0.Status != StatusInProgress {
		t.Errorf("status: want in-progress, got %v", iter0.Status)
	}

	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks:0]",
		Type:   "iteration_end",
		Data:   map[string]any{"outcome": "success"},
	})
	if iter0.Status != StatusSuccess {
		t.Errorf("iter end: want success, got %v", iter0.Status)
	}

	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks]",
		Type:   "step_end",
		Data: map[string]any{
			"outcome":              "exhausted",
			"iterations_completed": float64(1),
			"break_triggered":      false,
		},
	})
	if loop.Status != StatusSuccess {
		t.Errorf("exhausted should map to success, got %v", loop.Status)
	}
	if loop.IterationsCompleted != 1 {
		t.Errorf("iterations completed: %d", loop.IterationsCompleted)
	}
}

func TestApplyEvent_SubWorkflowLazyLoad(t *testing.T) {
	tree := buildImplementChangeTree(t)
	// Walk into iteration 0's sub-workflow step.
	prefix := "[implement-tasks:0, implement-single-task, sub:implement-task]"
	tree.ApplyEvent(RawEvent{
		Prefix: prefix,
		Type:   "sub_workflow_start",
		Data: map[string]any{
			"workflow_name": "implement-task",
			"workflow_path": "/tmp/implement-task.yaml",
			"context": map[string]any{
				"params": map[string]any{"task_file": "tasks/01.md"},
			},
		},
	})

	loop := childByID(tree.Root, "implement-tasks")
	iter0 := loop.Children[0]
	sub := childByID(iter0, "implement-single-task")
	if !sub.SubLoaded {
		t.Fatalf("expected sub-workflow body lazy-loaded")
	}
	if len(sub.Children) != len(fixtureImplementTask().Steps) {
		t.Errorf("want %d sub-workflow children (from fixture), got %d",
			len(fixtureImplementTask().Steps), len(sub.Children))
	}
	// ensureSubWorkflowLoaded set the absolute path before applySubWorkflowStart
	// ran, so the event's workflow_path does not clobber it.
	if !filepath.IsAbs(sub.StaticWorkflowPath) ||
		!strings.HasSuffix(sub.StaticWorkflowPath, "workflows/core/implement-task.yaml") {
		t.Errorf("workflow path: got %q", sub.StaticWorkflowPath)
	}
	if sub.InterpolatedParams["task_file"] != "tasks/01.md" {
		t.Errorf("interpolated params not extracted: %v", sub.InterpolatedParams)
	}

	// Next step_start inside the sub-workflow finds the seeded child.
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks:0, implement-single-task, sub:implement-task, implement]",
		Type:   "step_start",
		Data: map[string]any{
			"prompt":              "go implement",
			"mode":                "interactive",
			"resolved_session_id": "sess-123",
			"cli":                 "claude",
			"model":               "claude-sonnet",
		},
	})
	impl := childByID(sub, "implement")
	if impl == nil {
		t.Fatalf("missing implement child after sub-workflow load")
	}
	if impl.Status != StatusInProgress {
		t.Errorf("status: got %v", impl.Status)
	}
	if impl.SessionID != "sess-123" || impl.AgentCLI != "claude" {
		t.Errorf("agent fields not set: session=%q cli=%q", impl.SessionID, impl.AgentCLI)
	}
	if impl.Type != NodeInteractiveAgent {
		t.Errorf("mode should classify to interactive agent, got %v", impl.Type)
	}
}

// TestApplyEvent_NestedSubWorkflowUnderLoop covers the case where an outer
// sub-workflow is itself a child of the top-level workflow and contains a loop
// whose body is a sub-workflow. The inner body must load so the TUI can show
// its steps — the path walk used to fail because applySubWorkflowStart
// clobbered the outer node's absolute StaticWorkflowPath with the relative
// path from the event, corrupting the parent-dir resolution for descendants.
func TestApplyEvent_NestedSubWorkflowUnderLoop(t *testing.T) {
	// Top-level workflow: one sub-workflow step "implement" that points at
	// implement-change.yaml.
	wf := fixtureChange()
	tree := BuildTree(&wf, fixturePath("openspec/change.yaml"))
	tree.SubWorkflowLoader = fixtureSubLoader()

	// Outer sub-workflow start (implement → implement-change.yaml). Audit
	// events carry whatever path the executor used, which in practice is
	// relative to the invocation CWD.
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change]",
		Type:   "sub_workflow_start",
		Data: map[string]any{
			"workflow_name": "implement-change",
			"workflow_path": "workflows/openspec/implement-change.yaml",
		},
	})
	// Loop step_start pre-creates iteration placeholders.
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change, implement-tasks]",
		Type:   "step_start",
		Data: map[string]any{
			"loop_type":        "for-each",
			"resolved_matches": []any{"tasks/01.md"},
		},
	})
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change, implement-tasks:0]",
		Type:   "iteration_start",
		Data: map[string]any{
			"iteration": float64(0),
			"loop_var":  map[string]any{"task_file": "tasks/01.md"},
		},
	})
	// Inner sub-workflow start (implement-single-task → ../core/implement-task.yaml).
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change, implement-tasks:0, implement-single-task, sub:implement-task]",
		Type:   "sub_workflow_start",
		Data: map[string]any{
			"workflow_name": "implement-task",
			"workflow_path": "workflows/core/implement-task.yaml",
		},
	})

	implement := childByID(tree.Root, "implement")
	if implement == nil || !implement.SubLoaded {
		t.Fatalf("outer sub-workflow not loaded: %+v", implement)
	}
	loop := childByID(implement, "implement-tasks")
	iter0 := findIteration(loop, 0)
	if iter0 == nil || iter0.FlattenTarget == nil {
		t.Fatalf("iter0 missing or no flatten target")
	}
	inner := iter0.FlattenTarget
	if inner.Type != NodeSubWorkflow || inner.ID != "implement-single-task" {
		t.Fatalf("flatten target wrong: id=%q type=%v", inner.ID, inner.Type)
	}
	if !inner.SubLoaded {
		t.Errorf("inner sub-workflow body should be loaded; err=%q", inner.ErrorMessage)
	}
	if len(inner.Children) == 0 {
		t.Errorf("inner sub-workflow has no children (bug: 'No steps to display')")
	}
}

func TestApplyEvent_NestedSubWorkflowUnderLoop_BuiltinPaths(t *testing.T) {
	wf := fixtureChange()
	tree := BuildTree(&wf, "builtin:spec-driven/change.yaml")
	// Use a loader that requires the builtin: prefix (like the real loader),
	// unlike fixtureSubLoader which matches by basename only.
	tree.SubWorkflowLoader = func(path string) (model.Workflow, error) {
		if !strings.HasPrefix(path, "builtin:") {
			return model.Workflow{}, fmt.Errorf("expected builtin: prefix, got %q", path)
		}
		base := filepath.Base(path)
		switch base {
		case "implement-change.yaml":
			return fixtureImplementChange(), nil
		case "implement-task.yaml":
			return fixtureImplementTask(), nil
		case "plan-change.yaml":
			return fixturePlanChange(), nil
		}
		return model.Workflow{}, fmt.Errorf("no fixture for %s", path)
	}

	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change]",
		Type:   "sub_workflow_start",
		Data: map[string]any{
			"workflow_name": "implement-change",
			"workflow_path": "builtin:spec-driven/implement-change.yaml",
		},
	})
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change, implement-tasks]",
		Type:   "step_start",
		Data: map[string]any{
			"loop_type":        "for-each",
			"resolved_matches": []any{"tasks/01.md"},
		},
	})
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change, implement-tasks:0]",
		Type:   "iteration_start",
		Data: map[string]any{
			"iteration": float64(0),
			"loop_var":  map[string]any{"task_file": "tasks/01.md"},
		},
	})
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement, sub:implement-change, implement-tasks:0, implement-single-task, sub:implement-task]",
		Type:   "sub_workflow_start",
		Data: map[string]any{
			"workflow_name": "implement-task",
			"workflow_path": "builtin:core/implement-task.yaml",
		},
	})

	implement := childByID(tree.Root, "implement")
	if implement == nil || !implement.SubLoaded {
		t.Fatalf("outer sub-workflow not loaded: %+v", implement)
	}
	loop := childByID(implement, "implement-tasks")
	iter0 := findIteration(loop, 0)
	if iter0 == nil || iter0.FlattenTarget == nil {
		t.Fatalf("iter0 missing or no flatten target")
	}
	inner := iter0.FlattenTarget
	if !inner.SubLoaded {
		t.Errorf("inner sub-workflow body should be loaded; err=%q", inner.ErrorMessage)
	}
	if len(inner.Children) == 0 {
		t.Errorf("inner sub-workflow has no children (bug: 'No steps to display')")
	}
}

func TestApplyEvent_ErrorRootScoped(t *testing.T) {
	tree := buildImplementChangeTree(t)
	tree.ApplyEvent(RawEvent{
		Prefix: "",
		Type:   "error",
		Data:   map[string]any{"message": "boom"},
	})
	if tree.Root.ErrorMessage != "boom" {
		t.Errorf("root error not set: %q", tree.Root.ErrorMessage)
	}
}

func TestApplyEvent_ErrorOnStep(t *testing.T) {
	tree := buildImplementChangeTree(t)
	tree.ApplyEvent(RawEvent{
		Prefix: "[archive]",
		Type:   "step_end",
		Data:   map[string]any{"outcome": "failed", "error": "bad command"},
	})
	arch := childByID(tree.Root, "archive")
	if arch.ErrorMessage != "bad command" {
		t.Errorf("step error not set: %q", arch.ErrorMessage)
	}
}

func TestTailer_PartialLineBuffering(t *testing.T) {
	tree := buildImplementChangeTree(t)
	var tl Tailer

	// Split a line mid-JSON across two writes.
	part1 := `2024-01-01T00:00:00Z [archive] step_start {"command":"ech`
	part2 := `o hi"}` + "\n"

	var seen []RawEvent
	n, err := tl.Apply(strings.NewReader(part1), func(e RawEvent) { seen = append(seen, e) })
	if err != nil {
		t.Fatalf("apply1: %v", err)
	}
	if n != len(part1) {
		t.Errorf("consumed %d, want %d", n, len(part1))
	}
	if len(seen) != 0 {
		t.Errorf("no events should be emitted before newline, got %d", len(seen))
	}

	n, err = tl.Apply(strings.NewReader(part2), func(e RawEvent) { seen = append(seen, e) })
	if err != nil {
		t.Fatalf("apply2: %v", err)
	}
	if n != len(part2) {
		t.Errorf("consumed %d, want %d", n, len(part2))
	}
	if len(seen) != 1 {
		t.Fatalf("expected 1 event after full line, got %d", len(seen))
	}
	if seen[0].Data["command"] != "echo hi" {
		t.Errorf("event data reconstructed wrong: %v", seen[0].Data)
	}

	tree.ApplyEvent(seen[0])
	if childByID(tree.Root, "archive").InterpolatedCommand != "echo hi" {
		t.Errorf("tree not updated")
	}
}

func TestTailer_EmptyReaderNoOp(t *testing.T) {
	var tl Tailer
	n, err := tl.Apply(bytes.NewReader(nil), func(RawEvent) {})
	if err != nil || n != 0 {
		t.Errorf("empty reader should be no-op: n=%d err=%v", n, err)
	}
}

func TestFileTailer_ReadSince(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	var ft FileTailer

	// Missing file → (nil, nil).
	ev, err := ft.ReadSince(dir)
	if err != nil || ev != nil {
		t.Fatalf("missing log should return nil,nil; got %v %v", ev, err)
	}

	// Write first event.
	if err := os.WriteFile(logPath,
		[]byte(`2024-01-01T00:00:00Z run_start {}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev, err = ft.ReadSince(dir)
	if err != nil {
		t.Fatalf("read1: %v", err)
	}
	if len(ev) != 1 || ev[0].Type != "run_start" {
		t.Fatalf("want [run_start], got %v", ev)
	}
	off1 := ft.Offset()

	// Append a second event and a partial third.
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`2024-01-01T00:00:01Z [archive] step_start {"command":"echo hi"}` + "\n")
	_, _ = f.WriteString(`2024-01-01T00:00:02Z [archive] step_end `)
	_ = f.Close()

	ev, err = ft.ReadSince(dir)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if len(ev) != 1 {
		t.Fatalf("want 1 full line (partial line buffered), got %d", len(ev))
	}
	if ev[0].Type != "step_start" {
		t.Errorf("want step_start, got %q", ev[0].Type)
	}

	// Complete the partial line.
	f, _ = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"outcome":"success"}` + "\n")
	_ = f.Close()

	ev, err = ft.ReadSince(dir)
	if err != nil {
		t.Fatalf("read3: %v", err)
	}
	if len(ev) != 1 {
		t.Fatalf("want 1 event (the completed step_end), got %d", len(ev))
	}
	if ev[0].Type != "step_end" || ev[0].Data["outcome"] != "success" {
		t.Errorf("completed event wrong: %+v", ev[0])
	}
	if ft.Offset() <= off1 {
		t.Errorf("offset should have advanced")
	}

	// Calling again with no new bytes returns empty.
	ev, err = ft.ReadSince(dir)
	if err != nil || len(ev) != 0 {
		t.Errorf("no-op read: want []/nil; got %v %v", ev, err)
	}
}

func TestApplyEvent_PendingUntilEvents(t *testing.T) {
	tree := buildImplementChangeTree(t)
	// Without any events, every root child is pending.
	for _, c := range tree.Root.Children {
		if c.Status != StatusPending {
			t.Errorf("child %q should be pending, got %v", c.ID, c.Status)
		}
	}
}

func TestApplyEvent_IterationStart_ClearsAborted(t *testing.T) {
	tree := buildImplementChangeTree(t)

	// Bootstrap loop.
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks]",
		Type:   "step_start",
		Data: map[string]any{
			"loop_type":        "for-each",
			"glob_pattern":     "tasks/*.md",
			"resolved_matches": []any{"tasks/01.md"},
		},
	})

	// Abort first iteration.
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks:0]",
		Type:   "iteration_start",
		Data:   map[string]any{"iteration": float64(0), "loop_var": map[string]any{"task_file": "tasks/01.md"}},
	})
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks:0]",
		Type:   "iteration_end",
		Data:   map[string]any{"outcome": "aborted"},
	})

	iter0 := childByID(tree.Root, "implement-tasks").Children[0]
	if !iter0.Aborted {
		t.Fatal("iteration should be aborted after aborted outcome")
	}

	// Restart the same iteration — Aborted must be cleared.
	tree.ApplyEvent(RawEvent{
		Prefix: "[implement-tasks:0]",
		Type:   "iteration_start",
		Data:   map[string]any{"iteration": float64(0), "loop_var": map[string]any{"task_file": "tasks/01.md"}},
	})
	if iter0.Aborted {
		t.Error("iteration_start should clear Aborted flag from prior run")
	}
	if iter0.Status != StatusInProgress {
		t.Errorf("status after restart: want in-progress, got %v", iter0.Status)
	}
}
