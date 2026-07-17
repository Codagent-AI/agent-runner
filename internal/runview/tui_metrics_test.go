package runview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func TestApplyEventAppendsAttemptMetricsAndRunTotals(t *testing.T) {
	wf := model.Workflow{Name: "metrics", Steps: []model.Step{{ID: "agent", Prompt: "do it", Mode: model.ModeAutonomous}}}
	tree := BuildTree(&wf, "")

	tree.ApplyEvent(stepEndMetricsEvent("agent", 1, 1200, "failed", collectedUsageRecord(10, 2), float64Pointer(0.25)))
	tree.ApplyEvent(stepEndMetricsEvent("agent", 2, 800, "success", collectedUsageRecord(7, 3), float64Pointer(0.15)))
	tree.ApplyEvent(RawEvent{Type: "run_end", Data: map[string]any{
		"outcome": "success",
		"totals": map[string]any{
			"active_duration_ms":     float64(5000),
			"tokens":                 map[string]any{"input": float64(17), "output": float64(5)},
			"usage_coverage":         "complete",
			"estimated_api_cost_usd": float64(0.40),
			"cost_coverage":          "complete",
		},
	}})

	node := tree.Root.Children[0]
	wantAttempts := []AttemptMetrics{
		{Attempt: 1, Usage: collectedUsageRecord(10, 2), CostUSD: float64Pointer(0.25), DurationMs: int64Pointer(1200), Outcome: "failed"},
		{Attempt: 2, Usage: collectedUsageRecord(7, 3), CostUSD: float64Pointer(0.15), DurationMs: int64Pointer(800), Outcome: "success"},
	}
	if diff := cmp.Diff(wantAttempts, node.Attempts); diff != "" {
		t.Fatalf("attempts mismatch (-want +got):\n%s", diff)
	}
	if node.DurationMs == nil || *node.DurationMs != 800 {
		t.Fatalf("latest duration = %v, want 800", node.DurationMs)
	}
	wantTotals := &model.RunTotals{
		ActiveDurationMS:    5000,
		Tokens:              model.TokenCounts{model.TokenInput: 17, model.TokenOutput: 5},
		UsageCoverage:       model.CoverageComplete,
		EstimatedAPICostUSD: float64Pointer(0.40),
		CostCoverage:        model.CoverageComplete,
	}
	if diff := cmp.Diff(wantTotals, tree.RunTotals); diff != "" {
		t.Fatalf("run totals mismatch (-want +got):\n%s", diff)
	}
	tree.ApplyEvent(RawEvent{Type: "run_start", Data: map[string]any{}})
	if tree.RunTotals != nil {
		t.Fatalf("resumed active run retained stale terminal totals: %+v", tree.RunTotals)
	}
}

func TestAgentDetailMetricsRenderCollectedUnavailableAndLatestAttempt(t *testing.T) {
	tests := []struct {
		name     string
		attempts []AttemptMetrics
		want     []string
		dontWant []string
	}{
		{
			name:     "collected usage and cost",
			attempts: []AttemptMetrics{{Attempt: 1, Usage: collectedUsageRecord(1234, 56), CostUSD: float64Pointer(0.42), DurationMs: int64Pointer(1500), Outcome: "success"}},
			want:     []string{"duration: 1.5s", "tokens: input 1,234", "output 56", "cost: $0.42"},
		},
		{
			name:     "unavailable usage and cost",
			attempts: []AttemptMetrics{{Attempt: 1, Usage: &model.UsageRecord{Status: model.UsageUnavailable, Reason: model.UnavailablePTYContext, CLI: "claude", Source: "agent-runner"}, DurationMs: int64Pointer(1000), Outcome: "success"}},
			want:     []string{"usage: unavailable (pty-context)", "cost: unavailable"},
			dontWant: []string{"$0.00", "input 0", "output 0"},
		},
		{
			name: "latest repeated attempt",
			attempts: []AttemptMetrics{
				{Attempt: 1, Usage: collectedUsageRecord(100, 10), CostUSD: float64Pointer(1.00), DurationMs: int64Pointer(3000), Outcome: "failed"},
				{Attempt: 2, Usage: collectedUsageRecord(20, 4), CostUSD: float64Pointer(0.20), DurationMs: int64Pointer(500), Outcome: "success"},
			},
			want:     []string{"attempt: 2", "duration: 500ms", "input 20", "output 4", "cost: $0.20"},
			dontWant: []string{"input 100", "$1.00", "duration: 3.0s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &StepNode{ID: "agent", Type: NodeHeadlessAgent, Status: StatusSuccess, Attempts: tt.attempts}
			latest := tt.attempts[len(tt.attempts)-1]
			node.DurationMs = latest.DurationMs
			plain := tuistyle.Sanitize(strings.Join(renderHeadlessBlock(node, 0, 100, true, 0, false), "\n"))
			for _, want := range tt.want {
				if !strings.Contains(plain, want) {
					t.Errorf("detail missing %q:\n%s", want, plain)
				}
			}
			for _, unwanted := range tt.dontWant {
				if strings.Contains(plain, unwanted) {
					t.Errorf("detail unexpectedly contains %q:\n%s", unwanted, plain)
				}
			}
		})
	}
}

func TestInteractiveAgentDetailRendersUsageAndCost(t *testing.T) {
	usage := collectedUsageRecord(80, 12)
	node := &StepNode{
		ID: "interactive", Type: NodeInteractiveAgent, Status: StatusSuccess, DurationMs: int64Pointer(900),
		Attempts: []AttemptMetrics{{Attempt: 1, Usage: usage, CostUSD: float64Pointer(0.08), DurationMs: int64Pointer(900), Outcome: "success"}},
	}
	plain := tuistyle.Sanitize(strings.Join(renderInteractiveBlock(node, 0, 100, 0, false), "\n"))
	for _, want := range []string{"duration: 900ms", "tokens: input 80", "output 12", "cost: $0.08"} {
		if !strings.Contains(plain, want) {
			t.Errorf("interactive detail missing %q:\n%s", want, plain)
		}
	}
}

func TestRenderSummaryAggregatesAttemptsNestedContainersAndCoverage(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	repeated := summaryLeaf("retry", root, 1000, float64Pointer(0.20), collectedUsageRecord(10, 2))
	repeated.Attempts = append(repeated.Attempts,
		AttemptMetrics{Attempt: 2, Usage: collectedUsageRecord(5, 1), CostUSD: float64Pointer(0.10), DurationMs: int64Pointer(500), Outcome: "success"})

	loop := &StepNode{ID: "loop", Type: NodeLoop, Status: StatusSuccess, Parent: root}
	iter1 := &StepNode{ID: "loop", Type: NodeIteration, Status: StatusSuccess, Parent: loop, IterationIndex: 0}
	iter2 := &StepNode{ID: "loop", Type: NodeIteration, Status: StatusSuccess, Parent: loop, IterationIndex: 1}
	iter1.Children = []*StepNode{summaryLeaf("loop-priced", iter1, 2000, float64Pointer(0.40), collectedUsageRecord(20, 4))}
	iter2.Children = []*StepNode{summaryLeaf("loop-unpriced", iter2, 3000, nil, unavailableUsageRecord())}
	loop.Children = []*StepNode{iter1, iter2}

	sub := &StepNode{ID: "sub", Type: NodeSubWorkflow, Status: StatusSuccess, Parent: root, SubLoaded: true}
	sub.Children = []*StepNode{summaryLeaf("sub-child", sub, 750, float64Pointer(0.30), collectedUsageRecord(7, 2))}

	group := &StepNode{ID: "group", Type: NodeGroup, Status: StatusSuccess, Parent: root}
	group.Children = []*StepNode{summaryLeaf("group-child", group, 250, float64Pointer(0.05), collectedUsageRecord(3, 1))}
	pending := &StepNode{ID: "pending", Type: NodeShell, Status: StatusPending, Parent: root}
	root.Children = []*StepNode{repeated, loop, sub, group, pending}

	tree := &Tree{Root: root, RunTotals: &model.RunTotals{
		ActiveDurationMS:    7500,
		Tokens:              model.TokenCounts{model.TokenInput: 45, model.TokenOutput: 10},
		UsageCoverage:       model.CoveragePartial,
		EstimatedAPICostUSD: float64Pointer(1.05),
		CostCoverage:        model.CoveragePartial,
	}}
	m := newTestModel(tree, FromList)
	plain := tuistyle.Sanitize(m.renderSummary())

	for _, want := range []string{
		"Run summary",
		"retry", "1.5s", "$0.30",
		"loop", "5.0s", "$0.40 (partial)",
		"sub", "750ms", "$0.30",
		"group", "250ms", "$0.05",
		"pending", "—",
		"Total", "7.5s", "input 45", "output 10", "$1.05 (partial)", "usage partial",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("summary missing %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(plain, "  iter 1") || !strings.Contains(plain, "    loop-priced") ||
		!strings.Contains(plain, "  sub-child") || !strings.Contains(plain, "  group-child") {
		t.Errorf("nested rows are not indented:\n%s", plain)
	}
}

func TestRenderSummaryComputesMidRunTotalsFromAttempts(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	root.Children = []*StepNode{
		summaryLeaf("done", root, 1200, float64Pointer(0.25), collectedUsageRecord(12, 3)),
		{ID: "not-yet-run", Type: NodeHeadlessAgent, Status: StatusPending, Parent: root},
	}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	plain := tuistyle.Sanitize(m.renderSummary())
	for _, want := range []string{"done", "1.2s", "$0.25", "not-yet-run", "Total", "input 12", "output 3", "usage complete"} {
		if !strings.Contains(plain, want) {
			t.Errorf("mid-run summary missing %q:\n%s", want, plain)
		}
	}
}

func TestSummaryToggleWorksInEveryRunStateAndAtDrillDepth(t *testing.T) {
	statuses := []NodeStatus{StatusInProgress, StatusSuccess, StatusFailed}
	for _, status := range statuses {
		t.Run(statusLabel(status), func(t *testing.T) {
			root := &StepNode{ID: "workflow", Type: NodeRoot, Status: status}
			sub := &StepNode{ID: "sub", Type: NodeSubWorkflow, Status: status, Parent: root, SubLoaded: true}
			root.Children = []*StepNode{sub}
			m := newTestModel(&Tree{Root: root}, FromList)
			m.path = []*StepNode{root, sub}

			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
			if !m.showSummary {
				t.Fatal("s did not show summary")
			}
			if len(m.path) != 2 {
				t.Fatalf("toggle changed drill path: %d", len(m.path))
			}
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
			if m.showSummary {
				t.Fatal("second s did not restore detail")
			}
		})
	}
}

func TestSummaryCompletionDefaultsAndHelp(t *testing.T) {
	t.Run("successful live completion auto shows summary", func(t *testing.T) {
		m := newLiveModelWithFlags()
		m.Update(liverun.ExecDoneMsg{Result: "success"})
		if !m.showSummary {
			t.Fatal("successful completion did not show summary")
		}
	})

	t.Run("failed live completion keeps detail", func(t *testing.T) {
		m := newLiveModelWithFlags()
		m.showSummary = true
		m.Update(liverun.ExecDoneMsg{Result: "failed"})
		if m.showSummary {
			t.Fatal("failed completion unexpectedly showed summary")
		}
	})

	t.Run("help advertises summary", func(t *testing.T) {
		m := newTestModel(simpleTree(), FromList)
		if help := tuistyle.Sanitize(m.renderHelpBar()); !strings.Contains(help, "s summary") {
			t.Fatalf("help missing summary binding: %q", help)
		}
	})
}

func TestNewCompletedRunStartsInSummaryButFailedRunDoesNot(t *testing.T) {
	for _, tt := range []struct {
		name          string
		outcome       string
		completed     bool
		includeRunEnd bool
		wantSummary   bool
	}{
		{name: "completed inspect", completed: true, wantSummary: true},
		{name: "completed list", outcome: "success", completed: true, includeRunEnd: true, wantSummary: true},
		{name: "failed inspect overrides completed state", outcome: "failed", completed: true, includeRunEnd: true, wantSummary: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			workflowPath := filepath.Join(dir, "workflow.yaml")
			if err := os.WriteFile(workflowPath, []byte("name: workflow\nsteps:\n  - id: agent\n    agent: test-profile\n    prompt: test\n    mode: autonomous\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			state := model.RunState{WorkflowFile: workflowPath, WorkflowName: "workflow", Params: map[string]string{}, Completed: tt.completed}
			stateData, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state.json"), stateData, 0o644); err != nil {
				t.Fatal(err)
			}
			audit := "2026-07-17T10:00:00Z run_start {}\n"
			if tt.includeRunEnd {
				audit += "2026-07-17T10:00:01Z run_end {\"outcome\":\"" + tt.outcome + "\"}\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(audit), 0o644); err != nil {
				t.Fatal(err)
			}
			entered := FromInspect
			if strings.Contains(tt.name, "list") {
				entered = FromList
			}
			m, err := New(dir, dir, entered)
			if err != nil {
				t.Fatal(err)
			}
			if m.showSummary != tt.wantSummary {
				t.Fatalf("showSummary = %v, want %v", m.showSummary, tt.wantSummary)
			}
		})
	}
}

func TestInspectCompletedRunShowsAuditMetricsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte("name: workflow\nsteps:\n  - id: agent\n    agent: test-profile\n    prompt: test\n    mode: autonomous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateData, err := json.Marshal(model.RunState{
		WorkflowFile: workflowPath, WorkflowName: "workflow", Params: map[string]string{}, Completed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	stepEnd := stepEndMetricsEvent("agent", 1, 1500, "success", collectedUsageRecord(1234, 56), float64Pointer(0.42))
	stepEndData, err := json.Marshal(stepEnd.Data)
	if err != nil {
		t.Fatal(err)
	}
	runEndData, err := json.Marshal(map[string]any{
		"outcome": "success",
		"totals": map[string]any{
			"active_duration_ms": 1500, "tokens": map[string]any{"input": 1234, "output": 56},
			"usage_coverage": "complete", "estimated_api_cost_usd": 0.42, "cost_coverage": "complete",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	audit := strings.Join([]string{
		"2026-07-17T10:00:00Z run_start {}",
		"2026-07-17T10:00:01Z [agent] step_start {\"mode\":\"autonomous\"}",
		"2026-07-17T10:00:02Z [agent] step_end " + string(stepEndData),
		"2026-07-17T10:00:03Z run_end " + string(runEndData),
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(audit), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := New(dir, dir, FromInspect)
	if err != nil {
		t.Fatal(err)
	}
	m.termWidth = 120
	m.termHeight = 40
	summary := tuistyle.Sanitize(m.View())
	for _, want := range []string{"Run summary", "agent", "1.5s", "$0.42", "input 1,234", "output 56"} {
		if !strings.Contains(summary, want) {
			t.Errorf("inspect summary missing %q:\n%s", want, summary)
		}
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	detail := tuistyle.Sanitize(m.View())
	for _, want := range []string{"tokens: input 1,234", "output 56", "cost: $0.42"} {
		if !strings.Contains(detail, want) {
			t.Errorf("inspect detail missing %q:\n%s", want, detail)
		}
	}
}

func TestBuildTreeIncludesGroupMembers(t *testing.T) {
	wf := model.Workflow{Name: "workflow", Steps: []model.Step{{ID: "group", Steps: []model.Step{{ID: "child", Command: "true"}}}}}
	tree := BuildTree(&wf, "")
	group := tree.Root.Children[0]
	if group.Type != NodeGroup || len(group.Children) != 1 || group.Children[0].ID != "child" || group.Children[0].Parent != group {
		t.Fatalf("group tree = %#v, children = %#v", group, group.Children)
	}
}

func TestApplyEventResolvesCurrentAuditShapeForGroupMembers(t *testing.T) {
	wf := model.Workflow{Name: "workflow", Steps: []model.Step{
		{ID: "child", Command: "echo top-level"},
		{ID: "group", Steps: []model.Step{{ID: "child", Prompt: "test", Mode: model.ModeAutonomous}}},
	}}
	tree := BuildTree(&wf, "")
	tree.ApplyEvent(RawEvent{Prefix: "[group]", Type: "step_start", Data: map[string]any{}})
	tree.ApplyEvent(stepEndMetricsEvent("child", 1, 250, "success", collectedUsageRecord(4, 1), float64Pointer(0.05)))

	if topLevel := tree.Root.Children[0]; topLevel.Status != StatusPending || len(topLevel.Attempts) != 0 {
		t.Fatalf("event was attributed to duplicate top-level ID: %#v", topLevel)
	}
	child := tree.Root.Children[1].Children[0]
	if child.Status != StatusSuccess || len(child.Attempts) != 1 {
		t.Fatalf("group child was not updated from audit event: %#v", child)
	}
}

func stepEndMetricsEvent(id string, attempt int, duration int64, outcome string, usage *model.UsageRecord, cost *float64) RawEvent {
	return RawEvent{Prefix: "[" + id + "]", Type: "step_end", Data: map[string]any{
		"outcome":     outcome,
		"duration_ms": float64(duration),
		"identity":    map[string]any{"attempt": float64(attempt), "kind": "step", "step_type": "agent", "agent_invoked": true},
		"usage": map[string]any{
			"status": string(usage.Status), "reason": string(usage.Reason), "cli": usage.CLI, "provider": usage.Provider,
			"model": usage.Model, "tokens": tokenCountsAsAny(usage.Tokens), "source": usage.Source, "completeness": string(usage.Completeness),
		},
		"estimated_api_cost_usd": costValue(cost),
	}}
}

func summaryLeaf(id string, parent *StepNode, duration int64, cost *float64, usage *model.UsageRecord) *StepNode {
	return &StepNode{ID: id, Type: NodeHeadlessAgent, Status: StatusSuccess, Parent: parent, DurationMs: int64Pointer(duration), Attempts: []AttemptMetrics{{
		Attempt: 1, Usage: usage, CostUSD: cost, DurationMs: int64Pointer(duration), Outcome: "success",
	}}}
}

func collectedUsageRecord(input, output int64) *model.UsageRecord {
	return &model.UsageRecord{Status: model.UsageCollected, CLI: "claude", Tokens: model.TokenCounts{model.TokenInput: input, model.TokenOutput: output}, Source: "test", Completeness: model.CompletenessComplete}
}

func unavailableUsageRecord() *model.UsageRecord {
	return &model.UsageRecord{Status: model.UsageUnavailable, Reason: model.UnavailableNoUsageEvent, CLI: "codex", Source: "test"}
}

func float64Pointer(v float64) *float64 { return &v }
func int64Pointer(v int64) *int64       { return &v }

func tokenCountsAsAny(tokens model.TokenCounts) map[string]any {
	out := make(map[string]any, len(tokens))
	for category, count := range tokens {
		out[category] = float64(count)
	}
	return out
}

func costValue(cost *float64) any {
	if cost == nil {
		return nil
	}
	return *cost
}
