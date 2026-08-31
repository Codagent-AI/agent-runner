package runview

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/mattn/go-runewidth"
)

// detailDocument is the selected-node presentation source. Screen and
// clipboard renderers deliberately share this structure so visual truncation
// never changes what is copied.
type detailDocument struct {
	width    int
	header   []string
	sections []detailSection
}

type detailRailKind int

const (
	detailRailPrevious detailRailKind = iota
	detailRailInput
	detailRailOutput
	detailRailError
)

const currentFormLabel = "Current form"

type detailSection struct {
	label string
	kind  detailRailKind
	body  string
	copy  string
}

type detailBuildOptions struct {
	width         int
	loadedFull    bool
	inputExpanded bool
	previous      *StepNode
	pulsePhase    float64
	runActive     bool
	resumeReady   bool
	resolverCfg   ResolverConfig
}

func buildDetailDocument(node *StepNode, options detailBuildOptions) detailDocument {
	if options.width < 12 {
		options.width = 12
	}
	doc := detailDocument{width: options.width, header: detailHeader(node, options)}
	if node == nil {
		return doc
	}
	if previous, ok := previousExecutionSection(options.previous, options.width); ok {
		doc.sections = append(doc.sections, previous)
	}

	if node.Status == StatusPending {
		buildPendingDetail(&doc, node, options)
		return doc
	}

	switch node.Type {
	case NodeShell:
		doc.addInput("Current command", currentCommand(node), options)
		doc.addOutput("Current output", streamsText(node.Stdout, node.Stderr, options.loadedFull))
	case NodeScript:
		doc.addInput("Current script", node.StaticScript, options)
		doc.addOutput("Current output", streamsText(node.Stdout, node.Stderr, options.loadedFull))
	case NodeHeadlessAgent, NodeAgentCall:
		doc.addInput("Current prompt", currentPrompt(node), options)
		doc.addOutput("Current response", agentResponseText(node, options))
	case NodeInteractiveAgent:
		doc.addInput("Current prompt", currentPrompt(node), options)
	case NodeUI:
		doc.addOutput(currentFormLabel, uiFormText(node))
		doc.addOutput("Current outcome", outcomeText(node))
	case NodeSubWorkflow, NodeLoop, NodeIteration, NodeGroup:
		doc.addOutput("Current status", containerStatusText(node))
	}

	if node.ErrorMessage != "" {
		doc.sections = append(doc.sections, detailSection{label: "Error", kind: detailRailError, body: node.ErrorMessage, copy: node.ErrorMessage})
	}
	if node.Status == StatusWarning {
		doc.sections = append(doc.sections, detailSection{label: "Warning", kind: detailRailError, body: "Workflow execution continued after this non-blocking failure.", copy: "Workflow execution continued after this non-blocking failure."})
	}
	return doc
}

func previousExecutionSection(node *StepNode, width int) (detailSection, bool) {
	if node == nil {
		return detailSection{}, false
	}
	name := node.ID
	if node.Type == NodeAgentCall && node.callLabel() != "" {
		name = node.callLabel()
	}
	metadata := []string{nodeTypeLabel(node.Type), statusLabel(node.Status)}
	if outcome := selectedOutcome(node); outcome != "" && outcome != statusLabel(node.Status) {
		metadata = append(metadata, outcome)
	}
	if (node.Type == NodeShell || node.Type == NodeScript) && node.ExitCode != nil {
		metadata = append(metadata, fmt.Sprintf("exit: %d", *node.ExitCode))
	}
	if duration := selectedDuration(node); duration != nil {
		metadata = append(metadata, "duration: "+formatDuration(*duration))
	}
	body := strings.Join(metadata, " · ")
	switch {
	case node.Status == StatusSkipped:
		if skipIf := firstNonEmpty(node.TriggeredSkipIf, node.StaticSkipIf); skipIf != "" {
			body += "\nskip_if: " + skipIf
		}
	case isInteractiveExecution(node):
		if node.Type == NodeInteractiveAgent {
			body += previousInteractiveAgentMetadata(node)
		}
		body += "\nNo transcript captured"
	case node.Type == NodeUI:
		// The compact metadata above is the recorded UI outcome. Never include
		// form values in historical context.
	default:
		if excerpt := previousOutputExcerpt(node, width-3); excerpt != "" {
			body += "\n" + excerpt
		}
	}
	return detailSection{label: "Previous: " + name, kind: detailRailPrevious, body: body, copy: body}, true
}

func previousInteractiveAgentMetadata(node *StepNode) string {
	var lines []string
	if profile := firstNonEmpty(node.AgentProfile, node.StaticAgent); profile != "" {
		lines = append(lines, "profile: "+profile)
	}
	if cliName := firstNonEmpty(node.AgentCLI, node.StaticCLI); cliName != "" {
		lines = append(lines, "cli: "+cliName)
	}
	if modelName := firstNonEmpty(node.AgentModel, node.StaticModel); modelName != "" {
		lines = append(lines, "model: "+modelName)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

func isInteractiveExecution(node *StepNode) bool {
	if node == nil {
		return false
	}
	return node.Type == NodeInteractiveAgent ||
		((node.Type == NodeShell || node.Type == NodeScript) && node.StaticMode == model.ModeInteractive)
}

func previousOutputExcerpt(node *StepNode, width int) string {
	if node == nil {
		return ""
	}
	var output string
	switch node.Type {
	case NodeHeadlessAgent, NodeAgentCall:
		output = firstNonEmpty(node.Stdout, node.Stderr)
	case NodeShell, NodeScript:
		if node.Status == StatusFailed {
			output = firstNonEmpty(node.Stderr, node.Stdout)
		} else {
			output = node.Stdout
		}
	default:
		return ""
	}
	output = strings.TrimRight(sanitizeUTF8(output), "\r\n")
	if output == "" {
		return ""
	}
	rows := wrappedPlainLines(output, max(1, width))
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) == 0 {
		return ""
	}
	if len(rows) <= 2 {
		return strings.Join(rows, "\n")
	}
	first := rows[len(rows)-2]
	ellipsisWidth := runewidth.StringWidth("…")
	if width <= ellipsisWidth {
		first = ""
	} else if runewidth.StringWidth(first) > width-ellipsisWidth {
		first = runewidth.Truncate(first, width-ellipsisWidth, "")
	}
	return "…" + first + "\n" + rows[len(rows)-1]
}

func buildPendingDetail(doc *detailDocument, node *StepNode, options detailBuildOptions) {
	switch node.Type {
	case NodeShell:
		doc.addInput("Current command", node.StaticCommand, options)
	case NodeScript:
		doc.addInput("Current script", node.StaticScript, options)
	case NodeHeadlessAgent, NodeInteractiveAgent:
		doc.addInput("Current prompt", node.StaticPrompt, options)
	case NodeAgentCall:
		doc.addInput("Current prompt", node.InterpolatedPrompt, options)
	case NodeUI:
		doc.addOutput(currentFormLabel, uiFormText(node))
	case NodeSubWorkflow:
		var body []string
		if workflow := CanonicalName(node.StaticWorkflowPath, options.resolverCfg); workflow != "" {
			body = append(body, "workflow: "+workflow)
		} else if node.StaticWorkflow != "" {
			body = append(body, "workflow: "+node.StaticWorkflow)
		}
		body = append(body, plainParams(node.StaticParams)...)
		doc.addOutput("Current status", strings.Join(body, "\n"))
	case NodeLoop, NodeIteration, NodeGroup:
		doc.addOutput("Current status", staticContainerText(node))
	}
}

func detailHeader(node *StepNode, options detailBuildOptions) []string {
	if node == nil {
		return nil
	}
	parts := []string{node.ID, nodeTypeLabel(node.Type), statusLabel(node.Status)}
	if outcome := selectedOutcome(node); outcome != "" && outcome != statusLabel(node.Status) {
		parts = append(parts, outcome)
	}
	if duration := selectedDuration(node); duration != nil && node.Status != StatusPending {
		parts = append(parts, "duration: "+formatDuration(*duration))
	}
	lines := []string{strings.Join(parts, " · ")}
	if node.Status == StatusPending {
		return lines
	}
	lines = append(lines, detailExecutionMetadata(node)...)
	if isAgentNode(node) {
		lines = append(lines, detailAgentMetadata(node, options)...)
	}
	return lines
}

func detailExecutionMetadata(node *StepNode) []string {
	var lines []string
	if node.Type == NodeShell || node.Type == NodeScript {
		if node.ExitCode != nil {
			lines = append(lines, fmt.Sprintf("exit: %d", *node.ExitCode))
		}
	}
	if node.CaptureName != "" {
		lines = append(lines, "capture: "+node.CaptureName)
	}
	if node.Status == StatusSkipped && node.StaticSkipIf != "" {
		lines = append(lines, "skip_if: "+node.StaticSkipIf)
	}
	if node.BreakTriggered && node.StaticBreakIf != "" {
		lines = append(lines, "break_if: "+node.StaticBreakIf)
	}
	return lines
}

func detailAgentMetadata(node *StepNode, options detailBuildOptions) []string {
	var lines []string
	if node.Type == NodeAgentCall {
		target := strings.TrimSpace(node.CallTargetKind + " " + node.CallTargetName)
		if target != "" {
			lines = append(lines, "target: "+target)
		}
	}
	if profile := firstNonEmpty(node.AgentProfile, node.StaticAgent); profile != "" {
		lines = append(lines, "profile: "+profile)
	}
	if cliName := firstNonEmpty(node.AgentCLI, node.StaticCLI); cliName != "" {
		lines = append(lines, "cli: "+cliName)
	}
	modelName := firstNonEmpty(node.AgentModel, node.StaticModel)
	if modelName == "" {
		modelName = "(unknown)"
	}
	lines = append(lines, "model: "+modelName)
	lines = append(lines, detailMetrics(node)...)
	if options.resumeReady && node.SessionID != "" {
		lines = append(lines, "session: "+node.SessionID, "enter → resume session")
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func detailMetrics(node *StepNode) []string {
	if node.Status == StatusPending || node.Status == StatusInProgress || len(node.Attempts) == 0 {
		return nil
	}
	latest := node.Attempts[len(node.Attempts)-1]
	var lines []string
	if latest.Attempt > 1 {
		lines = append(lines, fmt.Sprintf("attempt: %d", latest.Attempt))
	}
	if latest.Usage == nil || latest.Usage.Status != model.UsageCollected {
		reason := ""
		if latest.Usage != nil && latest.Usage.Reason != "" {
			reason = " (" + string(latest.Usage.Reason) + ")"
		}
		lines = append(lines, "usage: ?"+reason)
	} else {
		lines = append(lines, "tokens: "+formatTokenCounts(latest.Usage.Tokens))
	}
	if latest.CostUSD == nil {
		lines = append(lines, "cost: ?")
	} else {
		lines = append(lines, "cost: "+formatUSD(*latest.CostUSD))
	}
	return lines
}

func selectedOutcome(node *StepNode) string {
	if len(node.Attempts) > 0 {
		if outcome := node.Attempts[len(node.Attempts)-1].Outcome; outcome != "" {
			return outcome
		}
	}
	return node.Outcome
}

func selectedDuration(node *StepNode) *int64 {
	if len(node.Attempts) > 0 {
		if duration := node.Attempts[len(node.Attempts)-1].DurationMs; duration != nil {
			return duration
		}
	}
	return node.DurationMs
}

func (doc *detailDocument) addInput(label, input string, options detailBuildOptions) {
	input = strings.TrimRight(input, "\r\n")
	if input == "" {
		return
	}
	preview, expandable := previewInput(input, doc.width-3, options.inputExpanded)
	if expandable {
		if options.inputExpanded {
			preview += "\n i collapse"
		} else {
			preview += "\n…\n i expand"
		}
	}
	doc.sections = append(doc.sections, detailSection{label: label, kind: detailRailInput, body: preview, copy: input})
}

func (doc *detailDocument) addOutput(label, body string) {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		body = "(empty)"
	}
	doc.sections = append(doc.sections, detailSection{label: label, kind: detailRailOutput, body: body, copy: body})
}

func previewInput(input string, width int, expanded bool) (string, bool) {
	lines := wrappedPlainLines(input, width)
	if len(lines) <= 3 || expanded {
		return strings.Join(lines, "\n"), len(lines) > 3
	}
	return strings.Join(lines[:3], "\n"), true
}

func streamsText(stdout, stderr string, loadedFull bool) string {
	var parts []string
	if stdout != "" {
		parts = append(parts, "stdout:\n"+boundedOutput(stdout, loadedFull))
	}
	if stderr != "" {
		parts = append(parts, "stderr:\n"+boundedOutput(stderr, loadedFull))
	}
	return strings.Join(parts, "\n\n")
}

func agentResponseText(node *StepNode, options detailBuildOptions) string {
	response := streamsText(node.Stdout, node.Stderr, options.loadedFull)
	progress := node.Status == StatusInProgress && options.runActive && !node.Aborted
	if response == "" && progress {
		return "Working " + tuistyle.SpinnerGlyph(options.pulsePhase)
	}
	if response != "" && progress {
		return response + "\n\nWorking " + tuistyle.SpinnerGlyph(options.pulsePhase)
	}
	return response
}

func boundedOutput(output string, loadedFull bool) string {
	output = sanitizeUTF8(output)
	if loadedFull {
		return output
	}
	t := truncateOutput(output)
	lines := t.Lines
	if banner := t.banner(); banner != "" {
		lines = append([]string{banner}, lines...)
	}
	return strings.Join(lines, "\n")
}

func outcomeText(node *StepNode) string {
	var lines []string
	if outcome := selectedOutcome(node); outcome != "" {
		lines = append(lines, "outcome: "+outcome)
	} else {
		lines = append(lines, "status: "+statusLabel(node.Status))
	}
	if duration := selectedDuration(node); duration != nil {
		lines = append(lines, "duration: "+formatDuration(*duration))
	}
	return strings.Join(lines, "\n")
}

func containerStatusText(node *StepNode) string {
	lines := []string{"identity: " + node.ID, "status: " + statusLabel(node.Status)}
	if outcome := selectedOutcome(node); outcome != "" {
		lines = append(lines, "outcome: "+outcome)
	}
	if duration := selectedDuration(node); duration != nil {
		lines = append(lines, "duration: "+formatDuration(*duration))
	}
	if node.Type == NodeSubWorkflow {
		if node.StaticWorkflowPath != "" {
			lines = append(lines, "workflow: "+node.StaticWorkflowPath)
		} else if node.StaticWorkflow != "" {
			lines = append(lines, "workflow: "+node.StaticWorkflow)
		}
		params := node.InterpolatedParams
		if params == nil {
			params = node.StaticParams
		}
		lines = append(lines, plainParams(params)...)
	}
	if node.Type == NodeLoop {
		if total := loopTotal(node); total > 0 {
			lines = append(lines, fmt.Sprintf("iterations: %d of %d", node.IterationsCompleted, total))
		}
	}
	if node.Type == NodeIteration {
		lines = append(lines, "iteration: "+itNum(node.IterationIndex))
	}
	lines = append(lines, aggregateChildStatuses(node.Children)...)
	return strings.Join(lines, "\n")
}

func uiFormText(node *StepNode) string {
	var lines []string
	if node.StaticUITitle != "" {
		lines = append(lines, node.StaticUITitle)
	}
	if node.StaticUIBody != "" {
		lines = append(lines, node.StaticUIBody)
	}
	for _, input := range node.StaticUIInputs {
		line := input.Prompt
		if line == "" {
			line = input.ID
		}
		if len(input.Options) > 0 {
			line += ": " + strings.Join(input.Options, ", ")
		}
		if input.Default != "" {
			line += " (default: " + input.Default + ")"
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(node.StaticUIActions) > 0 {
		actions := make([]string, 0, len(node.StaticUIActions))
		for _, action := range node.StaticUIActions {
			actions = append(actions, action.Label)
		}
		lines = append(lines, "actions: "+strings.Join(actions, ", "))
	}
	if len(lines) == 0 && node.StaticMode != model.ModeUI {
		return "definition unavailable"
	}
	return strings.Join(lines, "\n")
}

func staticContainerText(node *StepNode) string {
	switch node.Type {
	case NodeLoop:
		if node.StaticLoopOver != "" {
			return "loop: for-each\nover: " + node.StaticLoopOver
		}
		if node.StaticLoopMax != nil {
			return fmt.Sprintf("loop: counted\nmax: %d", *node.StaticLoopMax)
		}
	case NodeIteration:
		return "iteration: " + itNum(node.IterationIndex)
	case NodeGroup:
		return fmt.Sprintf("configured steps: %d", len(node.Children))
	}
	return "pending"
}

func aggregateChildStatuses(children []*StepNode) []string {
	counts := map[NodeStatus]int{}
	for _, child := range children {
		counts[child.Status]++
	}
	var lines []string
	for _, status := range []NodeStatus{StatusSuccess, StatusWarning, StatusInProgress, StatusPending, StatusSkipped, StatusFailed} {
		if counts[status] > 0 {
			lines = append(lines, fmt.Sprintf("%d %s", counts[status], statusLabel(status)))
		}
	}
	return lines
}

func plainParams(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+": "+params[key])
	}
	return lines
}

func currentCommand(node *StepNode) string {
	if node.InterpolatedCommand != "" {
		return node.InterpolatedCommand
	}
	return node.StaticCommand
}

func currentPrompt(node *StepNode) string {
	if node.InterpolatedPrompt != "" {
		return node.InterpolatedPrompt
	}
	return node.StaticPrompt
}

func nodeTypeLabel(t NodeType) string {
	switch t {
	case NodeShell:
		return "shell"
	case NodeScript:
		return "script"
	case NodeUI:
		return "ui"
	case NodeHeadlessAgent:
		return "agent"
	case NodeInteractiveAgent:
		return "interactive agent"
	case NodeAgentCall:
		return "agent call"
	case NodeSubWorkflow:
		return "sub-workflow"
	case NodeLoop:
		return "loop"
	case NodeIteration:
		return "iteration"
	case NodeGroup:
		return "group"
	}
	return "step"
}

func (doc detailDocument) renderScreen() []string {
	var lines []string
	for _, header := range doc.header {
		lines = append(lines, tuistyle.NormalStyle.Render(tuistyle.Sanitize(header)))
	}
	for _, section := range doc.sections {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		style := detailRailStyle(section.kind)
		lines = append(lines, style.Render("▎ "+tuistyle.Sanitize(section.label)))
		for _, line := range wrappedPlainLines(section.body, doc.width-3) {
			lines = append(lines, style.Render("▎ ")+tuistyle.NormalStyle.Render(line))
		}
	}
	return lines
}

func (doc detailDocument) renderCopy() string {
	var lines []string
	lines = append(lines, doc.header...)
	for _, section := range doc.sections {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, section.label)
		if section.copy != "" {
			lines = append(lines, section.copy)
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func detailRailStyle(kind detailRailKind) lipgloss.Style {
	switch kind {
	case detailRailPrevious:
		return lipgloss.NewStyle().Foreground(tuistyle.InactiveAmber)
	case detailRailInput:
		return lipgloss.NewStyle().Foreground(tuistyle.AccentCyan)
	case detailRailError:
		return lipgloss.NewStyle().Foreground(tuistyle.FailedRed)
	default:
		return lipgloss.NewStyle().Foreground(tuistyle.SuccessGreen)
	}
}

func wrappedPlainLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	for _, line := range strings.Split(tuistyle.Sanitize(text), "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapLine(line, width)...)
	}
	return lines
}
