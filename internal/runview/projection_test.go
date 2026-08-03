package runview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func TestProjectTreeExpandsSelectedAndActiveBranchesWithDualFocusWindow(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	container := &StepNode{ID: "container", Type: NodeGroup, Parent: root}
	root.Children = []*StepNode{container}
	for i := 0; i < 9; i++ {
		child := &StepNode{ID: string(rune('a' + i)), Type: NodeGroup, Parent: container}
		leaf := &StepNode{ID: child.ID + "-leaf", Type: NodeShell, Parent: child}
		child.Children = []*StepNode{leaf}
		container.Children = append(container.Children, child)
	}

	selected := container.Children[1].Children[0]
	active := container.Children[7].Children[0]
	rows := projectTree(root, selected, active)

	var got []string
	for _, row := range rows {
		if row.kind == treeRowNode {
			got = append(got, row.node.ID)
		} else {
			got = append(got, row.omissionLabel())
		}
	}
	want := []string{"container", "a", "b", "b-leaf", "c", "… 3 between", "g", "h", "h-leaf", "… 1 later"}
	if diff := strings.Join(got, "\n") + "\n"; diff != strings.Join(want, "\n")+"\n" {
		t.Fatalf("projected rows mismatch (-want +got):\nwant %q\n got %q", want, got)
	}
	for _, row := range rows {
		if row.kind == treeRowOmission && row.selectable() {
			t.Fatal("omission row must not be selectable")
		}
	}
}

func TestProjectTreeShowsAllChildrenInDrilledScope(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	group := &StepNode{ID: "group", Type: NodeGroup, Parent: root}
	root.Children = []*StepNode{group}
	for i := 0; i < 7; i++ {
		group.Children = append(group.Children, &StepNode{ID: string(rune('a' + i)), Type: NodeShell, Parent: group})
	}

	rows := projectTree(group, group.Children[6], nil)
	if len(rows) != 7 {
		t.Fatalf("drilled scope has %d rows, want all 7 direct children", len(rows))
	}
	for i, row := range rows {
		if row.kind != treeRowNode || row.node != group.Children[i] {
			t.Fatalf("row %d = %#v, want direct child %q", i, row, group.Children[i].ID)
		}
	}
}

func TestProjectTreeSelectedCompletedContainerShowsFinalFiveChildren(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	group := &StepNode{ID: "group", Type: NodeGroup, Status: StatusSuccess, Parent: root}
	root.Children = []*StepNode{group}
	for i := 0; i < 7; i++ {
		group.Children = append(group.Children, &StepNode{ID: string(rune('a' + i)), Type: NodeShell, Parent: group})
	}

	rows := projectTree(root, group, nil)
	var got []string
	for _, row := range rows {
		if row.kind == treeRowNode {
			got = append(got, row.node.ID)
		} else {
			got = append(got, row.omissionLabel())
		}
	}
	want := []string{"group", "… 2 earlier", "c", "d", "e", "f", "g"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

func TestTreePaneLayoutKeepsFullRowBeyondHalfWhenDetailFits(t *testing.T) {
	rows := []string{"▶  ○  this-is-a-very-long-workflow-step-name-that-needs-room  ◇"}
	layout := measureTreePaneLayout(100, rows, 0)
	if layout.sidebar <= 50 {
		t.Fatalf("sidebar = %d, want more than half the terminal", layout.sidebar)
	}
	if layout.detail < 20 {
		t.Fatalf("detail = %d, want at least 20", layout.detail)
	}
	if strings.Contains(tuistyle.Sanitize(layout.rows[0]), "…") {
		t.Fatalf("row was truncated despite room: %q", layout.rows[0])
	}
}

func TestTreePaneLayoutDoesNotShrinkSettledSidebarAndStaysValidWhenNarrow(t *testing.T) {
	settled := measureTreePaneLayout(100, []string{strings.Repeat("x", 60)}, 0).sidebar
	stable := measureTreePaneLayout(100, []string{"short"}, settled)
	if stable.sidebar != settled {
		t.Fatalf("sidebar shrank from %d to %d", settled, stable.sidebar)
	}
	narrow := measureTreePaneLayout(3, []string{"▶  ○  very-long-name  ◇"}, 0)
	if narrow.sidebar < 0 || narrow.detail < 0 {
		t.Fatalf("narrow layout has negative dimension: %#v", narrow)
	}
}

func TestModelRestoresNodeSelectionByKeyAfterTreeReplacement(t *testing.T) {
	build := func() (*Tree, *StepNode) {
		root := &StepNode{ID: "root", Type: NodeRoot}
		group := &StepNode{ID: "group", Type: NodeGroup, Parent: root}
		leaf := &StepNode{ID: "leaf", Type: NodeShell, Parent: group}
		group.Children = []*StepNode{leaf}
		root.Children = []*StepNode{group}
		return &Tree{Root: root}, leaf
	}
	first, selected := build()
	m := newTestModel(first, FromInspect)
	m.setSelected(selected)
	second, replacement := build()
	m.tree = second
	if got := m.selectedNode(); got != replacement {
		t.Fatalf("restored selected node = %p, want rebuilt node %p", got, replacement)
	}
}

func TestBuildSelectedDetailLinesDoesNotIncludeContainerDescendants(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	group := &StepNode{ID: "group", Type: NodeGroup, Status: StatusSuccess, Parent: root}
	leaf := &StepNode{ID: "leaf", Type: NodeShell, Status: StatusSuccess, Parent: group, StaticCommand: "echo leaf"}
	group.Children = []*StepNode{leaf}
	root.Children = []*StepNode{group}

	_, ranges := buildSelectedDetailLines(group, 80, map[string]bool{}, 0, false, ResolverConfig{})
	if len(ranges) != 1 || ranges[0].node != group {
		t.Fatalf("selected detail ranges = %#v, want only group", ranges)
	}
}

func TestFitTreeRowNeverExceedsExtremelyNarrowSidebar(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	loop := &StepNode{ID: "very-long-loop-name", Type: NodeLoop, Parent: root, IterationsCompleted: 3, LoopMatches: []string{"a", "b", "c", "d"}}
	root.Children = []*StepNode{loop}
	m := newTestModel(&Tree{Root: root}, FromInspect)
	m.setSelected(loop)
	row := renderedStepRow{node: loop, selectable: true, depth: 8}
	for _, width := range []int{0, 1, 2, 5} {
		if got := lipgloss.Width(m.fitTreeRow(row, width)); got > width {
			t.Fatalf("width %d rendered %d columns", width, got)
		}
	}
}

func TestSelectionSurvivesInsertionThatChangesNodeKey(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	first := &StepNode{ID: "first", Type: NodeShell, Parent: root}
	selected := &StepNode{ID: "selected", Type: NodeShell, Parent: root}
	root.Children = []*StepNode{first, selected}
	m := newTestModel(&Tree{Root: root}, FromInspect)
	m.setSelected(selected)
	oldKey := m.selectedKey

	inserted := &StepNode{ID: "inserted", Type: NodeShell, Parent: root}
	root.Children = append([]*StepNode{inserted}, root.Children...)
	m.projectedRows()

	if m.selectedNode() != selected {
		t.Fatalf("selection moved to %v after insertion", m.selectedNode())
	}
	if m.selectedKey == oldKey {
		t.Fatalf("selection key %q was not refreshed after insertion", m.selectedKey)
	}
}

func TestAutoFollowOutsideManualScopeKeepsVisibleSelection(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	scope := &StepNode{ID: "scope", Type: NodeGroup, Parent: root}
	inside := &StepNode{ID: "inside", Type: NodeShell, Parent: scope}
	active := &StepNode{ID: "active", Type: NodeShell, Status: StatusInProgress, Parent: root}
	scope.Children = []*StepNode{inside}
	root.Children = []*StepNode{scope, active}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.path = []*StepNode{root, scope}
	m.setSelected(inside)
	m.applyAutoFollowToNode(active)
	if m.selectedNode() != inside {
		t.Fatalf("outside active step replaced scoped selection with %v", m.selectedNode())
	}
}

func TestRightPaneWidthUsesSettledSidebarWidth(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	step := &StepNode{ID: "short", Type: NodeShell, Parent: root}
	root.Children = []*StepNode{step}
	m := newTestModel(&Tree{Root: root}, FromInspect)
	m.termWidth = 100
	m.sidebarWidth = 60
	want := measureTreePaneLayout(m.termWidth, rowTexts(m.buildProjectedRenderedRows()), m.sidebarWidth).detail
	if got := m.rightPaneWidth(); got != want {
		t.Fatalf("right pane width = %d, want settled layout width %d", got, want)
	}
}
