package runview

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

// makeNode is a minimal StepNode factory for logview tests.
func makeNode(id string, t NodeType, status NodeStatus) *StepNode {
	return &StepNode{ID: id, Type: t, Status: status}
}

// noResolver is an empty ResolverConfig used in tests.
var noResolver ResolverConfig

// TestBuildLogLines_FlatChildren verifies that two shell steps each produce at
// least one line and their ranges remain non-overlapping even with an
// inter-block spacer line between them.
func TestBuildLogLines_FlatChildren(t *testing.T) {
	step1 := makeNode("step1", NodeShell, StatusSuccess)
	step2 := makeNode("step2", NodeShell, StatusSuccess)

	lines, ranges := buildLogLines(
		[]*StepNode{step1, step2},
		nil,
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	if len(lines) == 0 {
		t.Fatal("expected lines, got none")
	}
	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d", len(ranges))
	}
	r0, r1 := ranges[0], ranges[1]
	if r0.node != step1 {
		t.Errorf("ranges[0].node mismatch")
	}
	if r1.node != step2 {
		t.Errorf("ranges[1].node mismatch")
	}
	if r0.endLine+1 != r1.startLine {
		t.Errorf("expected one spacer line between ranges: r0.end=%d r1.start=%d", r0.endLine, r1.startLine)
	}
	if r0.startLine >= r0.endLine {
		t.Errorf("range 0 has no lines: start=%d end=%d", r0.startLine, r0.endLine)
	}
	if r1.startLine >= r1.endLine {
		t.Errorf("range 1 has no lines: start=%d end=%d", r1.startLine, r1.endLine)
	}
	if got := stripANSI(lines[r1.startLine-1]); got != "" {
		t.Fatalf("line before second block should be the inter-block blank line, got %q", got)
	}
	if sep := stripANSI(lines[r1.startLine]); !strings.Contains(sep, "step2") {
		t.Fatalf("second block should start at its separator, got %q", sep)
	}
}

// TestBuildLogLines_SubWorkflowNested verifies that a sub-workflow's started
// children are rendered inline under the parent block, and that ranges include
// both the parent and child nodes.
func TestBuildLogLines_SubWorkflowNested(t *testing.T) {
	sw := makeNode("deploy", NodeSubWorkflow, StatusInProgress)
	child := makeNode("run-tests", NodeShell, StatusSuccess)
	child.Parent = sw
	sw.Children = []*StepNode{child}

	lines, ranges := buildLogLines(
		[]*StepNode{sw},
		nil,
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	// Expect at least 2 ranges: parent + child.
	if len(ranges) < 2 {
		t.Fatalf("expected at least 2 ranges, got %d", len(ranges))
	}
	// Parent range should cover only the parent's own block, not descendants.
	parentRange := ranges[0]
	if parentRange.node != sw {
		t.Errorf("ranges[0] should be the sub-workflow node")
	}
	if parentRange.endLine >= len(lines) {
		t.Errorf("parent range should stop before child lines: end=%d totalLines=%d", parentRange.endLine, len(lines))
	}

	// Child range must start after the parent block and not overlap it.
	childRange := ranges[1]
	if childRange.node != child {
		t.Errorf("ranges[1] should be the child node")
	}
	if childRange.startLine < parentRange.endLine {
		t.Errorf("child should start after parent block: child.start=%d parent.end=%d", childRange.startLine, parentRange.endLine)
	}

	// Child lines should be indented (indent=1 → 2 spaces prefix).
	childLines := lines[childRange.startLine:childRange.endLine]
	for _, l := range childLines {
		plain := stripANSI(l)
		if plain != "" && !strings.HasPrefix(plain, "  ") {
			t.Errorf("child line not indented: %q", plain)
		}
	}
}

// TestBuildLogLines_LoopWithIterations verifies that two started iterations
// appear inline under the loop header and their ranges are nested.
func TestBuildLogLines_LoopWithIterations(t *testing.T) {
	loop := makeNode("process", NodeLoop, StatusInProgress)
	iter0 := makeNode("process[0]", NodeIteration, StatusSuccess)
	iter1 := makeNode("process[1]", NodeIteration, StatusSuccess)
	iter0.Parent = loop
	iter1.Parent = loop
	loop.Children = []*StepNode{iter0, iter1}

	lines, ranges := buildLogLines(
		[]*StepNode{loop},
		nil,
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	if len(lines) == 0 {
		t.Fatal("no lines emitted")
	}
	// Expect loop + 2 iteration ranges.
	if len(ranges) < 3 {
		t.Fatalf("expected at least 3 ranges (loop + 2 iters), got %d", len(ranges))
	}
	loopRange := ranges[0]
	if loopRange.node != loop {
		t.Errorf("ranges[0] should be loop node")
	}
	// Both iterations should come after the loop block without overlapping it.
	for i := 1; i < 3; i++ {
		r := ranges[i]
		if r.startLine < loopRange.endLine {
			t.Errorf("iteration range[%d] overlaps the loop header block", i)
		}
	}
}

// TestBuildLogLines_PendingStepSkipped verifies that pending steps that are
// not selected are not emitted to the log.
func TestBuildLogLines_PendingStepSkipped(t *testing.T) {
	done := makeNode("setup", NodeShell, StatusSuccess)
	pending := makeNode("deploy", NodeShell, StatusPending)

	lines, ranges := buildLogLines(
		[]*StepNode{done, pending},
		nil, // no ghost
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	if len(ranges) != 1 {
		t.Fatalf("expected 1 range (only done step), got %d", len(ranges))
	}
	if ranges[0].node != done {
		t.Errorf("expected done node in range")
	}
	_ = lines
}

// TestBuildLogLines_GhostBlock verifies that a pending step that is selected
// produces a ghost block at the correct position with a dashed separator.
func TestBuildLogLines_GhostBlock(t *testing.T) {
	done := makeNode("setup", NodeShell, StatusSuccess)
	pending := makeNode("deploy", NodeShell, StatusPending)

	lines, ranges := buildLogLines(
		[]*StepNode{done, pending},
		pending, // pending is the ghost
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges (done + ghost), got %d", len(ranges))
	}
	ghostRange := ranges[1]
	if ghostRange.node != pending {
		t.Errorf("ranges[1] should be ghost node")
	}
	if ghostRange.startLine <= ranges[0].endLine-1 {
		t.Errorf("ghost block should start after done block")
	}

	// Ghost separator uses "- " dashes, not "═" or "─".
	ghostSep := firstNonBlankLine(lines, ghostRange)
	if !strings.Contains(ghostSep, "- ") {
		t.Errorf("ghost separator should contain '- ', got: %q", ghostSep)
	}
}

func TestBuildLogLines_GhostSubWorkflow_ShowsResolvedPathAndRawParams(t *testing.T) {
	cfg := ResolverConfig{WorkflowsRoot: "/repo/workflows", RepoRoot: "/repo"}
	done := makeNode("setup", NodeShell, StatusSuccess)
	pending := makeNode("verify", NodeSubWorkflow, StatusPending)
	pending.StaticWorkflow = "openspec/verify.yaml"
	pending.StaticWorkflowPath = "/repo/workflows/openspec/verify.yaml"
	pending.StaticParams = map[string]string{"task_file": "{{task_file}}"}

	lines, ranges := buildLogLines(
		[]*StepNode{done, pending},
		pending,
		80,
		make(map[string]bool),
		0, false, cfg,
	)

	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges (done + pending ghost), got %d", len(ranges))
	}

	ghostLines := lines[ranges[1].startLine:ranges[1].endLine]
	joined := stripANSI(strings.Join(ghostLines, "\n"))
	if !strings.Contains(joined, "workflow: openspec:verify") {
		t.Fatalf("ghost block should show canonical workflow path, got:\n%s", joined)
	}
	if !strings.Contains(joined, "task_file: {{task_file}}") {
		t.Fatalf("ghost block should show raw params, got:\n%s", joined)
	}
}

// TestBuildLogLines_SeparatorDepth verifies that separators use heavier
// characters at shallower nesting levels. Depth 0 uses "═", depth 1 uses "─".
func TestBuildLogLines_SeparatorDepth(t *testing.T) {
	sw := makeNode("outer", NodeSubWorkflow, StatusInProgress)
	inner := makeNode("inner-step", NodeShell, StatusSuccess)
	inner.Parent = sw
	sw.Children = []*StepNode{inner}

	lines, ranges := buildLogLines(
		[]*StepNode{sw},
		nil,
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	if len(ranges) < 2 {
		t.Fatalf("expected at least 2 ranges, got %d", len(ranges))
	}

	// Depth-0 separator for the sub-workflow.
	outerSep := firstNonBlankLine(lines, ranges[0])
	if !strings.Contains(outerSep, "═") {
		t.Errorf("depth-0 separator should contain '═', got: %q", outerSep)
	}

	// Depth-1 separator for the inner step.
	innerSep := firstNonBlankLine(lines, ranges[1])
	if strings.Contains(innerSep, "═") {
		t.Errorf("depth-1 separator should not contain '═', got: %q", innerSep)
	}
	if !strings.Contains(innerSep, "─") {
		t.Errorf("depth-1 separator should contain '─', got: %q", innerSep)
	}
}

func TestBuildLogLines_DeepNesting_DecreasesSeparatorWeight(t *testing.T) {
	level0 := makeNode("level0", NodeSubWorkflow, StatusInProgress)
	level1 := makeNode("level1", NodeSubWorkflow, StatusInProgress)
	level2 := makeNode("level2", NodeLoop, StatusInProgress)
	level3 := makeNode("level3", NodeIteration, StatusInProgress)
	leaf := makeNode("leaf", NodeShell, StatusSuccess)

	level1.Parent = level0
	level2.Parent = level1
	level3.Parent = level2
	leaf.Parent = level3

	level0.Children = []*StepNode{level1}
	level1.Children = []*StepNode{level2}
	level2.Children = []*StepNode{level3}
	level3.Children = []*StepNode{leaf}

	lines, ranges := buildLogLines(
		[]*StepNode{level0},
		nil,
		80,
		make(map[string]bool),
		0, false, noResolver,
	)

	if len(ranges) != 5 {
		t.Fatalf("expected 5 ranges, got %d", len(ranges))
	}

	level0Sep := firstNonBlankLine(lines, ranges[0])
	level1Sep := firstNonBlankLine(lines, ranges[1])
	level2Sep := firstNonBlankLine(lines, ranges[2])
	level3Sep := firstNonBlankLine(lines, ranges[3])
	leafSep := firstNonBlankLine(lines, ranges[4])

	if !strings.Contains(level0Sep, "═") {
		t.Fatalf("depth 0 separator should contain ═, got %q", level0Sep)
	}
	if !strings.Contains(level1Sep, "─") || strings.Contains(level1Sep, "═") {
		t.Fatalf("depth 1 separator should use ─ only, got %q", level1Sep)
	}
	if !strings.Contains(level2Sep, "─") || strings.Contains(level2Sep, "═") {
		t.Fatalf("depth 2 separator should use lighter ─ only, got %q", level2Sep)
	}
	if !strings.Contains(level3Sep, "·") {
		t.Fatalf("depth 3 separator should contain ·, got %q", level3Sep)
	}
	if !strings.Contains(leafSep, "·") {
		t.Fatalf("depth 4 separator should contain ·, got %q", leafSep)
	}

	for i := 1; i < len(ranges); i++ {
		if ranges[i-1].startLine >= ranges[i].startLine {
			t.Fatalf("range %d should start after range %d", i, i-1)
		}
	}
}

// TestBuildLogLines_LargeOutputTruncated verifies that output exceeding the
// tail cap is truncated (truncateOutput applied).
func TestBuildLogLines_LargeOutputTruncated(t *testing.T) {
	step := makeNode("build", NodeShell, StatusSuccess)
	// Build output exceeding maxOutputLines (2000).
	var sb strings.Builder
	for range 2500 {
		sb.WriteString("line of output\n")
	}
	step.Stdout = sb.String()

	lines, _ := buildLogLines(
		[]*StepNode{step},
		nil,
		80,
		make(map[string]bool), // loadedFull = false → truncation applies
		0, false, noResolver,
	)

	// The block should include a truncation banner.
	found := false
	for _, l := range lines {
		if strings.Contains(stripANSI(l), "showing last") || strings.Contains(stripANSI(l), "lines") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected truncation banner in output, lines: %v", lines)
	}
}

func TestRenderHeadlessBlock_StripsTrailingPromptBlankBeforeDuration(t *testing.T) {
	duration := int64(123000)
	exitCode := 0
	node := &StepNode{
		ID:                 "summarize",
		Type:               NodeHeadlessAgent,
		Status:             StatusSuccess,
		InterpolatedPrompt: "First line\nSecond line\n",
		DurationMs:         &duration,
		ExitCode:           &exitCode,
	}

	lines := renderHeadlessBlock(node, 0, 80, false, 0, false)
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = stripANSI(line)
	}

	durationIdx := -1
	for i, line := range plain {
		if strings.Contains(line, "duration: ") {
			durationIdx = i
			break
		}
	}
	if durationIdx < 0 {
		t.Fatalf("expected duration line in block, got:\n%s", strings.Join(plain, "\n"))
	}

	blankCount := 0
	for i := durationIdx - 1; i >= 0 && plain[i] == ""; i-- {
		blankCount++
	}
	if blankCount != 1 {
		t.Fatalf("expected exactly one blank line before duration, got %d in:\n%s", blankCount, strings.Join(plain, "\n"))
	}
}

func TestRenderHeadlessBlock_HeaderOrderAndUnknownModelFallback(t *testing.T) {
	node := &StepNode{
		ID:                 "implement",
		Type:               NodeHeadlessAgent,
		Status:             StatusInProgress,
		AgentProfile:       "implementor",
		AgentCLI:           "claude",
		StaticSession:      model.SessionResume,
		SessionID:          "sess-123",
		InterpolatedPrompt: "Do it",
	}

	lines := renderHeadlessBlock(node, 0, 80, false, 0, false)
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = stripANSI(line)
	}

	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "model: (unknown)") {
		t.Fatalf("expected explicit unknown model fallback, got:\n%s", joined)
	}

	idxAgent := lineIndexContaining(plain, "agent: implementor")
	idxCLI := lineIndexContaining(plain, "cli: claude")
	idxModel := lineIndexContaining(plain, "model: (unknown)")
	idxSession := lineIndexContaining(plain, "session: resume")
	idxSessionID := lineIndexContaining(plain, "session id: sess-123")

	if idxAgent < 0 || idxCLI < 0 || idxModel < 0 || idxSession < 0 || idxSessionID < 0 {
		t.Fatalf("missing expected header lines:\n%s", joined)
	}
	if idxAgent >= idxCLI || idxCLI >= idxModel || idxModel >= idxSession || idxSession >= idxSessionID {
		t.Fatalf("unexpected header order:\n%s", joined)
	}
}

func TestRenderAgentBlocks_HideResumeHintWhenRunActive(t *testing.T) {
	headless := &StepNode{
		ID:        "implement",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		SessionID: "sess-123",
	}
	interactive := &StepNode{
		ID:        "review",
		Type:      NodeInteractiveAgent,
		Status:    StatusSuccess,
		SessionID: "sess-456",
	}

	for name, lines := range map[string][]string{
		"headless":    renderHeadlessBlock(headless, 0, 80, false, 0, true),
		"interactive": renderInteractiveBlock(interactive, 0, 80, 0, true),
	} {
		var plain []string
		for _, line := range lines {
			plain = append(plain, stripANSI(line))
		}
		if joined := strings.Join(plain, "\n"); strings.Contains(joined, "resume session") {
			t.Fatalf("%s block should hide resume hint while run is active:\n%s", name, joined)
		}
	}
}

func TestRenderHeadlessBlock_ShowsSingleGlyphSpinnerBelowStreamingOutput(t *testing.T) {
	node := &StepNode{
		ID:         "implement",
		Type:       NodeHeadlessAgent,
		Status:     StatusInProgress,
		AgentCLI:   "claude",
		AgentModel: "sonnet",
		Stdout:     "streaming output",
	}

	lines := renderHeadlessBlock(node, 0, 80, false, 0, true)
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = stripANSI(line)
	}

	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "streaming output") {
		t.Fatalf("expected streamed output in block, got:\n%s", joined)
	}
	if got := lastNonBlankLine(plain); got != "⠋" {
		t.Fatalf("last non-blank line = %q, want %q in:\n%s", got, "⠋", joined)
	}
}

func TestFormatDuration_InsertsSpacesBetweenUnits(t *testing.T) {
	if got := formatDuration(123000); got != "2m 3s" {
		t.Fatalf("formatDuration(123000) = %q, want %q", got, "2m 3s")
	}
	if got := formatDuration(61000); got != "1m 1s" {
		t.Fatalf("formatDuration(61000) = %q, want %q", got, "1m 1s")
	}
}

// stripANSI removes ANSI escape codes for plain-text assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func firstNonBlankLine(lines []string, r stepLineRange) string {
	for i := r.startLine; i < r.endLine; i++ {
		if plain := stripANSI(lines[i]); plain != "" {
			return plain
		}
	}
	return ""
}

func lastNonBlankLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] != "" {
			return lines[i]
		}
	}
	return ""
}

func lineIndexContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}
