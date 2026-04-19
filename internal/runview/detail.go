package runview

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

func (m *Model) renderDetail(n *StepNode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder

	b.WriteString(tuistyle.DetailHeaderStyle.Render(n.ID))
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
		m.renderWrapped(b, "$ "+cmd)
		b.WriteString("\n")
	}

	renderCommonModifiers(b, n)

	if n.Status == StatusPending {
		return
	}

	renderExitAndDuration(b, n)

	if n.ErrorMessage != "" {
		b.WriteString("\n")
		detailLabel(b, "error:")
		m.renderWrapped(b, n.ErrorMessage)
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
	if n.StaticSession != "" {
		detailDim(b, "session", string(n.StaticSession))
	}
	if n.SessionID != "" {
		detailDim(b, "session id", n.SessionID)
	}

	renderCommonModifiers(b, n)

	prompt := n.InterpolatedPrompt
	if prompt == "" {
		prompt = n.StaticPrompt
	}
	if prompt != "" {
		b.WriteString("\n")
		detailLabel(b, "prompt:")
		m.renderWrapped(b, prompt)
	}

	if n.Status == StatusPending {
		return
	}

	renderExitAndDuration(b, n)

	if n.ErrorMessage != "" {
		b.WriteString("\n")
		detailLabel(b, "error:")
		m.renderWrapped(b, n.ErrorMessage)
		return
	}

	m.renderAgentOutputBlock(b, n)

	if n.SessionID != "" && !m.running {
		b.WriteString("\n\n")
		b.WriteString(tuistyle.AccentStyle.Render("enter → resume session"))
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
	if n.StaticSession != "" {
		detailDim(b, "session", string(n.StaticSession))
	}
	if n.SessionID != "" {
		detailDim(b, "session id", n.SessionID)
	}

	renderCommonModifiers(b, n)

	prompt := n.InterpolatedPrompt
	if prompt == "" {
		prompt = n.StaticPrompt
	}
	if prompt != "" {
		b.WriteString("\n")
		detailLabel(b, "prompt:")
		m.renderWrapped(b, prompt)
	}

	if n.Status == StatusPending {
		return
	}

	renderOutcomeAndDuration(b, n)

	if n.ErrorMessage != "" {
		b.WriteString("\n")
		detailLabel(b, "error:")
		m.renderWrapped(b, n.ErrorMessage)
	}

	if n.SessionID != "" && !m.running {
		b.WriteString("\n\n")
		b.WriteString(tuistyle.AccentStyle.Render("enter → resume session"))
	}
}

func (m *Model) renderSubWorkflowDetail(b *strings.Builder, n *StepNode) {
	name := CanonicalName(n.StaticWorkflowPath, m.resolverCfg)
	if name == "" {
		name = bareWorkflowName(n.StaticWorkflow)
	}
	if name != "" {
		detailDim(b, "workflow", name)
	}

	params := n.InterpolatedParams
	if params == nil {
		params = n.StaticParams
	}
	if len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			detailDim(b, k, params[k])
		}
	}

	renderCommonModifiers(b, n)

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
	if n.StaticLoopRequireMatches != nil {
		detailDim(b, "require_matches", boolWord(*n.StaticLoopRequireMatches))
	}
	if n.BreakTriggered {
		detailDim(b, "break_triggered", "yes")
	}

	renderCommonModifiers(b, n)

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

// renderCommonModifiers emits any step-level YAML attributes that are shared
// across step types and aren't already printed by the type-specific renderer:
// capture, capture_stderr, continue_on_failure, skip_if, break_if, workdir.
// Entries render as "name: value" dim lines, skipping fields that are unset
// (empty string for strings; false for bools).
func renderCommonModifiers(b *strings.Builder, n *StepNode) {
	if n.CaptureName != "" {
		detailDim(b, "capture", n.CaptureName)
	}
	if n.StaticCaptureStderr {
		detailDim(b, "capture_stderr", "yes")
	}
	if n.StaticContinueOnFailure {
		detailDim(b, "continue_on_failure", "yes")
	}
	if n.StaticSkipIf != "" {
		detailDim(b, "skip_if", n.StaticSkipIf)
	}
	if n.StaticBreakIf != "" {
		detailDim(b, "break_if", n.StaticBreakIf)
	}
	if n.StaticWorkdir != "" {
		detailDim(b, "workdir", n.StaticWorkdir)
	}
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
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

// renderAgentOutputBlock renders a headless agent step's output. Unlike a
// shell step, the agent's output is its response to the user's prompt, so the
// block is labeled "agent" (not "stdout"/"stderr"). While the step is still
// in-progress with no output yet, a spinner is shown so the user can see the
// CLI is alive. When only one stream has content, the label is bare "agent:";
// when both do, sub-labels disambiguate stdout vs stderr. An empty completed
// step falls back to "(empty)".
func (m *Model) renderAgentOutputBlock(b *strings.Builder, n *StepNode) {
	stdout := sanitizeUTF8(n.Stdout)
	stderr := sanitizeUTF8(n.Stderr)

	b.WriteString("\n")

	if stdout == "" && stderr == "" {
		if n.Status == StatusInProgress {
			// Label on its own row; the spinner art renders below so the
			// full 3-line animation has room to breathe.
			detailLabel(b, "agent:")
			for _, line := range tuistyle.SpinnerFrame(m.pulsePhase) {
				b.WriteString(tuistyle.AccentStyle.Render(line))
				b.WriteString("\n")
			}
			return
		}
		b.WriteString(tuistyle.LabelStyle.Render("agent: ") + tuistyle.DimStyle.Render("(empty)"))
		return
	}

	if stdout != "" && stderr != "" {
		detailLabel(b, "agent (stdout):")
		m.renderWrappedLines(b, n, stdout)
		b.WriteString("\n")
		detailLabel(b, "agent (stderr):")
		m.renderWrappedLines(b, n, stderr)
		return
	}

	text := stdout
	if text == "" {
		text = stderr
	}
	detailLabel(b, "agent:")
	m.renderWrappedLines(b, n, text)
}

// renderWrappedLines emits `output` word-wrapped to the detail-pane width
// with no "| " gutter. When the user has not loaded the full output, the
// block is tail-truncated with the shared truncateOutput helper and prefixed
// with a "(truncated…)" banner. Used by both shell stdout/stderr and agent
// stdout/stderr.
func (m *Model) renderWrappedLines(b *strings.Builder, n *StepNode, output string) {
	fullLoaded := m.loadedFull[n]
	var t truncatedOutput
	if fullLoaded {
		lines := strings.Split(output, "\n")
		t = truncatedOutput{Lines: lines, TotalLines: len(lines)}
	} else {
		t = truncateOutput(output)
	}
	if banner := t.banner(); banner != "" {
		b.WriteString(tuistyle.DimStyle.Render(banner))
		b.WriteString("\n")
	}
	width := m.detailWidth
	if width <= 0 {
		width = 80
	}
	for _, line := range t.Lines {
		sanitized := tuistyle.Sanitize(line)
		if sanitized == "" {
			b.WriteString("\n")
			continue
		}
		for _, wrapped := range wrapLine(sanitized, width) {
			b.WriteString(tuistyle.NormalStyle.Render(wrapped))
			b.WriteString("\n")
		}
	}
}

func (m *Model) renderOutputBlock(b *strings.Builder, n *StepNode, label, output string) {
	output = sanitizeUTF8(output)
	if output == "" {
		b.WriteString("\n")
		b.WriteString(tuistyle.LabelStyle.Render(label+": ") + tuistyle.DimStyle.Render("(empty)"))
		return
	}
	b.WriteString("\n")
	detailLabel(b, label+":")
	m.renderWrappedLines(b, n, output)
}

func renderExitAndDuration(b *strings.Builder, n *StepNode) {
	if n.ExitCode != nil || n.DurationMs != nil {
		b.WriteString("\n")
	}
	failed := n.Status == StatusFailed || (n.ExitCode != nil && *n.ExitCode != 0)
	if n.ExitCode != nil {
		label := "exit"
		val := fmt.Sprintf("%d", *n.ExitCode)
		if *n.ExitCode != 0 {
			b.WriteString(tuistyle.LabelStyle.Render(label+": ") + tuistyle.StatusFailed.Render(val))
		} else {
			detailDim(b, label, val)
		}
		if n.DurationMs != nil {
			b.WriteString("       ")
			detailDurationStyled(b, *n.DurationMs, failed)
		}
		b.WriteString("\n")
	} else if n.DurationMs != nil {
		detailDurationStyled(b, *n.DurationMs, failed)
		b.WriteString("\n")
	}
}

func renderOutcomeAndDuration(b *strings.Builder, n *StepNode) {
	b.WriteString("\n")
	outcome := statusLabel(n.Status)
	failed := n.Status == StatusFailed
	if failed {
		b.WriteString(tuistyle.LabelStyle.Render("outcome: ") + tuistyle.StatusFailed.Render(outcome))
		b.WriteString("\n")
	} else {
		detailDim(b, "outcome", outcome)
	}
	if n.DurationMs != nil {
		detailDurationStyled(b, *n.DurationMs, failed)
		b.WriteString("\n")
	}
}

func detailDurationStyled(b *strings.Builder, ms int64, _ bool) {
	val := formatDuration(ms)
	b.WriteString(tuistyle.LabelStyle.Render("duration: ") + tuistyle.NormalStyle.Render(val))
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
	b.WriteString(tuistyle.LabelStyle.Render(label+": ") + tuistyle.NormalStyle.Render(value))
	b.WriteString("\n")
}

func detailLabel(b *strings.Builder, s string) {
	b.WriteString(tuistyle.SectionStyle.Render(s))
	b.WriteString("\n")
}

func (m *Model) renderWrapped(b *strings.Builder, text string) {
	width := m.detailWidth
	if width <= 0 {
		width = 80
	}
	for _, line := range strings.Split(text, "\n") {
		sanitized := tuistyle.Sanitize(line)
		if sanitized == "" {
			b.WriteString("\n")
			continue
		}
		for _, wrapped := range wrapLine(sanitized, width) {
			b.WriteString(tuistyle.NormalStyle.Render(wrapped))
			b.WriteString("\n")
		}
	}
}

// wrapLine word-wraps s so each returned segment has visual width <= width.
// Words longer than width are rune-split so they still fit.
func wrapLine(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	if runewidth.StringWidth(s) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
	}
	for _, w := range words {
		ww := runewidth.StringWidth(w)
		if ww > width {
			flush()
			remaining := w
			for runewidth.StringWidth(remaining) > width {
				chunk := runewidth.Truncate(remaining, width, "")
				if chunk == "" {
					// A single rune is wider than `width` (e.g. CJK/emoji on a
					// very narrow pane). Force progress by emitting that one
					// rune alone so the loop can terminate.
					_, size := utf8.DecodeRuneInString(remaining)
					if size == 0 {
						break
					}
					chunk = remaining[:size]
				}
				out = append(out, chunk)
				remaining = remaining[len(chunk):]
			}
			if remaining != "" {
				cur.WriteString(remaining)
				curW = runewidth.StringWidth(remaining)
			}
			continue
		}
		if curW == 0 {
			cur.WriteString(w)
			curW = ww
			continue
		}
		if curW+1+ww > width {
			flush()
			cur.WriteString(w)
			curW = ww
			continue
		}
		cur.WriteByte(' ')
		cur.WriteString(w)
		curW += 1 + ww
	}
	flush()
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

// bareWorkflowName strips directory segments and .yaml/.yml suffix from a raw
// workflow reference so fallbacks display "implement-task" rather than
// "workflows/implement-task.yaml".
func bareWorkflowName(s string) string {
	if s == "" {
		return ""
	}
	base := filepath.Base(s)
	ext := filepath.Ext(base)
	if ext == ".yaml" || ext == ".yml" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
