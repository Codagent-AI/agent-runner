package runview

import (
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

func TestModelInputExpansionIsStableByNodeKeyAndResetsDetailScrollOnSelection(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot}
	long := &StepNode{ID: "long", Type: NodeShell, Status: StatusSuccess, Parent: root, StaticCommand: strings.Repeat("wrapped command ", 20)}
	other := &StepNode{ID: "other", Type: NodeShell, Status: StatusSuccess, Parent: root, StaticCommand: "echo other"}
	root.Children = []*StepNode{long, other}
	m := newTestModel(&Tree{Root: root}, FromInspect)
	m.termWidth = 42
	m.setSelected(long)
	m.logOffset = 4
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if !m.inputExpanded[long.NodeKey()] {
		t.Fatal("i did not expand the selected long input")
	}
	m.handleStepNavigation(1)
	if m.logOffset != 0 {
		t.Fatalf("selection detail scroll = %d, want reset to top", m.logOffset)
	}
	m.handleStepNavigation(-1)
	if !m.inputExpanded[long.NodeKey()] {
		t.Fatal("input expansion was discarded after changing selection")
	}
}
