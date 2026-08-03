package runview

import (
	"strings"
	"testing"

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
