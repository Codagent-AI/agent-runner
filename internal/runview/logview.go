package runview

// stepLineRange is now the one selected-detail document span. It remains a
// small layout value for scroll clamping; it never maps one execution's
// scrolling back into tree selection.
type stepLineRange struct {
	node      *StepNode
	startLine int
	endLine   int
}

type stepLineAnchor struct {
	stepKey           string
	lineOffsetInBlock int
}

func buildSelectedDetailLines(selected *StepNode, bodyWidth int, loadedFull map[string]bool, pulsePhase float64, runActive bool, resolverCfg ResolverConfig) ([]string, []stepLineRange) {
	if selected == nil {
		return nil, nil
	}
	lines := buildDetailDocument(selected, detailBuildOptions{
		width:       bodyWidth,
		loadedFull:  loadedFull[selected.NodeKey()],
		pulsePhase:  pulsePhase,
		runActive:   runActive,
		resolverCfg: resolverCfg,
	}).renderScreen()
	return lines, []stepLineRange{{node: selected, startLine: 0, endLine: len(lines)}}
}
