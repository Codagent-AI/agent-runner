package runview

import (
	"strings"
	"testing"
)

// makeNode is a minimal StepNode factory for logview tests.
func makeNode(id string, t NodeType, status NodeStatus) *StepNode {
	return &StepNode{ID: id, Type: t, Status: status}
}

// noResolver is an empty ResolverConfig used in tests.
var noResolver ResolverConfig

// TestBuildLogLines_FlatChildren verifies that two shell steps each produce at
// least one line and their ranges are contiguous and non-overlapping.
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
	if r0.endLine != r1.startLine {
		t.Errorf("ranges not contiguous: r0.end=%d r1.start=%d", r0.endLine, r1.startLine)
	}
	if r0.startLine >= r0.endLine {
		t.Errorf("range 0 has no lines: start=%d end=%d", r0.startLine, r0.endLine)
	}
	if r1.startLine >= r1.endLine {
		t.Errorf("range 1 has no lines: start=%d end=%d", r1.startLine, r1.endLine)
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
	// Parent range must span all lines including child.
	parentRange := ranges[0]
	if parentRange.node != sw {
		t.Errorf("ranges[0] should be the sub-workflow node")
	}
	if parentRange.endLine != len(lines) {
		t.Errorf("parent range should span to end of lines: end=%d totalLines=%d", parentRange.endLine, len(lines))
	}

	// Child range must be nested within parent.
	childRange := ranges[1]
	if childRange.node != child {
		t.Errorf("ranges[1] should be the child node")
	}
	if childRange.startLine <= parentRange.startLine {
		t.Errorf("child should start after parent header: child.start=%d parent.start=%d", childRange.startLine, parentRange.startLine)
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
	// Both iterations should be nested inside loop's range.
	for i := 1; i < 3; i++ {
		r := ranges[i]
		if r.startLine < loopRange.startLine || r.endLine > loopRange.endLine {
			t.Errorf("iteration range[%d] not inside loop range", i)
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
	ghostSep := stripANSI(lines[ghostRange.startLine])
	if !strings.Contains(ghostSep, "- ") {
		t.Errorf("ghost separator should contain '- ', got: %q", ghostSep)
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
	outerSep := stripANSI(lines[ranges[0].startLine])
	if !strings.Contains(outerSep, "═") {
		t.Errorf("depth-0 separator should contain '═', got: %q", outerSep)
	}

	// Depth-1 separator for the inner step.
	innerSep := stripANSI(lines[ranges[1].startLine])
	if strings.Contains(innerSep, "═") {
		t.Errorf("depth-1 separator should not contain '═', got: %q", innerSep)
	}
	if !strings.Contains(innerSep, "─") {
		t.Errorf("depth-1 separator should contain '─', got: %q", innerSep)
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
