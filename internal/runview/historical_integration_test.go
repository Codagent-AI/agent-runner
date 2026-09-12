package runview

import (
	"encoding/json"
	"path/filepath"
	"slices"
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

// writeRepairRun materializes a run directory for a recorded repair fixture and
// opens it exactly as the run view would for a historical run.
func writeRepairRun(t *testing.T, workflowName, workflowYAML string, auditLines []string, state *model.RunState) *Model {
	t.Helper()
	base := t.TempDir()
	workflowPath := filepath.Join(base, "workflows", workflowName+"-v1.0.yaml")
	writeFile(t, workflowPath, workflowYAML)

	projectDir := filepath.Join(base, "project")
	sessionDir := filepath.Join(projectDir, "runs", "repair-2026-09-01T00-00-00-000000000Z")
	state.WorkflowFile = workflowPath
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sessionDir, "state.json"), string(encoded))
	writeFile(t, filepath.Join(sessionDir, "audit.log"), strings.Join(auditLines, "\n")+"\n")

	m, err := New(sessionDir, projectDir, FromInspect)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const repairPlanWorkflowYAML = `name: plan-change
steps:
  - id: write-plan
    prompt: write the plan
    agent: planner
    mode: autonomous
  - id: check-plan
    command: validate-plan
    repair:
      prompt: fix the plan
      session: resume
      max: 1
`

const repairPRWorkflowYAML = `name: implement-change
steps:
  - id: open-draft-pr
    prompt: open the draft pr
    agent: implementor
    mode: autonomous
  - id: verify-draft-pr
    command: gh pr view --json isDraft
    repair:
      rerun: open-draft-pr
      max: 1
  - id: finalize
    command: echo done
`

// TestRepairProjectionINT005 projects recorded repair audit into the sidebar
// and the selected detail for the check, an attempt row, and a repair agent
// row, for a run whose workflow is read back off disk.
func TestRepairProjectionINT005(t *testing.T) {
	m := writeRepairRun(t, "plan-change", repairPlanWorkflowYAML, repairedInlineAuditFixture(), &model.RunState{
		WorkflowName: "plan-change", Completed: true,
	})

	check := childByID(m.tree.Root, "check-plan")
	if check == nil {
		t.Fatalf("check-plan missing: loadErr=%q", m.loadErr)
	}
	m.setSelected(check)

	sidebar := strings.Join(stripANSISlice(m.buildStepRows(m.tree.Root.Children)), "\n")
	for _, want := range []string{"check-plan (repaired 1/1)", "⟳  attempt 1", "repair 1"} {
		if !strings.Contains(sidebar, want) {
			t.Errorf("sidebar missing %q:\n%s", want, sidebar)
		}
	}

	attempt := check.Children[0]
	m.setSelected(attempt)
	attemptDetail := stripANSI(strings.Join(m.selectedDetailDocument(80).renderScreen(), "\n"))
	for _, want := range []string{"attempt 1", "repair attempt", "exit: 1", "plan is missing a tasks section"} {
		if !strings.Contains(attemptDetail, want) {
			t.Errorf("attempt detail missing %q:\n%s", want, attemptDetail)
		}
	}

	repair := check.Children[1]
	m.setSelected(repair)
	repairDetail := stripANSI(strings.Join(m.selectedDetailDocument(80).renderScreen(), "\n"))
	for _, want := range []string{"repair 1", "Current prompt", "fix the plan", "Current response", "added the tasks section"} {
		if !strings.Contains(repairDetail, want) {
			t.Errorf("repair agent detail missing %q:\n%s", want, repairDetail)
		}
	}

	legend := stripANSI(m.renderLegend())
	if !strings.Contains(legend, "⟳  repair attempt") {
		t.Errorf("legend missing the repair-attempt glyph:\n%s", legend)
	}
}

// TestRepairProjectionINT005BlockedRerunEvidence covers the blocked rerun and
// the exhausted inline budget through the same persisted boundary.
func TestRepairProjectionINT005BlockedRerunEvidence(t *testing.T) {
	reason := "verify-draft-pr failed: no draft pull request found; blocked: push rejected: token lacks workflow scope"
	m := writeRepairRun(t, "implement-change", repairPRWorkflowYAML, blockedRerunAuditFixture(), &model.RunState{
		WorkflowName: "implement-change", FailureReason: reason,
	})

	check := childByID(m.tree.Root, "verify-draft-pr")
	m.setSelected(check)
	doc := m.selectedDetailDocument(100)
	copyText := stripANSI(doc.renderCopy())
	for _, want := range []string{
		"repair: rerun open-draft-pr · 0 of 1 used · blocked",
		"Failure evidence",
		reason,
		"guarded response [open-draft-pr]",
		"push rejected: token lacks workflow scope",
	} {
		if !strings.Contains(copyText, want) {
			t.Errorf("blocked check detail missing %q:\n%s", want, copyText)
		}
	}
	if _, suffix := stepRowLabel(check); suffix != " (blocked)" {
		t.Errorf("blocked suffix = %q", suffix)
	}
	if got := m.renderFailureReason(); !strings.Contains(stripANSI(got), reason) {
		t.Errorf("run header reason = %q", stripANSI(got))
	}

	exhausted := writeRepairRun(t, "plan-change", repairPlanWorkflowYAML, exhaustedInlineAuditFixture(), &model.RunState{
		WorkflowName: "plan-change",
	})
	exhaustedCheck := childByID(exhausted.tree.Root, "check-plan")
	if _, suffix := stepRowLabel(exhaustedCheck); suffix != " (2/2)" {
		t.Errorf("exhausted suffix = %q", suffix)
	}
	if len(exhaustedCheck.Children) != 4 {
		t.Errorf("exhausted check children = %d, want two attempts and two repair agents", len(exhaustedCheck.Children))
	}
}

// TestRepairProjectionINT005PreChangeAuditRendersUnchanged pins the rendering
// of an audit log recorded before repair existed.
func TestRepairProjectionINT005PreChangeAuditRendersUnchanged(t *testing.T) {
	m := writeRepairRun(t, "plan-change", repairPlanWorkflowYAML, preRepairAuditFixture(), &model.RunState{
		WorkflowName: "plan-change",
	})

	check := childByID(m.tree.Root, "check-plan")
	m.setSelected(check)

	wantSidebar := []string{
		"   ⚙  write-plan  ✓",
		"▶  $  check-plan  ✗",
	}
	got := stripANSISlice(m.buildStepRows(m.tree.Root.Children))
	if !slices.Equal(got, wantSidebar) {
		t.Fatalf("pre-change sidebar changed:\ngot  %#v\nwant %#v", got, wantSidebar)
	}

	wantDetail := strings.Join([]string{
		"check-plan · shell · failed",
		"exit: 1",
		"",
		"▎ Previous: write-plan",
		"▎ agent · success",
		"▎ plan written",
		"",
		"▎ Current command",
		"▎ validate-plan",
		"",
		"▎ Current output",
		"▎ stderr:",
		"▎ plan is missing a tasks section",
	}, "\n")
	if gotDetail := stripANSI(strings.Join(m.selectedDetailDocument(80).renderScreen(), "\n")); gotDetail != wantDetail {
		t.Fatalf("pre-change detail changed:\ngot:\n%s\nwant:\n%s", gotDetail, wantDetail)
	}
}

func stripANSISlice(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = stripANSI(row)
	}
	return out
}
