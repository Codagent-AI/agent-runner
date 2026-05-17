package runview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

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

	return tuistyle.ScreenMargin + crumbStr +
		tuistyle.DimStyle.Render(suffix) +
		m.styledRunStatus()
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
			return tuistyle.StatusSuccess.Render("completed")
		}
	}
	// Inspect / list mode: use the run-lock and root status.
	if m.active {
		return tuistyle.StatusSuccess.Render("active")
	}
	status := m.rootStatus()
	switch status {
	case StatusFailed:
		return tuistyle.StatusFailed.Bold(true).Render("failed")
	case StatusSuccess:
		return tuistyle.StatusSuccess.Render("completed")
	default:
		hint := " (r to resume)"
		if m.entered == FromDefinition {
			hint = " (r to start run)"
		}
		return tuistyle.StatusInactive.Bold(true).Render("inactive") +
			tuistyle.DimStyle.Render(hint)
	}
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
	return c.Children
}

func (m *Model) selectedNode() *StepNode {
	children := m.currentChildren()
	if m.cursor < 0 || m.cursor >= len(children) {
		return nil
	}
	return children[m.cursor]
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
