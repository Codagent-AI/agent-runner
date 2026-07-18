package runview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/mattn/go-runewidth"

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
		{Attempt: 1, Usage: collectedUsageRecord(10, 2), CostUSD: float64Pointer(0.25), DurationMs: int64Pointer(1200), Outcome: "failed", AgentInvoked: true},
		{Attempt: 2, Usage: collectedUsageRecord(7, 3), CostUSD: float64Pointer(0.15), DurationMs: int64Pointer(800), Outcome: "success", AgentInvoked: true},
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
		AttemptMetrics{Attempt: 2, Usage: collectedUsageRecord(5, 1), CostUSD: float64Pointer(0.10), DurationMs: int64Pointer(500), Outcome: "success", AgentInvoked: true})

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
		"Total", "7.5s", "45", "10", "$1.05 (partial)", "usage partial",
		"Processed tokens: unavailable (none)",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("summary missing %q:\n%s", want, plain)
		}
	}
	for _, nested := range []string{"iteration 1", "loop-priced", "sub-child", "group-child"} {
		if strings.Contains(plain, nested) {
			t.Errorf("root summary flattened nested row %q:\n%s", nested, plain)
		}
	}
}

func TestRenderSummaryAlignsDurationAndCostColumns(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	root.Children = []*StepNode{
		summaryLeaf("short", root, 1200, float64Pointer(0.25), collectedUsageRecord(12, 3)),
		summaryLeaf("a-much-longer-step", root, 12000, float64Pointer(12.34), collectedUsageRecord(20, 4)),
	}
	m := newTestModel(&Tree{Root: root, RunTotals: &model.RunTotals{
		ActiveDurationMS: 13200, Tokens: model.TokenCounts{model.TokenInput: 32, model.TokenOutput: 7},
		UsageCoverage: model.CoverageComplete, EstimatedAPICostUSD: float64Pointer(12.59), CostCoverage: model.CoverageComplete,
	}}, FromList)

	lines := strings.Split(tuistyle.Sanitize(m.renderSummary()), "\n")
	header := summaryLineContaining(t, lines, "Duration")
	short := summaryLineContaining(t, lines, "short")
	long := summaryLineContaining(t, lines, "a-much-longer-step")
	total := summaryLineContaining(t, lines, "Total")

	if !strings.Contains(header, "Step") || !strings.Contains(header, "Cost") {
		t.Fatalf("summary header = %q, want Step, Duration, and Cost columns", header)
	}
	assertSameRightEdge(t, header, "Duration", short, "1.2s")
	assertSameRightEdge(t, header, "Duration", long, "12.0s")
	assertSameRightEdge(t, header, "Duration", total, "13.2s")
	assertSameRightEdge(t, header, "Cost", short, "$0.25")
	assertSameRightEdge(t, header, "Cost", long, "$12.34")
	assertSameRightEdge(t, header, "Cost", total, "$12.59")
}

func TestRenderSummaryDistinguishesUnavailableFromNotApplicableCost(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	agent := summaryLeaf("interactive-agent", root, 1200, nil, unavailableUsageRecord())
	shell := &StepNode{ID: "shell", Type: NodeShell, Status: StatusSuccess, Parent: root,
		Attempts: []AttemptMetrics{{Attempt: 1, DurationMs: int64Pointer(25), Outcome: "success", AgentInvoked: false}}}
	root.Children = []*StepNode{agent, shell}

	lines := strings.Split(tuistyle.Sanitize(newTestModel(&Tree{Root: root}, FromList).renderSummary()), "\n")
	agentLine := summaryLineContaining(t, lines, "interactive-agent")
	shellLine := summaryLineContaining(t, lines, "shell")
	if !strings.Contains(agentLine, "unavailable") {
		t.Fatalf("agent row = %q, want explicit unavailable cost", agentLine)
	}
	if !strings.HasSuffix(strings.TrimSpace(shellLine), "—") {
		t.Fatalf("shell row = %q, want not-applicable em dash", shellLine)
	}
}

func TestRenderSummaryShowsAlignedTokenColumnsAndCanonicalTotals(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	firstUsage := &model.UsageRecord{
		Status: model.UsageCollected, CLI: "claude",
		Tokens: model.TokenCounts{
			model.TokenInput: 1200, model.TokenCachedInput: 800, model.TokenCacheWrite: 120,
			model.TokenOutput: 400, model.TokenReasoning: 95,
		},
		TokenTotals: &model.TokenTotals{Input: 2120, Output: 495, Total: 2615},
		Source:      "test", Completeness: model.CompletenessComplete,
	}
	secondUsage := &model.UsageRecord{
		Status: model.UsageCollected, CLI: "opencode",
		Tokens: model.TokenCounts{
			model.TokenInput: 30, model.TokenCachedInput: 20, model.TokenCacheWrite: 10,
			model.TokenOutput: 8, model.TokenReasoning: 2,
		},
		TokenTotals: &model.TokenTotals{Input: 60, Output: 10, Total: 70},
		Source:      "test", Completeness: model.CompletenessComplete,
	}
	root.Children = []*StepNode{
		summaryLeaf("plan", root, 32000, float64Pointer(0.42), firstUsage),
		summaryLeaf("verify", root, 8000, float64Pointer(0.08), secondUsage),
	}
	tree := &Tree{Root: root, RunTotals: &model.RunTotals{
		ActiveDurationMS: 40000,
		Tokens: model.TokenCounts{
			model.TokenInput: 1230, model.TokenCachedInput: 820, model.TokenCacheWrite: 130,
			model.TokenOutput: 408, model.TokenReasoning: 97,
		},
		UsageCoverage: model.CoverageComplete,
		TokenTotals:   &model.TokenTotals{Input: 2180, Output: 505, Total: 2685}, TokenTotalCoverage: model.CoverageComplete,
		EstimatedAPICostUSD: float64Pointer(0.50), CostCoverage: model.CoverageComplete,
	}}
	m := newTestModel(tree, FromList)
	m.termWidth = 180

	lines := strings.Split(tuistyle.Sanitize(m.renderSummary()), "\n")
	header := summaryLineContaining(t, lines, "Cache read")
	plan := summaryLineContaining(t, lines, "plan")
	total := summaryLineContaining(t, lines, "Total")
	for _, column := range []string{"Step", "Duration", "Input", "Cache read", "Cache write", "Output", "Reasoning", "Cost"} {
		if !strings.Contains(header, column) {
			t.Fatalf("summary header missing %q: %q", column, header)
		}
	}
	for headerValue, rowValue := range map[string]string{
		"Duration": "32.0s", "Input": "1,200", "Cache read": "800", "Cache write": "120",
		"Output": "400", "Reasoning": "95", "Cost": "$0.42",
	} {
		assertSameRightEdge(t, header, headerValue, plan, rowValue)
	}
	for headerValue, rowValue := range map[string]string{
		"Duration": "40.0s", "Input": "1,230", "Cache read": "820", "Cache write": "130",
		"Output": "408", "Reasoning": "97", "Cost": "$0.50",
	} {
		assertSameRightEdge(t, header, headerValue, total, rowValue)
	}
	processed := summaryLineContaining(t, lines, "Processed tokens")
	for _, want := range []string{"input 2,180", "output 505", "total 2,685", "complete"} {
		if !strings.Contains(processed, want) {
			t.Fatalf("processed-token line missing %q: %q", want, processed)
		}
	}
}

func TestSummaryReusesRunViewDrillPath(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	loop := &StepNode{ID: "verify-loop", Type: NodeLoop, Status: StatusSuccess, Parent: root}
	iteration := &StepNode{ID: "verify-loop", Type: NodeIteration, Status: StatusSuccess, Parent: loop, IterationIndex: 0}
	iteration.Children = []*StepNode{summaryLeaf("check", iteration, 1000, float64Pointer(0.10), collectedUsageRecord(5, 1))}
	loop.Children = []*StepNode{iteration}
	root.Children = []*StepNode{loop, summaryLeaf("after", root, 500, float64Pointer(0.05), collectedUsageRecord(2, 1))}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.showSummary = true
	rootSummary := tuistyle.Sanitize(m.renderSummary())
	if !strings.Contains(rootSummary, "verify-loop") || !strings.Contains(rootSummary, "after") || strings.Contains(rootSummary, "iteration 1") || strings.Contains(rootSummary, "check") {
		t.Fatalf("root summary must show only direct children:\n%s", rootSummary)
	}

	if _, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}); len(m.path) != 2 || !m.showSummary {
		t.Fatalf("summary Enter did not drill into loop: path=%d showSummary=%v", len(m.path), m.showSummary)
	}
	nestedSummary := tuistyle.Sanitize(m.renderSummary())
	if !strings.Contains(nestedSummary, "iteration 1") || strings.Contains(nestedSummary, "after") || strings.Contains(nestedSummary, "check") {
		t.Fatalf("nested summary must show only loop children:\n%s", nestedSummary)
	}

	if _, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}); len(m.path) != 1 || !m.showSummary {
		t.Fatalf("summary Escape did not drill out: path=%d showSummary=%v", len(m.path), m.showSummary)
	}
}

func summaryLineContaining(t *testing.T, lines []string, value string) string {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, value) {
			return line
		}
	}
	t.Fatalf("summary has no line containing %q:\n%s", value, strings.Join(lines, "\n"))
	return ""
}

func assertSameRightEdge(t *testing.T, header, headerValue, row, rowValue string) {
	t.Helper()
	if !strings.Contains(header, headerValue) || !strings.Contains(row, rowValue) {
		t.Fatalf("cannot compare %q in %q with %q in %q", headerValue, header, rowValue, row)
	}
	headerIndex := strings.Index(header, headerValue)
	rowIndex := strings.Index(row, rowValue)
	headerEnd := runewidth.StringWidth(header[:headerIndex]) + runewidth.StringWidth(headerValue)
	rowEnd := runewidth.StringWidth(row[:rowIndex]) + runewidth.StringWidth(rowValue)
	if rowEnd != headerEnd {
		t.Fatalf("%q right edge = %d, want column edge %d:\nheader: %q\nrow:    %q", rowValue, rowEnd, headerEnd, header, row)
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
	for _, want := range []string{"done", "1.2s", "$0.25", "not-yet-run", "Total", "12", "3", "usage complete"} {
		if !strings.Contains(plain, want) {
			t.Errorf("mid-run summary missing %q:\n%s", want, plain)
		}
	}
}

func TestApplyStepStartRecordsStartedAt(t *testing.T) {
	wf := model.Workflow{Name: "metrics", Steps: []model.Step{{ID: "agent", Prompt: "do it", Mode: model.ModeAutonomous}}}
	tree := BuildTree(&wf, "")
	tree.ApplyEvent(RawEvent{Timestamp: "2026-07-13T03:23:36Z", Prefix: "[agent]", Type: "step_start", Data: map[string]any{}})
	node := tree.Root.Children[0]
	want := time.Date(2026, 7, 13, 3, 23, 36, 0, time.UTC)
	if !node.StartedAt.Equal(want) {
		t.Fatalf("StartedAt = %v, want %v", node.StartedAt, want)
	}
}

func TestSummaryAddsInFlightElapsedForRunningStepWhenLive(t *testing.T) {
	start := time.Date(2026, 7, 13, 3, 23, 36, 0, time.UTC)
	now := start.Add(5 * time.Second)

	running := &StepNode{ID: "run", Type: NodeHeadlessAgent, Status: StatusInProgress, StartedAt: start}
	if got := aggregateSummaryMetrics(running, now); !got.durationReported || got.durationMS != 5000 {
		t.Fatalf("live running: durationReported=%v durationMS=%d, want true/5000", got.durationReported, got.durationMS)
	}

	// Not live (now zero): the in-flight step contributes no duration.
	if got := aggregateSummaryMetrics(running, time.Time{}); got.durationReported || got.durationMS != 0 {
		t.Fatalf("not live: durationReported=%v durationMS=%d, want false/0", got.durationReported, got.durationMS)
	}

	// Aborted mid-execution: excluded even while live.
	aborted := &StepNode{ID: "ab", Type: NodeHeadlessAgent, Status: StatusInProgress, Aborted: true, StartedAt: start}
	if got := aggregateSummaryMetrics(aborted, now); got.durationReported || got.durationMS != 0 {
		t.Fatalf("aborted: durationReported=%v durationMS=%d, want false/0", got.durationReported, got.durationMS)
	}

	// A prior failed attempt plus the current in-flight retry both count.
	retry := &StepNode{ID: "retry", Type: NodeHeadlessAgent, Status: StatusInProgress, StartedAt: start,
		Attempts: []AttemptMetrics{{Attempt: 1, DurationMs: int64Pointer(2000), Outcome: "failed"}}}
	if got := aggregateSummaryMetrics(retry, now); got.durationMS != 7000 {
		t.Fatalf("retry: durationMS=%d, want 7000", got.durationMS)
	}
}

func TestMidRunCoverageExcludesSkippedAgentStep(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	invoked := summaryLeaf("ran", root, 1200, float64Pointer(0.25), collectedUsageRecord(12, 3))
	skipped := &StepNode{ID: "skipped", Type: NodeHeadlessAgent, Status: StatusSkipped, Parent: root,
		Attempts: []AttemptMetrics{{Attempt: 1, Outcome: "skipped", AgentInvoked: false}}}
	root.Children = []*StepNode{invoked, skipped}

	totals := (&Model{tree: &Tree{Root: root}}).summaryRunTotals(time.Time{})
	if totals.UsageCoverage != model.CoverageComplete {
		t.Fatalf("usage coverage = %q, want complete (skipped step must not drag it to partial)", totals.UsageCoverage)
	}
	if totals.CostCoverage != model.CoverageComplete {
		t.Fatalf("cost coverage = %q, want complete", totals.CostCoverage)
	}
}

func TestSummaryScrollsRowsWhilePinningTotals(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	var children []*StepNode
	for i := 0; i < 30; i++ {
		children = append(children, summaryLeaf(fmt.Sprintf("step-%02d", i), root, 1000, float64Pointer(0.1), collectedUsageRecord(5, 1)))
	}
	root.Children = children
	m := newTestModel(&Tree{Root: root}, FromList)
	m.termHeight = 20
	m.showSummary = true

	// At offset 0 the top rows and the pinned totals line are visible; a late
	// row has overflowed off the bottom.
	plain := tuistyle.Sanitize(m.renderSummary())
	if !strings.Contains(plain, "Total") {
		t.Fatalf("totals line clipped at offset 0:\n%s", plain)
	}
	if !strings.Contains(plain, "step-00") {
		t.Fatalf("top row missing at offset 0:\n%s", plain)
	}
	if strings.Contains(plain, "step-29") {
		t.Fatalf("expected step-29 to be scrolled out of view at offset 0:\n%s", plain)
	}

	// Scrolling past the end reaches the last row; the totals line stays pinned
	// and the offset clamps (top row leaves view, no runaway).
	for i := 0; i < 50; i++ {
		m.scrollSummary(1)
	}
	plain = tuistyle.Sanitize(m.renderSummary())
	if !strings.Contains(plain, "step-29") {
		t.Fatalf("bottom row not reachable after scrolling:\n%s", plain)
	}
	if !strings.Contains(plain, "Total") {
		t.Fatalf("totals line clipped after scrolling:\n%s", plain)
	}
	if strings.Contains(plain, "step-00") {
		t.Fatalf("top row still visible after scrolling to the bottom:\n%s", plain)
	}
}

func TestSummaryNavigationKeysAdjustCursor(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusSuccess}
	var children []*StepNode
	for i := 0; i < 30; i++ {
		children = append(children, summaryLeaf(fmt.Sprintf("step-%02d", i), root, 1000, float64Pointer(0.1), collectedUsageRecord(5, 1)))
	}
	root.Children = children
	m := newTestModel(&Tree{Root: root}, FromList)
	m.termHeight = 20
	m.showSummary = true

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.cursor != 1 {
		t.Fatalf("j did not select next summary row: cursor=%d, want 1", m.cursor)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if m.cursor != 0 {
		t.Fatalf("k did not select previous summary row: cursor=%d, want 0", m.cursor)
	}
}

func TestSummaryQuitConfirmDuringLiveRunReachesConfirmHandler(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)
	m.running = true
	m.showSummary = true

	// q while the summary is shown must dismiss the summary and open the
	// quit-confirmation modal, so the modal's y/n keys reach the confirm
	// handler instead of being swallowed by the summary key block.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !m.quitConfirming {
		t.Fatal("q during live-run summary did not open quit confirmation")
	}
	if m.showSummary {
		t.Fatal("summary should be dismissed when the quit confirmation opens")
	}

	// n cancels the confirmation (previously swallowed while showSummary stayed true).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.quitConfirming {
		t.Fatal("n did not cancel the quit confirmation")
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
	for _, want := range []string{"Run summary", "agent", "1.5s", "$0.42", "1,234", "56"} {
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
		Attempt: 1, Usage: usage, CostUSD: cost, DurationMs: int64Pointer(duration), Outcome: "success", AgentInvoked: true,
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
