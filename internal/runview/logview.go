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
// loadedFull is keyed by StepNode.NodeKey() so it is unique even when
// multiple nodes share the same ID (e.g. iteration nodes) and survives
// equivalent tree rebuilds.
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
		if len(*lines) > 0 {
			*lines = append(*lines, "")
		}
		startLine := len(*lines)

		var blockLines []string
		if isGhost {
			blockLines = renderGhostBlock(child, indent, bodyWidth, resolverCfg)
		} else {
			switch child.Type {
			case NodeShell:
				blockLines = renderShellBlock(child, indent, bodyWidth, loadedFull[child.NodeKey()])
			case NodeHeadlessAgent:
				blockLines = renderHeadlessBlock(child, indent, bodyWidth, loadedFull[child.NodeKey()], pulsePhase, running)
			case NodeInteractiveAgent:
				blockLines = renderInteractiveBlock(child, indent, bodyWidth, pulsePhase, running)
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

		*ranges = append(*ranges, stepLineRange{node: child, startLine: startLine, endLine: len(*lines)})

		// Recurse for container types (not ghost blocks).
		if !isGhost && child.IsContainer() {
			var nested []*StepNode
			if child.Type == NodeIteration {
				nested = child.Drilldown().Children
			} else {
				nested = child.Children
			}
			if len(nested) > 0 {
				buildLogLinesRecurse(nested, pendingSelected, bodyWidth, loadedFull, pulsePhase, running, resolverCfg, indent+1, lines, ranges)
			}
		}

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
func renderGhostBlock(node *StepNode, indent, width int, resolverCfg ResolverConfig) []string {
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
		wfName := CanonicalName(node.StaticWorkflowPath, resolverCfg)
		if wfName != "" {
			lines = append(lines, tuistyle.DimStyle.Render(blockDimStr("workflow", wfName)))
		} else if node.StaticWorkflow != "" {
			lines = append(lines, tuistyle.DimStyle.Render(blockDimStr("workflow", node.StaticWorkflow)))
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
	reps := (fillLen + len(dashPat) - 1) / len(dashPat)
	f := runewidth.Truncate(strings.Repeat(dashPat, reps), fillLen, "")
	return prefix + f + suffix
}
