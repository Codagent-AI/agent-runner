package runview

import (
	"fmt"
	"sort"
)

// treeRow is a deterministic, presentation-only projection of a workflow
// tree. Only node rows participate in selection; omission rows describe the
// siblings intentionally hidden by the inline expansion window.
type treeRowKind int

const (
	treeRowNode treeRowKind = iota
	treeRowOmission
)

type omissionPlacement int

const (
	omissionEarlier omissionPlacement = iota
	omissionBetween
	omissionLater
)

type treeRow struct {
	kind      treeRowKind
	node      *StepNode
	depth     int
	omitted   int
	placement omissionPlacement
}

func (r treeRow) selectable() bool { return r.kind == treeRowNode && r.node != nil }

func (r treeRow) omissionLabel() string {
	if r.kind != treeRowOmission {
		return ""
	}
	where := "later"
	switch r.placement {
	case omissionEarlier:
		where = "earlier"
	case omissionBetween:
		where = "between"
	}
	return fmt.Sprintf("… %d %s", r.omitted, where)
}

// projectTree returns the visible rows under manual scope. Scope is never
// changed here: selected and active merely determine which in-scope branches
// expand. The root scope, like every manually drilled scope, always exposes all
// its direct children; only nested expansions use the five-child window.
func projectTree(scope, selected, active *StepNode) []treeRow {
	if scope == nil {
		return nil
	}
	container := scope.Drilldown()
	if container == nil {
		return nil
	}
	rows := make([]treeRow, 0, len(container.Children))
	for _, child := range container.Children {
		rows = append(rows, treeRow{kind: treeRowNode, node: child})
		rows = append(rows, projectExpandedChildren(child, selected, active, 1)...)
	}
	return rows
}

func projectExpandedChildren(parent, selected, active *StepNode, depth int) []treeRow {
	if parent == nil || !parent.IsContainer() {
		return nil
	}
	target := parent.Drilldown()
	if target == nil {
		return nil
	}
	selectedFocus := directFocus(target.Children, selected)
	activeFocus := directFocus(target.Children, active)
	if selected != parent && active != parent && selectedFocus == nil && activeFocus == nil {
		return nil
	}
	visible := inlineChildWindow(target.Children, selectedFocus, activeFocus, parent.Status)
	rows := make([]treeRow, 0, len(visible)+3)
	previous := -1
	for _, index := range visible {
		if previous < 0 && index > 0 {
			rows = append(rows, treeRow{kind: treeRowOmission, depth: depth, omitted: index, placement: omissionEarlier})
		} else if previous >= 0 && index > previous+1 {
			rows = append(rows, treeRow{kind: treeRowOmission, depth: depth, omitted: index - previous - 1, placement: omissionBetween})
		}
		child := target.Children[index]
		rows = append(rows, treeRow{kind: treeRowNode, node: child, depth: depth})
		rows = append(rows, projectExpandedChildren(child, selected, active, depth+1)...)
		previous = index
	}
	if previous >= 0 && previous+1 < len(target.Children) {
		rows = append(rows, treeRow{kind: treeRowOmission, depth: depth, omitted: len(target.Children) - previous - 1, placement: omissionLater})
	}
	return rows
}

func directFocus(children []*StepNode, node *StepNode) *StepNode {
	for _, child := range children {
		if node == child || isDescendantOf(node, child) {
			return child
		}
	}
	return nil
}

func isDescendantOf(node, ancestor *StepNode) bool {
	for current := node; current != nil; current = current.Parent {
		if current == ancestor {
			return true
		}
	}
	return false
}

// inlineChildWindow implements the bounded child algorithm. The returned
// indexes are in workflow order and contain at most five real children.
func inlineChildWindow(children []*StepNode, selected, active *StepNode, status NodeStatus) []int {
	if len(children) <= 5 {
		out := make([]int, len(children))
		for i := range children {
			out[i] = i
		}
		return out
	}
	index := func(node *StepNode) int {
		for i, child := range children {
			if child == node {
				return i
			}
		}
		return -1
	}
	selectedIndex, activeIndex := index(selected), index(active)
	if selectedIndex < 0 && activeIndex < 0 {
		if status == StatusPending {
			return []int{0, 1, 2, 3, 4}
		}
		return []int{len(children) - 5, len(children) - 4, len(children) - 3, len(children) - 2, len(children) - 1}
	}
	if selectedIndex < 0 || activeIndex < 0 || selectedIndex == activeIndex {
		focus := selectedIndex
		if focus < 0 {
			focus = activeIndex
		}
		start := focus - 2
		if start < 0 {
			start = 0
		}
		if start+5 > len(children) {
			start = len(children) - 5
		}
		return []int{start, start + 1, start + 2, start + 3, start + 4}
	}
	chosen := map[int]bool{selectedIndex: true, activeIndex: true}
	type candidate struct{ index, distance int }
	candidates := make([]candidate, 0, len(children)-2)
	for i := range children {
		if chosen[i] {
			continue
		}
		distance := abs(i - selectedIndex)
		if other := abs(i - activeIndex); other < distance {
			distance = other
		}
		candidates = append(candidates, candidate{i, distance})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			return candidates[i].index < candidates[j].index
		}
		return candidates[i].distance < candidates[j].distance
	})
	for _, candidate := range candidates[:min(3, len(candidates))] {
		chosen[candidate.index] = true
	}
	out := make([]int, 0, len(chosen))
	for i := range children {
		if chosen[i] {
			out = append(out, i)
		}
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
