package runview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func stripANSI(s string) string { return tuistyle.Sanitize(s) }

func TestDetailDocumentShellUsesCurrentRailsAndKeepsStreamsDistinct(t *testing.T) {
	duration := int64(1250)
	exit := 1
	node := &StepNode{
		ID:                  "test",
		Type:                NodeShell,
		Status:              StatusFailed,
		InterpolatedCommand: "go test ./...",
		DurationMs:          &duration,
		ExitCode:            &exit,
		CaptureName:         "test_output",
		Stdout:              "ordinary output",
		Stderr:              "failure output",
		ErrorMessage:        "command failed",
	}

	doc := buildDetailDocument(node, detailBuildOptions{width: 80, loadedFull: true})
	plain := tuistyle.Sanitize(strings.Join(doc.renderScreen(), "\n"))
	for _, want := range []string{
		"test", "shell", "failed", "exit: 1", "duration: 1.2s", "capture: test_output",
		"Current command", "go test ./...", "Current output", "stdout:", "ordinary output", "stderr:", "failure output",
		"Error", "command failed",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("detail missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "═") || strings.Contains(plain, "─") {
		t.Fatalf("detail retained a continuous-log separator:\n%s", plain)
	}
}

func TestDetailDocumentCopyUsesFullInputAndOptionalPreviousSection(t *testing.T) {
	prompt := strings.Repeat("long prompt words ", 20)
	node := &StepNode{ID: "review", Type: NodeHeadlessAgent, Status: StatusSuccess, StaticPrompt: prompt, Stdout: "recorded response"}
	doc := buildDetailDocument(node, detailBuildOptions{width: 24, inputExpanded: false})
	doc.sections = append([]detailSection{{label: "Previous: setup", kind: detailRailPrevious, body: "short screen recap", copy: "complete previous recap"}}, doc.sections...)

	screen := tuistyle.Sanitize(strings.Join(doc.renderScreen(), "\n"))
	copied := doc.renderCopy()
	if !strings.Contains(screen, "i expand") || !strings.Contains(screen, "…") {
		t.Fatalf("collapsed screen detail missing preview affordance:\n%s", screen)
	}
	if !strings.Contains(copied, prompt) {
		t.Fatalf("copy did not retain full collapsed prompt:\n%s", copied)
	}
	for _, want := range []string{"Previous: setup", "complete previous recap", "Current prompt", "Current response", "recorded response"} {
		if !strings.Contains(copied, want) {
			t.Errorf("copy missing %q:\n%s", want, copied)
		}
	}
}

func TestDetailDocumentBuildsPreviousExecutionRailFromBoundedResponse(t *testing.T) {
	duration := int64(1200)
	previous := &StepNode{
		ID:           "plan",
		Type:         NodeHeadlessAgent,
		Status:       StatusSuccess,
		StartOrdinal: 1,
		DurationMs:   &duration,
		Stdout:       "first response row\nsecond response row\nthird response row\n\n",
	}
	selected := &StepNode{ID: "build", Type: NodeShell, Status: StatusSuccess, StartOrdinal: 2, StaticCommand: "make build"}

	doc := buildDetailDocument(selected, detailBuildOptions{width: 80, previous: previous})
	plain := tuistyle.Sanitize(strings.Join(doc.renderScreen(), "\n"))
	copied := doc.renderCopy()
	for _, text := range []string{
		"Previous: plan", "agent · success · duration: 1.2s", "…", "second response row", "third response row",
		"Current command", "make build",
	} {
		if !strings.Contains(plain, text) {
			t.Errorf("screen detail missing %q:\n%s", text, plain)
		}
		if !strings.Contains(copied, text) {
			t.Errorf("copy detail missing %q:\n%s", text, copied)
		}
	}
	if strings.Contains(plain, "first response row") {
		t.Fatalf("previous rail exceeded its two-row tail:\n%s", plain)
	}
}

func TestSelectedDetailLoadsPersistedHistoricalShellOutput(t *testing.T) {
	sessionDir := t.TempDir()
	wf := model.Workflow{Name: "history", Steps: []model.Step{{ID: "script", Script: "echo historical"}}}
	tree := BuildTree(&wf, "history.yaml")
	tree.ApplyEvent(RawEvent{Prefix: "[script]", Type: "step_start"})
	tree.ApplyEvent(RawEvent{Prefix: "[script]", Type: "step_end", Data: map[string]any{"outcome": "success"}})
	if err := os.MkdirAll(filepath.Join(sessionDir, "output"), 0o750); err != nil {
		t.Fatal(err)
	}
	prefix := sanitizeOutputPrefixForTest("[script]")
	if err := os.WriteFile(filepath.Join(sessionDir, "output", prefix+".out"), []byte("persisted historical output\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(tree, FromInspect)
	m.sessionDir = sessionDir
	m.setSelected(childByID(tree.Root, "script"))
	if detail := m.selectedStepDetailText(); !strings.Contains(detail, "persisted historical output") {
		t.Fatalf("historical selected detail did not load persisted output:\n%s", detail)
	}
}

func TestPreviousExecutionRailUsesTypeSpecificEvidenceAndTwoRowBound(t *testing.T) {
	duration := int64(25)
	cases := []struct {
		name     string
		node     *StepNode
		contains []string
		absent   []string
	}{
		{
			name:     "failed script prefers stderr",
			node:     &StepNode{ID: "script", Type: NodeScript, Status: StatusFailed, DurationMs: &duration, Stdout: "stdout must not be used", Stderr: "error one\nerror two\nerror three"},
			contains: []string{"error two", "error three", "…"},
			absent:   []string{"stdout must not be used", "error one"},
		},
		{
			name:     "interactive agent has no transcript",
			node:     &StepNode{ID: "terminal", Type: NodeInteractiveAgent, Status: StatusSuccess, DurationMs: &duration, AgentProfile: "planner", AgentCLI: "codex", AgentModel: "gpt-5.6", Stdout: "must not be shown"},
			contains: []string{"interactive agent", "profile: planner", "cli: codex", "model: gpt-5.6", "No transcript captured"},
			absent:   []string{"must not be shown"},
		},
		{
			name:     "skipped explains triggering expression without output",
			node:     &StepNode{ID: "skip", Type: NodeShell, Status: StatusSkipped, DurationMs: &duration, TriggeredSkipIf: "previous_success", Stdout: "must not be shown"},
			contains: []string{"skipped", "skip_if: previous_success"},
			absent:   []string{"must not be shown"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			section, ok := previousExecutionSection(tc.node, 80)
			if !ok {
				t.Fatal("previous section was omitted")
			}
			for _, want := range tc.contains {
				if !strings.Contains(section.body, want) {
					t.Errorf("previous body missing %q: %q", want, section.body)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(section.body, unwanted) {
					t.Errorf("previous body included %q: %q", unwanted, section.body)
				}
			}
		})
	}
}

func TestPreviousOutputExcerptKeepsOnlyTwoVisualRowsWhenTruncated(t *testing.T) {
	node := &StepNode{Type: NodeShell, Status: StatusSuccess, Stdout: "first row\nsecond row\nthird row"}
	if got, want := previousOutputExcerpt(node, 80), "…second row\nthird row"; got != want {
		t.Fatalf("previous excerpt = %q, want %q", got, want)
	}
}

func TestPreviousOutputExcerptStaysWithinTwoRowsAtTinyWidths(t *testing.T) {
	node := &StepNode{Type: NodeShell, Status: StatusSuccess, Stdout: "first\nsecond\nthird"}
	for _, width := range []int{0, 1} {
		excerpt := previousOutputExcerpt(node, width)
		if got := len(wrappedPlainLines(excerpt, width)); got > 2 {
			t.Errorf("width %d excerpt uses %d visual rows: %q", width, got, excerpt)
		}
	}
}

func TestDetailDocumentPendingContainsOnlyStaticData(t *testing.T) {
	node := &StepNode{
		ID:           "future",
		Type:         NodeHeadlessAgent,
		Status:       StatusPending,
		StaticPrompt: "configured prompt",
		DurationMs:   int64Pointer(900),
		Stdout:       "must not be shown",
		ErrorMessage: "must not be shown",
		Attempts:     []AttemptMetrics{{Usage: collectedUsageRecord(5, 3)}},
		AgentProfile: "runtime-profile",
		AgentCLI:     "runtime-cli",
		AgentModel:   "runtime-model",
		StaticAgent:  "configured-agent",
		StaticCLI:    "configured-cli",
		StaticModel:  "configured-model",
	}
	plain := buildDetailDocument(node, detailBuildOptions{width: 80}).renderCopy()
	for _, want := range []string{"pending", "Current prompt", "configured prompt"} {
		if !strings.Contains(plain, want) {
			t.Errorf("pending detail missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"900ms", "must not be shown", "runtime-profile", "runtime-cli", "runtime-model", "tokens:"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("pending detail fabricated runtime data %q:\n%s", unwanted, plain)
		}
	}
}

func TestDetailDocumentContainerShowsAggregateWithoutChildren(t *testing.T) {
	group := &StepNode{ID: "parallel", Type: NodeGroup, Status: StatusInProgress}
	group.Children = []*StepNode{
		{ID: "done", Type: NodeShell, Status: StatusSuccess, Parent: group},
		{ID: "running", Type: NodeShell, Status: StatusInProgress, Parent: group},
		{ID: "later", Type: NodeShell, Status: StatusPending, Parent: group},
	}
	plain := buildDetailDocument(group, detailBuildOptions{width: 80}).renderCopy()
	for _, want := range []string{"Current status", "1 success", "1 in progress", "1 pending"} {
		if !strings.Contains(plain, want) {
			t.Errorf("container detail missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"done\n", "running\n", "later\n"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("container detail listed child %q:\n%s", unwanted, plain)
		}
	}
}

func TestDetailDocumentProgressIsOnlyForActiveHeadlessOrCall(t *testing.T) {
	running := &StepNode{ID: "agent", Type: NodeHeadlessAgent, Status: StatusInProgress, StaticPrompt: "work"}
	active := buildDetailDocument(running, detailBuildOptions{width: 80, runActive: true, pulsePhase: 1}).renderCopy()
	if !strings.Contains(active, "Working") {
		t.Fatalf("active headless agent lacks response progress:\n%s", active)
	}
	inactive := buildDetailDocument(running, detailBuildOptions{width: 80, runActive: false, pulsePhase: 1}).renderCopy()
	if strings.Contains(inactive, "Working") {
		t.Fatalf("inactive agent retained progress:\n%s", inactive)
	}
	interactive := &StepNode{ID: "terminal", Type: NodeInteractiveAgent, Status: StatusInProgress, StaticPrompt: "work"}
	plain := buildDetailDocument(interactive, detailBuildOptions{width: 80, runActive: true, pulsePhase: 1}).renderCopy()
	if strings.Contains(plain, "Working") || strings.Contains(plain, "Current response") {
		t.Fatalf("interactive agent fabricated response progress:\n%s", plain)
	}
}

func TestDetailDocumentAgentMetricsPreserveUnavailableAndLegacySemantics(t *testing.T) {
	unavailable := &StepNode{ID: "agent", Type: NodeHeadlessAgent, Status: StatusSuccess, Attempts: []AttemptMetrics{{Usage: &model.UsageRecord{Status: model.UsageUnavailable, Reason: model.UnavailablePTYContext}}}}
	plain := buildDetailDocument(unavailable, detailBuildOptions{width: 80}).renderCopy()
	for _, want := range []string{"usage: ?", "pty-context", "cost: ?"} {
		if !strings.Contains(plain, want) {
			t.Errorf("unavailable metrics missing %q:\n%s", want, plain)
		}
	}
	legacy := &StepNode{ID: "legacy", Type: NodeHeadlessAgent, Status: StatusSuccess}
	if legacyPlain := buildDetailDocument(legacy, detailBuildOptions{width: 80}).renderCopy(); strings.Contains(legacyPlain, "usage:") || strings.Contains(legacyPlain, "cost:") {
		t.Fatalf("legacy detail invented metrics:\n%s", legacyPlain)
	}
}

func TestDetailDocumentUsesLatestAttemptForOutcomeDurationAndMetrics(t *testing.T) {
	firstDuration := int64(3_000)
	latestDuration := int64(500)
	firstCost, latestCost := 1.00, 0.20
	node := &StepNode{
		ID:         "retry",
		Type:       NodeHeadlessAgent,
		Status:     StatusSuccess,
		Outcome:    "failed",
		DurationMs: &firstDuration,
		Attempts: []AttemptMetrics{
			{Attempt: 1, Outcome: "failed", DurationMs: &firstDuration, Usage: collectedUsageRecord(100, 10), CostUSD: &firstCost},
			{Attempt: 2, Outcome: "success", DurationMs: &latestDuration, Usage: collectedUsageRecord(20, 4), CostUSD: &latestCost},
		},
	}
	plain := buildDetailDocument(node, detailBuildOptions{width: 80}).renderCopy()
	for _, want := range []string{"success", "duration: 500ms", "attempt: 2", "input 20", "output 4", "cost: $0.20"} {
		if !strings.Contains(plain, want) {
			t.Errorf("latest attempt detail missing %q:\n%s", want, plain)
		}
	}
	for _, stale := range []string{"failed", "3.0s", "input 100", "$1.00"} {
		if strings.Contains(plain, stale) {
			t.Errorf("detail retained stale attempt value %q:\n%s", stale, plain)
		}
	}
}

func TestDetailDocumentCurrentFormUsesStaticUIConfiguration(t *testing.T) {
	node := &StepNode{
		ID:              "choose",
		Type:            NodeUI,
		Status:          StatusPending,
		StaticUITitle:   "Choose scope",
		StaticUIBody:    "Pick the work to run.",
		StaticUIActions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		StaticUIInputs:  []model.UIInput{{Kind: "select", ID: "scope", Prompt: "Scope", Options: []string{"all", "changed"}, Default: "changed"}},
	}
	plain := buildDetailDocument(node, detailBuildOptions{width: 80}).renderCopy()
	for _, want := range []string{"Current form", "Choose scope", "Pick the work to run.", "Scope", "all", "changed", "Continue"} {
		if !strings.Contains(plain, want) {
			t.Errorf("configured form missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Current outcome") {
		t.Fatalf("pending form invented runtime outcome:\n%s", plain)
	}
}

func TestDetailDocumentContainerStatusRetainsMetadataAndStaticParamsFallback(t *testing.T) {
	duration := int64(1200)
	node := &StepNode{
		ID:                 "nested",
		Type:               NodeSubWorkflow,
		Status:             StatusSuccess,
		Outcome:            "success",
		DurationMs:         &duration,
		StaticWorkflow:     "child.yaml",
		StaticParams:       map[string]string{"task": "{{task}}"},
		InterpolatedParams: nil,
	}
	plain := buildDetailDocument(node, detailBuildOptions{width: 80}).renderCopy()
	for _, want := range []string{"Current status", "identity: nested", "outcome: success", "duration: 1.2s", "task: {{task}}"} {
		if !strings.Contains(plain, want) {
			t.Errorf("container status missing %q:\n%s", want, plain)
		}
	}
}

func TestDetailDocumentSanitizesHeaderValuesBeforeScreenRendering(t *testing.T) {
	node := &StepNode{ID: "safe\x1b]52;c;forged\a", Type: NodeHeadlessAgent, Status: StatusSuccess, AgentProfile: "profile\x1b[31m"}
	screen := strings.Join(buildDetailDocument(node, detailBuildOptions{width: 80}).renderScreen(), "\n")
	if strings.Contains(screen, "\x1b]52") || strings.Contains(screen, "\x1b[31m") {
		t.Fatalf("screen renderer retained terminal escape sequence: %q", screen)
	}
}

func TestModelInputExpansionIsStableByNodeKeyAndResetsDetailScrollOnSelection(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot}
	long := &StepNode{ID: "long", Type: NodeShell, Status: StatusSuccess, Parent: root, StaticCommand: strings.Repeat("wrapped command ", 20)}
	other := &StepNode{ID: "other", Type: NodeShell, Status: StatusSuccess, Parent: root, StaticCommand: "echo other"}
	root.Children = []*StepNode{long, other}
	m := newTestModel(&Tree{Root: root}, FromInspect)
	m.termWidth = 42
	m.setSelected(long)
	m.detailOffset = 4
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if !m.inputExpanded[long.NodeKey()] {
		t.Fatal("i did not expand the selected long input")
	}
	m.handleStepNavigation(1)
	if m.detailOffset != 0 {
		t.Fatalf("selection detail scroll = %d, want reset to top", m.detailOffset)
	}
	m.handleStepNavigation(-1)
	if !m.inputExpanded[long.NodeKey()] {
		t.Fatal("input expansion was discarded after changing selection")
	}
}
