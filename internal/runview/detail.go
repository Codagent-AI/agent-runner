package runview

import (
	"fmt"
	"strings"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

func (m *Model) renderDetail(n *StepNode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder

	b.WriteString(tuistyle.SelectedStyle.Render(n.ID))
	b.WriteString("\n\n")

	switch n.Type {
	case NodeShell:
		m.renderShellDetail(&b, n)
	case NodeHeadlessAgent:
		m.renderHeadlessDetail(&b, n)
	case NodeInteractiveAgent:
		m.renderInteractiveDetail(&b, n)
	case NodeSubWorkflow:
		m.renderSubWorkflowDetail(&b, n)
	case NodeLoop:
		m.renderLoopDetail(&b, n)
	case NodeIteration:
		m.renderIterationDetail(&b, n)
	}
	return b.String()
}

func (m *Model) renderShellDetail(b *strings.Builder, n *StepNode) {
	cmd := n.InterpolatedCommand
	if cmd == "" {
		cmd = n.StaticCommand
	}
	if cmd != "" {
		detailLine(b, "$ "+cmd)
		b.WriteString("\n")
	}

	if n.Status == StatusPending {
		if n.CaptureName != "" {
			detailDim(b, "capture", n.CaptureName)
		}
		return
	}

	renderExitAndDuration(b, n)

	if n.CaptureName != "" {
		detailDim(b, "capture", n.CaptureName)
	}

	if n.ErrorMessage != "" {
		b.WriteString("\n")
		detailLabel(b, "error:")
		renderWrapped(b, n.ErrorMessage)
		return
	}

	m.renderOutputBlock(b, n, "stdout", n.Stdout)
	m.renderOutputBlock(b, n, "stderr", n.Stderr)
}

func (m *Model) renderHeadlessDetail(b *strings.Builder, n *StepNode) {
	profile := n.AgentProfile
	if profile == "" {
		profile = n.StaticAgent
	}
	if profile != "" {
		detailDim(b, "agent", profile)
	}
	model := n.AgentModel
	if model == "" {
		model = n.StaticModel
	}
	if model != "" {
		detailDim(b, "model", model)
	}
	cli := n.AgentCLI
	if cli == "" {
		cli = n.StaticCLI
	}
	if cli != "" {
		detailDim(b, "cli", cli)
	}
	if n.SessionID != "" {
		detailDim(b, "session id", n.SessionID)
	}

	prompt := n.InterpolatedPrompt
	if prompt == "" {
		prompt = n.StaticPrompt
	}
	if prompt != "" {
		b.WriteString("\n")
		detailLabel(b, "prompt:")
		renderWrapped(b, prompt)
	}

	if n.Status == StatusPending {
		return
	}

	renderExitAndDuration(b, n)

	if n.ErrorMessage != "" {
		b.WriteString("\n")
		detailLabel(b, "error:")
		renderWrapped(b, n.ErrorMessage)
		return
	}

	m.renderOutputBlock(b, n, "stdout", n.Stdout)
	m.renderOutputBlock(b, n, "stderr", n.Stderr)

	if n.SessionID != "" {
		b.WriteString("\n")
		b.WriteString(accentStyle.Render("enter → resume session"))
	}
}

func (m *Model) renderInteractiveDetail(b *strings.Builder, n *StepNode) {
	profile := n.AgentProfile
	if profile == "" {
		profile = n.StaticAgent
	}
	if profile != "" {
		detailDim(b, "agent", profile)
	}
	model := n.AgentModel
	if model == "" {
		model = n.StaticModel
	}
	if model != "" {
		detailDim(b, "model", model)
	}
	cli := n.AgentCLI
	if cli == "" {
		cli = n.StaticCLI
	}
	if cli != "" {
		detailDim(b, "cli", cli)
	}
	if n.SessionID != "" {
		detailDim(b, "session id", n.SessionID)
	}

	prompt := n.InterpolatedPrompt
	if prompt == "" {
		prompt = n.StaticPrompt
	}
	if prompt != "" {
		b.WriteString("\n")
		detailLabel(b, "prompt:")
		renderWrapped(b, prompt)
	}

	if n.Status == StatusPending {
		return
	}

	renderOutcomeAndDuration(b, n)

	if n.ErrorMessage != "" {
		b.WriteString("\n")
		detailLabel(b, "error:")
		renderWrapped(b, n.ErrorMessage)
	}

	if n.SessionID != "" {
		b.WriteString("\n")
		b.WriteString(accentStyle.Render("enter → resume session"))
	}
}

func (m *Model) renderSubWorkflowDetail(b *strings.Builder, n *StepNode) {
	name := CanonicalName(n.StaticWorkflowPath, m.resolverCfg)
	if name == "" && n.StaticWorkflow != "" {
		name = n.StaticWorkflow
	}
	if name != "" {
		detailDim(b, "workflow", name)
	}

	params := n.InterpolatedParams
	if params == nil {
		params = n.StaticParams
	}
	if len(params) > 0 {
		for k, v := range params {
			detailDim(b, k, v)
		}
	}

	if n.Status != StatusPending {
		renderOutcomeAndDuration(b, n)
	}

	b.WriteString("\n")
	b.WriteString(tuistyle.DimStyle.Render("press enter to drill in →"))
}

func (m *Model) renderLoopDetail(b *strings.Builder, n *StepNode) {
	loopType := n.LoopType
	if loopType == "" {
		if n.StaticLoopOver != "" {
			loopType = "for-each"
		} else {
			loopType = "counted"
		}
	}
	detailDim(b, "loop", loopType)

	if loopType == "for-each" {
		if n.StaticLoopOver != "" {
			detailDim(b, "over", n.StaticLoopOver)
		}
		if n.StaticLoopAs != "" {
			detailDim(b, "as", n.StaticLoopAs)
		}
		if len(n.LoopMatches) > 0 {
			detailDim(b, "matches", fmt.Sprintf("%d", len(n.LoopMatches)))
		}
	} else if n.StaticLoopMax != nil {
		detailDim(b, "max", fmt.Sprintf("%d", *n.StaticLoopMax))
	}

	total := m.loopTotal(n)
	if total > 0 {
		detailDim(b, "iterations", fmt.Sprintf("%d of %d", n.IterationsCompleted, total))
	}
	if n.BreakTriggered {
		detailDim(b, "break_triggered", "yes")
	}

	if n.Status != StatusPending {
		renderOutcomeAndDuration(b, n)
	}

	b.WriteString("\n")
	b.WriteString(tuistyle.DimStyle.Render("press enter to drill in →"))
}

func (m *Model) renderIterationDetail(b *strings.Builder, n *StepNode) {
	detailDim(b, "iteration", fmt.Sprintf("%d", n.IterationIndex+1))
	if n.BindingValue != "" {
		detailDim(b, "value", n.BindingValue)
	}
	if n.Status != StatusPending {
		renderOutcomeAndDuration(b, n)
	}
	b.WriteString("\n")
	b.WriteString(tuistyle.DimStyle.Render("press enter to drill in →"))
}

func (m *Model) loopTotal(n *StepNode) int {
	if len(n.LoopMatches) > 0 {
		return len(n.LoopMatches)
	}
	if n.StaticLoopMax != nil {
		return *n.StaticLoopMax
	}
	return 0
}

func (m *Model) renderOutputBlock(b *strings.Builder, n *StepNode, label, output string) {
	output = sanitizeUTF8(output)
	if output == "" {
		b.WriteString("\n")
		b.WriteString(tuistyle.DimStyle.Render(label + ": (empty)"))
		return
	}

	fullLoaded := m.loadedFull[n] && label == "stdout"
	var t truncatedOutput
	if fullLoaded {
		lines := strings.Split(output, "\n")
		t = truncatedOutput{Lines: lines, TotalLines: len(lines)}
	} else {
		t = truncateOutput(output)
	}

	b.WriteString("\n")
	detailLabel(b, label+":")

	if banner := t.banner(); banner != "" {
		b.WriteString(tuistyle.DimStyle.Render(banner))
		b.WriteString("\n")
	}

	for _, line := range t.Lines {
		b.WriteString("| ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func renderExitAndDuration(b *strings.Builder, n *StepNode) {
	if n.ExitCode != nil || n.DurationMs != nil {
		b.WriteString("\n")
	}
	if n.ExitCode != nil {
		label := "exit"
		val := fmt.Sprintf("%d", *n.ExitCode)
		if *n.ExitCode != 0 {
			b.WriteString(tuistyle.DimStyle.Render(label+": ") + tuistyle.StatusFailed.Render(val))
		} else {
			detailDim(b, label, val)
		}
		if n.DurationMs != nil {
			b.WriteString("       ")
			detailDim(b, "duration", formatDuration(*n.DurationMs))
		}
		b.WriteString("\n")
	} else if n.DurationMs != nil {
		detailDim(b, "duration", formatDuration(*n.DurationMs))
		b.WriteString("\n")
	}
}

func renderOutcomeAndDuration(b *strings.Builder, n *StepNode) {
	b.WriteString("\n")
	outcome := statusLabel(n.Status)
	detailDim(b, "outcome", outcome)
	if n.DurationMs != nil {
		detailDim(b, "duration", formatDuration(*n.DurationMs))
	}
}

func statusLabel(s NodeStatus) string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusFailed:
		return "failed"
	case StatusSkipped:
		return "skipped"
	case StatusInProgress:
		return "in progress"
	default:
		return "pending"
	}
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs) / 60
	remainSecs := int(secs) % 60
	return fmt.Sprintf("%dm%ds", mins, remainSecs)
}

func detailDim(b *strings.Builder, label, value string) {
	b.WriteString(tuistyle.DimStyle.Render(label+": ") + tuistyle.NormalStyle.Render(value))
	b.WriteString("\n")
}

func detailLine(b *strings.Builder, s string) {
	b.WriteString(tuistyle.NormalStyle.Render(s))
	b.WriteString("\n")
}

func detailLabel(b *strings.Builder, s string) {
	b.WriteString(tuistyle.DimStyle.Render(s))
	b.WriteString("\n")
}

func renderWrapped(b *strings.Builder, text string) {
	for _, line := range strings.Split(text, "\n") {
		b.WriteString("| ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}
