package runview

import (
	"fmt"
	"strings"

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

	startStr := tuistyle.FormatTime(m.startTime)
	suffix := ""
	if startStr != "" {
		suffix = "  ·  started " + startStr
	}
	suffix += "  ·  "

	return "  " + crumbStr +
		tuistyle.DimStyle.Render(suffix) +
		m.styledRunStatus()
}

func (m *Model) styledRunStatus() string {
	// Live-run mode: blink while running, then show result.
	if m.running {
		if tuistyle.BlinkOn(m.pulsePhase) {
			return tuistyle.StatusSuccess.Render("running")
		}
		return tuistyle.BlinkOffStyle.Render("running")
	}
	if m.liveResult != "" {
		switch m.liveResult {
		case "failed", "stopped":
			return tuistyle.StatusFailed.Render(m.liveResult)
		default:
			return tuistyle.StatusSuccess.Render("completed")
		}
	}
	// Inspect / list mode: use the run-lock and root status.
	if m.active {
		if tuistyle.BlinkOn(m.pulsePhase) {
			return tuistyle.StatusSuccess.Render("active")
		}
		return tuistyle.BlinkOffStyle.Render("active")
	}
	status := m.rootStatus()
	switch status {
	case StatusFailed:
		return tuistyle.StatusFailed.Render("failed")
	case StatusSuccess:
		return tuistyle.StatusSuccess.Render("completed")
	default:
		return tuistyle.StatusInactive.Render("inactive")
	}
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
		for k, v := range params {
			paramParts = append(paramParts, k+" = "+v)
		}
	}

	bar := tuistyle.InsetBarStyle.Render("▍ ")

	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(bar)
	b.WriteString(tuistyle.SectionStyle.Render("workflow: "))
	b.WriteString(tuistyle.NormalStyle.Render(name))
	b.WriteString("\n")
	if len(paramParts) > 0 {
		b.WriteString("  ")
		b.WriteString(bar)
		b.WriteString(tuistyle.SectionStyle.Render("params:   "))
		b.WriteString(tuistyle.NormalStyle.Render(strings.Join(paramParts, ", ")))
		b.WriteString("\n")
	}
	return b.String()
}
