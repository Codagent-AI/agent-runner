package runview

import (
	"fmt"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

type summaryMetrics struct {
	durationMS       int64
	durationReported bool
	costUSD          float64
	pricedAttempts   int
	totalAttempts    int
	tokens           model.TokenCounts
	agentAttempts    int
	usageReported    int
	costReported     int
}

func (m *Model) renderSummary() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.renderChrome())
	b.WriteString("\n\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.SectionStyle.Render("Run summary"))
	b.WriteString("\n\n")

	if m.tree == nil || m.tree.Root == nil || len(m.tree.Root.Children) == 0 {
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(tuistyle.DimStyle.Render("No steps to display."))
		b.WriteString("\n")
	} else {
		for _, child := range m.tree.Root.Children {
			m.renderSummaryNode(&b, child, 0)
		}
	}

	totals := m.summaryRunTotals()
	b.WriteString("\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.SectionStyle.Render("Total"))
	b.WriteString("  ")
	b.WriteString(formatDuration(totals.ActiveDurationMS))
	if len(totals.Tokens) > 0 {
		b.WriteString("  ")
		b.WriteString(formatTokenCounts(totals.Tokens))
	}
	b.WriteString("  cost ")
	b.WriteString(formatCoveredCost(totals.EstimatedAPICostUSD, totals.CostCoverage))
	b.WriteString("  usage ")
	b.WriteString(string(totals.UsageCoverage))
	b.WriteString("\n")

	if m.loadErr != "" {
		b.WriteString("\n")
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(tuistyle.DimStyle.Render("Error: " + m.loadErr))
	}
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(tuistyle.DimStyle.Render(m.notice))
	}

	b.WriteString("\n")
	b.WriteString(m.renderRule())
	b.WriteString("\n")
	b.WriteString(m.renderHelpBarWithCwd())
	b.WriteString("\n")
	return b.String()
}

func (m *Model) renderSummaryNode(b *strings.Builder, node *StepNode, depth int) summaryMetrics {
	metrics := aggregateSummaryMetrics(node)
	label := node.ID
	if node.Type == NodeIteration {
		label = fmt.Sprintf("iter %d", node.IterationIndex+1)
	}
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(label)
	b.WriteString("  ")
	if metrics.durationReported {
		b.WriteString(formatDuration(metrics.durationMS))
	} else {
		b.WriteString("—")
	}
	b.WriteString("  ")
	b.WriteString(formatSummaryCost(metrics))
	b.WriteString("\n")
	for _, child := range node.Children {
		m.renderSummaryNode(b, child, depth+1)
	}
	return metrics
}

func aggregateSummaryMetrics(node *StepNode) summaryMetrics {
	metrics := summaryMetrics{tokens: model.TokenCounts{}}
	if node == nil {
		return metrics
	}
	if node.IsContainer() {
		for _, child := range node.Children {
			metrics.add(aggregateSummaryMetrics(child))
		}
		return metrics
	}
	for i := range node.Attempts {
		attempt := &node.Attempts[i]
		metrics.totalAttempts++
		if attempt.DurationMs != nil {
			metrics.durationMS += *attempt.DurationMs
			metrics.durationReported = true
		}
		if attempt.CostUSD != nil {
			metrics.costUSD += *attempt.CostUSD
			metrics.pricedAttempts++
		}
		if node.Type == NodeHeadlessAgent || node.Type == NodeInteractiveAgent {
			metrics.agentAttempts++
			if attempt.CostUSD != nil {
				metrics.costReported++
			}
			if attempt.Usage != nil && attempt.Usage.Status == model.UsageCollected {
				metrics.usageReported++
			}
		}
		if attempt.Usage != nil && attempt.Usage.Status == model.UsageCollected {
			for category, count := range attempt.Usage.Tokens {
				metrics.tokens[category] += count
			}
		}
	}
	return metrics
}

func (m *summaryMetrics) add(other summaryMetrics) {
	m.durationMS += other.durationMS
	m.durationReported = m.durationReported || other.durationReported
	m.costUSD += other.costUSD
	m.pricedAttempts += other.pricedAttempts
	m.totalAttempts += other.totalAttempts
	m.agentAttempts += other.agentAttempts
	m.usageReported += other.usageReported
	m.costReported += other.costReported
	for category, count := range other.tokens {
		m.tokens[category] += count
	}
}

func formatSummaryCost(metrics summaryMetrics) string {
	if metrics.pricedAttempts == 0 {
		return "—"
	}
	value := formatUSD(metrics.costUSD)
	if metrics.agentAttempts > 0 && metrics.costReported < metrics.agentAttempts {
		value += " (partial)"
	}
	return value
}

func (m *Model) summaryRunTotals() model.RunTotals {
	if m.tree != nil && m.tree.RunTotals != nil {
		return *m.tree.RunTotals
	}
	metrics := aggregateSummaryMetrics(m.tree.Root)
	totals := model.RunTotals{
		ActiveDurationMS: metrics.durationMS,
		Tokens:           metrics.tokens,
		UsageCoverage:    summaryCoverage(metrics.agentAttempts, metrics.usageReported),
		CostCoverage:     summaryCoverage(metrics.agentAttempts, metrics.costReported),
	}
	if metrics.costReported > 0 {
		cost := metrics.costUSD
		totals.EstimatedAPICostUSD = &cost
	}
	return totals
}

func summaryCoverage(total, reported int) model.Coverage {
	switch {
	case total == 0 || reported == 0:
		return model.CoverageNone
	case reported == total:
		return model.CoverageComplete
	default:
		return model.CoveragePartial
	}
}

func formatCoveredCost(cost *float64, coverage model.Coverage) string {
	if cost == nil {
		return "unavailable (" + string(coverage) + ")"
	}
	value := formatUSD(*cost)
	if coverage != model.CoverageComplete {
		value += " (" + string(coverage) + ")"
	}
	return value
}
