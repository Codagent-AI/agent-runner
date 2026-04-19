package runview

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

// stepLineRange records the log-line span of one StepNode in the rendered log.
// startLine is inclusive, endLine exclusive (0-based into the log's line slice).
// Nested nodes are included so scroll-sync can map any log offset to a node.
type stepLineRange struct {
	node      *StepNode
	startLine int
	endLine   int
}

// stepLineAnchor records where a scroll position is anchored so that after a
// resize the same point in a block stays visible.
type stepLineAnchor struct {
	stepKey           string
	lineOffsetInBlock int
}

// buildLogLines assembles the continuous log pane for the current drill-in
// level. It walks children in execution order, emitting a block for every
// started step and a ghost block for a pending step that is currently selected.
// Container children (sub-workflow, loop, iteration) have their started
// children rendered inline beneath the parent header at indent+1.
//
// loadedFull is keyed by node.ID (not pointer) so it survives across rebuilds.
func buildLogLines(
	children []*StepNode,
	pendingSelected *StepNode,
	bodyWidth int,
	loadedFull map[string]bool,
	pulsePhase float64,
	running bool,
	resolverCfg ResolverConfig,
) (lines []string, ranges []stepLineRange) {
	buildLogLinesRecurse(children, pendingSelected, bodyWidth, loadedFull, pulsePhase, running, resolverCfg, 0, &lines, &ranges)
	return
}

func buildLogLinesRecurse(
	children []*StepNode,
	pendingSelected *StepNode,
	bodyWidth int,
	loadedFull map[string]bool,
	pulsePhase float64,
	running bool,
	resolverCfg ResolverConfig,
	indent int,
	lines *[]string,
	ranges *[]stepLineRange,
) {
	indent2 := strings.Repeat(" ", 2*indent)

	for _, child := range children {
		isPending := child.Status == StatusPending
		isGhost := isPending && child == pendingSelected

		if isPending && !isGhost {
			continue
		}

		startLine := len(*lines)

		var blockLines []string
		if isGhost {
			blockLines = renderGhostBlock(child, indent, bodyWidth)
		} else {
			switch child.Type {
			case NodeShell:
				blockLines = renderShellBlock(child, indent, bodyWidth, loadedFull[child.ID])
			case NodeHeadlessAgent:
				blockLines = renderHeadlessBlock(child, indent, bodyWidth, loadedFull[child.ID], pulsePhase, running)
			case NodeInteractiveAgent:
				blockLines = renderInteractiveBlock(child, indent, bodyWidth, running)
			case NodeSubWorkflow:
				blockLines = renderSubWorkflowBlock(child, indent, bodyWidth, resolverCfg)
			case NodeLoop:
				blockLines = renderLoopBlock(child, indent, bodyWidth)
			case NodeIteration:
				blockLines = renderIterationBlock(child, indent, bodyWidth)
			}
		}

		for _, line := range blockLines {
			*lines = append(*lines, indent2+line)
		}

		rangeIdx := len(*ranges)
		*ranges = append(*ranges, stepLineRange{node: child, startLine: startLine, endLine: len(*lines)})

		// Recurse for container types (not ghost blocks).
		if !isGhost && child.IsContainer() {
			var nested []*StepNode
			switch child.Type {
			case NodeSubWorkflow:
				nested = child.Children
			case NodeLoop:
				nested = child.Children
			case NodeIteration:
				// Use Drilldown for auto-flattened iterations.
				target := child.Drilldown()
				nested = target.Children
			}
			if len(nested) > 0 {
				buildLogLinesRecurse(nested, pendingSelected, bodyWidth, loadedFull, pulsePhase, running, resolverCfg, indent+1, lines, ranges)
			}
		}

		// Update this node's endLine to span all descendants.
		(*ranges)[rangeIdx].endLine = len(*lines)
	}
}

// renderSeparator generates a full-width separator line for a block.
// The separator fills to contentWidth using depth-appropriate characters.
//
//	Depth 0:  ══ name ══════════════ glyph ═
//	Depth 1:  ── name ─────────────── glyph ─
//	Depth 2:  ─ name ──────────────────── glyph
//	Depth 3+: · name ·························· glyph
func renderSeparator(name, glyph string, depth, contentWidth int) string {
	var sepChar, prefix, suffix string

	switch depth {
	case 0:
		sepChar = "═"
		prefix = "══ " + name + " "
		suffix = " " + glyph + " ═"
	case 1:
		sepChar = "─"
		prefix = "── " + name + " "
		suffix = " " + glyph + " ─"
	case 2:
		sepChar = "─"
		prefix = "─ " + name
		suffix = " " + glyph
	default:
		sepChar = "·"
		prefix = "· " + name
		suffix = " " + glyph
	}

	usedWidth := runewidth.StringWidth(prefix) + runewidth.StringWidth(suffix)
	fillLen := contentWidth - usedWidth
	var fill string
	if fillLen > 0 {
		fill = strings.Repeat(sepChar, fillLen)
	}
	return prefix + fill + suffix
}

// renderGhostBlock renders a dim placeholder block for a pending step that is
// currently selected. It shows only statically knowable fields (no runtime
// data) with a dashed separator so it is visually distinct from real blocks.
func renderGhostBlock(node *StepNode, indent, width int) []string {
	contentWidth := width - 2*indent
	if contentWidth <= 0 {
		return nil
	}

	glyph := blockTypeGlyph(node.Type)
	sep := buildGhostSeparator(node.ID, glyph, contentWidth)

	var lines []string
	lines = append(lines, tuistyle.DimStyle.Render(sep))

	switch node.Type {
	case NodeShell:
		cmd := node.StaticCommand
		if cmd != "" {
			for _, l := range renderWrappedText("$ "+cmd, contentWidth) {
				lines = append(lines, tuistyle.DimStyle.Render(l))
			}
		}
	case NodeHeadlessAgent, NodeInteractiveAgent:
		if node.StaticAgent != "" {
			lines = append(lines, tuistyle.DimStyle.Render(blockDimStr("agent", node.StaticAgent)))
		}
		if node.StaticPrompt != "" {
			lines = append(lines, tuistyle.DimStyle.Render(blockLabelStr("prompt:")))
			for _, l := range renderWrappedText(node.StaticPrompt, contentWidth) {
				lines = append(lines, tuistyle.DimStyle.Render(l))
			}
		}
	case NodeSubWorkflow:
		wfName := node.StaticWorkflow
		if wfName != "" {
			lines = append(lines, tuistyle.DimStyle.Render(blockDimStr("workflow", bareWorkflowName(wfName))))
		}
		for _, l := range sortedParams(node.StaticParams) {
			lines = append(lines, tuistyle.DimStyle.Render(l))
		}
	case NodeLoop:
		loopKind := "counted"
		if node.StaticLoopOver != "" {
			loopKind = "for-each"
		}
		lines = append(lines, tuistyle.DimStyle.Render(blockDimStr("loop", loopKind)))
	case NodeIteration:
		lines = append(lines, tuistyle.DimStyle.Render(blockDimStr("iteration", itNum(node.IterationIndex))))
	}

	return lines
}

func buildGhostSeparator(name, glyph string, contentWidth int) string {
	const dashPat = "- "
	prefix := name + " "
	suffix := " " + glyph
	usedWidth := runewidth.StringWidth(prefix) + runewidth.StringWidth(suffix)
	fillLen := contentWidth - usedWidth
	if fillLen <= 0 {
		return prefix + suffix
	}
	var fill strings.Builder
	for fill.Len() < fillLen {
		fill.WriteString(dashPat)
	}
	f := runewidth.Truncate(fill.String(), fillLen, "")
	return prefix + f + suffix
}
