package runview

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
)

// TestHistoricalProjectionINT001 exercises the persisted inspection boundary:
// workflow resolution, state, audit replay, metrics, output files, call
// filtering, skipped context, copy construction, and detail-first entry.
func TestHistoricalProjectionINT001(t *testing.T) {
	base := t.TempDir()
	workflowDir := filepath.Join(base, "workflows")
	workflowPath := filepath.Join(workflowDir, "history-v1.0.yaml")
	childPath := filepath.Join(workflowDir, "child-v1.0.yaml")
	writeFile(t, workflowPath, `name: history
steps:
  - id: parent
    prompt: parent prompt
    agent: planner
    mode: autonomous
    cli: claude
  - id: nested
    workflow: child-v1.0.yaml
  - id: skipped
    command: echo skipped
    skip_if: previous_success
  - id: selected
    script: echo selected
`)
	writeFile(t, childPath, `name: child
steps:
  - id: inner
    command: echo inner
`)

	projectDir := filepath.Join(base, "project")
	sessionDir := filepath.Join(projectDir, "runs", "history-2026-08-03T00-00-00-000000000Z")
	state, err := json.Marshal(model.RunState{
		WorkflowFile: workflowPath,
		WorkflowName: "history",
		WorkflowHash: "fixture-hash",
		Completed:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sessionDir, "state.json"), string(state))
	writeFile(t, filepath.Join(sessionDir, "audit.log"), strings.Join([]string{
		`2026-08-03T00:00:00Z run_start {"workflow_hash":"fixture-hash"}`,
		`2026-08-03T00:00:01Z [parent] step_start {"mode":"autonomous","prompt":"parent prompt","cli":"claude"}`,
		`2026-08-03T00:00:02Z [parent, call:review] agent_call_start {"call_id":"review","target_kind":"agent","target_name":"reviewer","cli":"claude"}`,
		`2026-08-03T00:00:03Z [parent, call:review] agent_call_end {"call_id":"review","outcome":"success","duration_ms":10,"cli_launched":true}`,
		`2026-08-03T00:00:04Z [parent] step_end {"outcome":"success","duration_ms":20,"identity":{"attempt":1,"agent_invoked":true},"usage":{"status":"collected","tokens":{"input":1}}}`,
		`2026-08-03T00:00:05Z [nested] step_start {}`,
		`2026-08-03T00:00:06Z [nested] sub_workflow_start {"workflow_path":"` + childPath + `"}`,
		`2026-08-03T00:00:07Z [nested, sub:child, inner] step_start {"command":"echo inner"}`,
		`2026-08-03T00:00:08Z [nested, sub:child, inner] step_end {"outcome":"success","stdout":"inner\n"}`,
		`2026-08-03T00:00:09Z [nested] sub_workflow_end {"outcome":"success"}`,
		`2026-08-03T00:00:10Z [skipped] step_end {"outcome":"skipped","skip_if":"previous_success"}`,
		`2026-08-03T00:00:11Z [selected] step_start {}`,
		`2026-08-03T00:00:12Z [selected] step_end {"outcome":"success"}`,
		`2026-08-03T00:00:13Z run_end {"outcome":"success","totals":{"active_duration_ms":30,"tokens":{},"usage_coverage":"complete"}}`,
	}, "\n")+"\n")

	callPrefix := sanitizeOutputPrefixForTest("[parent, call:review]")
	writeFile(t, filepath.Join(sessionDir, "output", callPrefix+".out"), "{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"filtered call response\"}\n")
	selectedPrefix := sanitizeOutputPrefixForTest("[selected]")
	writeFile(t, filepath.Join(sessionDir, "output", selectedPrefix+".out"), strings.Repeat("selected output\n", maxOutputLines+10))

	m, err := New(sessionDir, projectDir, FromInspect)
	if err != nil {
		t.Fatal(err)
	}
	if m.showSummary {
		t.Fatal("completed metrics run opened summary instead of detail")
	}
	// New replays through FileTailer. Compare it with a direct ordered replay
	// of the same serialized audit artifact so start ordinals are deterministic
	// across the two production replay paths.
	wf, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	direct := BuildTree(&wf, workflowPath)
	events, err := (&FileTailer{}).ReadSince(sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		direct.ApplyEvent(event)
	}
	if got, want := direct.FindByPrefix("[selected]").StartOrdinal, m.tree.FindByPrefix("[selected]").StartOrdinal; got != want {
		t.Fatalf("direct replay selected ordinal = %d, New replay ordinal = %d", got, want)
	}
	selected := childByID(m.tree.Root, "selected")
	if selected == nil {
		t.Fatalf("selected step missing: loadErr=%q children=%#v", m.loadErr, m.tree.Root.Children)
	}
	m.setSelected(selected)
	detail := m.selectedStepDetailText()
	for _, want := range []string{"Previous: skipped", "skip_if: previous_success", "Current script", "Current output", "selected output"} {
		if !strings.Contains(detail, want) {
			t.Errorf("selected historical detail missing %q (detail bytes=%d, selected output bytes=%d, loadedFull=%v)", want, len(detail), len(selected.Stdout), m.loadedFull[selected.NodeKey()])
		}
	}
	if strings.Count(selected.Stdout, "\n") > maxOutputLines {
		t.Fatalf("large historical output bypassed bounded read: %d lines", strings.Count(selected.Stdout, "\n"))
	}

	inner := m.tree.FindByPrefix("[nested, sub:child, inner]")
	if inner == nil {
		t.Fatalf("nested step missing: loadErr=%q tree=%#v", m.loadErr, m.tree.Root.Children)
	}
	m.setSelected(inner)
	copied := m.selectedStepDetailText()
	for _, want := range []string{"directory:", "breadcrumb:", "Previous: call agent: reviewer", "filtered call response", "Current command", "echo inner"} {
		if !strings.Contains(copied, want) {
			t.Errorf("copied nested detail missing %q:\n%s", want, copied)
		}
	}
	if strings.Contains(copied, `"type":"result"`) {
		t.Fatalf("copy retained raw call evidence instead of filtered response:\n%s", copied)
	}
}
