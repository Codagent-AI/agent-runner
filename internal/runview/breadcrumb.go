package runview

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

var githubPullRequestPath = regexp.MustCompile(`^/[^/]+/[^/]+/pull/(\d+)$`)

func (m *Model) renderBreadcrumb() string {
	var crumbs []string

	topName := CanonicalName(m.tree.WorkflowPath, m.resolverCfg)
	if topName == "" {
		topName = m.tree.Root.ID
	}
	crumbs = append(crumbs, topName)

	for _, n := range m.path[1:] {
		switch n.Type {
		case NodeIteration:
			crumbs = append(crumbs, fmt.Sprintf("iter %d", n.IterationIndex+1))
		case NodeSubWorkflow:
			name := CanonicalName(n.StaticWorkflowPath, m.resolverCfg)
			if name == "" {
				name = n.ID
			}
			crumbs = append(crumbs, name)
		default:
			crumbs = append(crumbs, n.ID)
		}
	}

	sep := tuistyle.AccentStyle.Render(tuistyle.BreadcrumbSeparator)
	crumbStr := tuistyle.LabelStyle.Render("← " + crumbs[0])
	if m.recordedVersion != "" {
		crumbStr += tuistyle.DimStyle.Render(" · " + m.recordedVersion)
	}
	if m.profileSet != "" && m.profileSet != "default" {
		crumbStr += tuistyle.DimStyle.Render(" · profile: " + m.profileSet)
	}
	for _, c := range crumbs[1:] {
		crumbStr += sep + tuistyle.LabelStyle.Render(c)
	}

	suffix := ""
	if m.running || m.active {
		if elapsed := formatLiveElapsed(m.startTime, time.Now()); elapsed != "" {
			suffix = "  ·  " + elapsed
		}
	} else if startStr := tuistyle.FormatTime(m.startTime); startStr != "" {
		suffix = "  ·  started " + startStr
	}
	suffix += "  ·  "

	result := tuistyle.ScreenMargin + crumbStr +
		tuistyle.DimStyle.Render(suffix) +
		m.styledRunStatus()
	if m.tree.PullRequestURL != "" {
		result += pullRequestBreadcrumbSegment(m.tree.PullRequestURL)
	}
	return result
}

func pullRequestBreadcrumbSegment(rawURL string) string {
	target, label, ok := linkablePullRequestURL(rawURL)
	if !ok {
		return tuistyle.DimStyle.Render(" · PR")
	}
	return tuistyle.DimStyle.Render(" · " + osc8Hyperlink(target, label))
}

// linkablePullRequestURL decides whether rawURL may be used as an OSC 8 target
// and what the segment should be labelled.
//
// These are deliberately separate properties. Safety is about what can break
// out of the escape sequence, so it turns on control characters, scheme, and
// userinfo — not on which forge hosts the pull request. The label is about
// what we can say about the URL: only a `/pull/<number>` path yields a number,
// and every other safe URL still links behind a bare "PR".
func linkablePullRequestURL(rawURL string) (target, label string, ok bool) {
	if strings.ContainsFunc(rawURL, unicode.IsControl) {
		return "", "", false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return "", "", false
	}
	label = "PR"
	if matches := githubPullRequestPath.FindStringSubmatch(parsed.Path); len(matches) == 2 {
		label += " #" + matches[1]
	}
	return rawURL, label, true
}

// osc8Hyperlink accepts only a URL returned by linkablePullRequestURL.
func osc8Hyperlink(target, label string) string {
	return "\x1b]8;;" + target + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func (m *Model) styledRunStatus() string {
	// Live-run mode: blink while running, then show result.
	if m.running {
		return tuistyle.StatusSuccess.Render("running")
	}
	if m.liveResult != "" {
		switch m.liveResult {
		case "failed", "stopped":
			return tuistyle.StatusFailed.Bold(true).Render(m.liveResult)
		default:
			return m.styledCompletedStatus()
		}
	}
	// Inspect / list mode: use the run-lock and root status.
	if m.active {
		return tuistyle.StatusSuccess.Render("active")
	}
	status := m.rootStatus()
	switch status {
	case StatusFailed:
		result := tuistyle.StatusFailed.Bold(true).Render("failed")
		if m.canResumeRun() {
			result += tuistyle.DimStyle.Render(" (r to resume)")
		}
		return result
	case StatusSuccess:
		return m.styledCompletedStatus()
	default:
		hint := " (r to resume)"
		if m.entered == FromDefinition {
			hint = " (r to start run)"
		}
		return tuistyle.StatusInactive.Bold(true).Render("inactive") +
			tuistyle.DimStyle.Render(hint)
	}
}

func (m *Model) warningCount() int {
	count := len(m.tree.WarningOrigins())
	if m.tree.RunWarningCount > count {
		return m.tree.RunWarningCount
	}
	return count
}

func (m *Model) styledCompletedStatus() string {
	if count := m.warningCount(); count > 0 {
		return tuistyle.StatusInactive.Render(fmt.Sprintf("complete with warnings (%d)", count))
	}
	return tuistyle.StatusSuccess.Render("completed")
}

func formatLiveElapsed(start, now time.Time) string {
	if start.IsZero() {
		return ""
	}
	elapsed := now.Sub(start)
	if elapsed < 0 {
		elapsed = 0
	}
	return "elapsed " + formatElapsedSeconds(elapsed)
}

func formatElapsedSeconds(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSecs := int(elapsed / time.Second)
	if totalSecs < 60 {
		return fmt.Sprintf("%ds", totalSecs)
	}
	if totalSecs < 3600 {
		mins := totalSecs / 60
		remainSecs := totalSecs % 60
		return fmt.Sprintf("%dm %ds", mins, remainSecs)
	}
	hours := totalSecs / 3600
	mins := (totalSecs % 3600) / 60
	remainSecs := totalSecs % 60
	parts := []string{fmt.Sprintf("%dh", hours)}
	if mins > 0 || remainSecs > 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if remainSecs > 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainSecs))
	}
	return strings.Join(parts, " ")
}

func (m *Model) rootStatus() NodeStatus {
	return m.tree.Root.Status
}

func (m *Model) currentContainer() *StepNode {
	if len(m.path) == 0 {
		return m.tree.Root
	}
	return m.path[len(m.path)-1].Drilldown()
}

func (m *Model) currentChildren() []*StepNode {
	c := m.currentContainer()
	if c == nil {
		return nil
	}
	if m.showSummary && (c.Type == NodeHeadlessAgent || c.Type == NodeInteractiveAgent) && len(c.Children) > 0 {
		return c.summaryChildren()
	}
	return c.Children
}

func (m *Model) selectedNode() *StepNode {
	m.restoreSelectedNode()
	if m.selected != nil {
		return m.selected
	}
	children := m.currentChildren()
	if m.cursor < 0 || m.cursor >= len(children) {
		return nil
	}
	return children[m.cursor]
}

func firstRealChild(container *StepNode) *StepNode {
	if container == nil {
		return nil
	}
	return firstNode(container.Children)
}

func firstNode(nodes []*StepNode) *StepNode {
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func (m *Model) setSelected(node *StepNode) {
	m.selected = node
	if node == nil {
		m.selectedKey = ""
		return
	}
	m.selectedKey = node.NodeKey()
	m.syncProjectedCursor()
}

func (m *Model) projectedRows() []treeRow {
	m.restoreSelectedNode()
	m.ensureProjectionContainersLoaded(m.selected)
	m.ensureProjectionContainersLoaded(m.activeNode())
	rows := projectTree(m.manualScope(), m.selectedNode(), m.activeNode())
	m.syncProjectedCursorFromRows(rows)
	return rows
}

func (m *Model) restoreSelectedNode() {
	if m.tree == nil || m.tree.Root == nil {
		return
	}
	m.restoreManualPath()
	if m.selected == nil && m.selectedKey == "" {
		return
	}
	inTree := m.selected != nil && isDescendantOf(m.selected, m.tree.Root)
	if restored := findNodeByKey(m.tree.Root, m.selectedKey); !inTree {
		m.selected = restored
	} else if m.selected.NodeKey() == m.selectedKey && restored != nil {
		// A rebuilt tree has no surviving pointer, so a stable key is the
		// primary way to restore its corresponding node.
		m.selected = restored
	} else if m.selected != nil {
		// Dynamic insertion can change an index-based key without replacing the
		// node pointer. Keep that real selected execution and refresh its key.
		m.selectedKey = m.selected.NodeKey()
	}

	scope := m.currentContainer()
	if m.selected != nil && scope != nil && isDescendantOf(m.selected, scope) {
		return
	}
	m.selected = firstRealChild(scope)
	if m.selected != nil {
		m.selectedKey = m.selected.NodeKey()
	}
}

func (m *Model) restoreManualPath() {
	if len(m.path) == 0 {
		m.path = []*StepNode{m.tree.Root}
		return
	}
	if m.path[0] == m.tree.Root {
		return
	}
	restored := []*StepNode{m.tree.Root}
	for _, old := range m.path[1:] {
		current := findNodeByKey(m.tree.Root, old.NodeKey())
		if current == nil {
			break
		}
		restored = append(restored, current)
	}
	m.path = restored
}

func findNodeByKey(root *StepNode, key string) *StepNode {
	if root == nil || key == "" {
		return nil
	}
	if root.NodeKey() == key {
		return root
	}
	for _, child := range root.Children {
		if found := findNodeByKey(child, key); found != nil {
			return found
		}
	}
	for _, child := range root.Body {
		if found := findNodeByKey(child, key); found != nil {
			return found
		}
	}
	return nil
}

func (m *Model) manualScope() *StepNode {
	if len(m.path) == 0 {
		return m.tree.Root
	}
	return m.path[len(m.path)-1]
}

func (m *Model) activeNode() *StepNode {
	if m.tree == nil {
		return nil
	}
	if node := m.tree.FindByPrefix(m.activeStepPrefix); node != nil {
		return node
	}
	return deepestInProgressNode(m.tree.Root)
}

func (m *Model) ensureProjectionContainersLoaded(node *StepNode) {
	for current := node; current != nil; current = current.Parent {
		target := current.Drilldown()
		if target != nil && target.Type == NodeSubWorkflow && !target.SubLoaded && target.ErrorMessage == "" {
			if err := m.tree.EnsureSubWorkflowLoaded(target); err != nil {
				target.ErrorMessage = err.Error()
			}
		}
	}
}

func (m *Model) syncProjectedCursor() {
	m.syncProjectedCursorFromRows(m.projectedRows())
}

func (m *Model) syncProjectedCursorFromRows(rows []treeRow) {
	for i, row := range rows {
		if row.node == m.selected {
			m.cursor = i
			return
		}
	}
}

func (m *Model) renderSubWorkflowHeader() string {
	container := m.currentContainer()
	if container == nil || container.Type != NodeSubWorkflow {
		return ""
	}

	name := CanonicalName(container.StaticWorkflowPath, m.resolverCfg)
	if name == "" {
		name = bareWorkflowName(container.StaticWorkflow)
	}
	if name == "" {
		name = container.ID
	}

	var paramParts []string
	params := container.InterpolatedParams
	if params == nil {
		params = container.StaticParams
	}
	if len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			paramParts = append(paramParts, k+" = "+params[k])
		}
	}

	bar := tuistyle.InsetBarStyle.Render("▍ ")

	var b strings.Builder
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(bar)
	b.WriteString(tuistyle.SectionStyle.Render("workflow: "))
	b.WriteString(tuistyle.NormalStyle.Render(name))
	b.WriteString("\n")
	if len(paramParts) > 0 {
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(bar)
		b.WriteString(tuistyle.SectionStyle.Render("params:   "))
		b.WriteString(tuistyle.NormalStyle.Render(strings.Join(paramParts, ", ")))
		b.WriteString("\n")
	}
	return b.String()
}
