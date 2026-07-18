package runview

import (
	"fmt"
	"strings"
	"time"

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
	// Count the currently-running step's elapsed time only while the run is
	// live; a static inspect of an interrupted run must not add stale wall time.
	var now time.Time
	if m.hasLiveUpdates() {
		now = time.Now()
	}

	// The step rows scroll within a fixed header and a pinned footer, so the
	// run-totals line stays visible no matter how many steps the workflow has.
	header := []string{""}
	header = append(header, strings.Split(m.renderChrome(), "\n")...)
	header = append(header, "")
	titleIdx := len(header)
	header = append(header, tuistyle.ScreenMargin+tuistyle.SectionStyle.Render("Run summary"))
	header = append(header, "")

	var rowLines []string
	if m.tree == nil || m.tree.Root == nil || len(m.tree.Root.Children) == 0 {
		rowLines = []string{tuistyle.ScreenMargin + tuistyle.DimStyle.Render("No steps to display.")}
	} else {
		var rb strings.Builder
		for _, child := range m.tree.Root.Children {
			m.renderSummaryNode(&rb, child, 0, now)
		}
		rowLines = strings.Split(strings.TrimRight(rb.String(), "\n"), "\n")
	}

	footer := m.summaryFooterLines(now)

	// Window the rows to the space left between header and footer. termHeight 0
	// (unsized, e.g. in tests) means render everything so nothing is hidden.
	budget := len(rowLines)
	if m.termHeight > 0 {
		budget = max(1, m.termHeight-len(header)-len(footer))
	}
	maxOffset := max(0, len(rowLines)-budget)
	if m.summaryOffset > maxOffset {
		m.summaryOffset = maxOffset
	}
	off := m.summaryOffset
	end := min(off+budget, len(rowLines))
	visible := rowLines[off:end]

	if maxOffset > 0 {
		header[titleIdx] += tuistyle.DimStyle.Render(fmt.Sprintf("  rows %d–%d of %d · ↑/↓ scroll", off+1, end, len(rowLines)))
	}

	lines := make([]string, 0, len(header)+len(visible)+len(footer))
	lines = append(lines, header...)
	lines = append(lines, visible...)
	lines = append(lines, footer...)
	return strings.Join(lines, "\n") + "\n"
}

// summaryFooterLines builds the pinned footer: the run-totals line, any
// error/notice, and the rule plus help bar.
func (m *Model) summaryFooterLines(now time.Time) []string {
	totals := m.summaryRunTotals(now)
	var total strings.Builder
	total.WriteString(tuistyle.ScreenMargin)
	total.WriteString(tuistyle.SectionStyle.Render("Total"))
	total.WriteString("  ")
	total.WriteString(formatDuration(totals.ActiveDurationMS))
	if len(totals.Tokens) > 0 {
		total.WriteString("  ")
		total.WriteString(formatTokenCounts(totals.Tokens))
	}
	total.WriteString("  cost ")
	total.WriteString(formatCoveredCost(totals.EstimatedAPICostUSD, totals.CostCoverage))
	total.WriteString("  usage ")
	total.WriteString(string(totals.UsageCoverage))

	footer := []string{"", total.String()}
	if m.loadErr != "" {
		footer = append(footer, "", tuistyle.ScreenMargin+tuistyle.DimStyle.Render("Error: "+m.loadErr))
	}
	if m.notice != "" {
		footer = append(footer, "", tuistyle.ScreenMargin+tuistyle.DimStyle.Render(m.notice))
	}
	footer = append(footer, "", m.renderRule(), m.renderHelpBarWithCwd())
	return footer
}

func (m *Model) renderSummaryNode(b *strings.Builder, node *StepNode, depth int, now time.Time) summaryMetrics {
	metrics := aggregateSummaryMetrics(node, now)
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
		m.renderSummaryNode(b, child, depth+1, now)
	}
	return metrics
}

func aggregateSummaryMetrics(node *StepNode, now time.Time) summaryMetrics {
	metrics := summaryMetrics{tokens: model.TokenCounts{}}
	if node == nil {
		return metrics
	}
	if node.IsContainer() {
		for _, child := range node.Children {
			metrics.add(aggregateSummaryMetrics(child, now))
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
		// Coverage denominators count only attempts that actually launched an
		// agent. Skipped or never-invoked agent steps are excluded so the
		// mid-run indicator matches the finished-run one (which filters on
		// agent_invoked via the authoritative run_end totals).
		if (node.Type == NodeHeadlessAgent || node.Type == NodeInteractiveAgent) && attempt.AgentInvoked {
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
	// Mid-run, the currently-executing step has no attempt record yet (attempts
	// are appended only on step_end). Add its elapsed wall time so the summary
	// reflects active work instead of reading 0 until the first step finishes.
	// Skipped when not live (now zero) or aborted (not actually running).
	if !now.IsZero() && node.Status == StatusInProgress && !node.Aborted && !node.StartedAt.IsZero() {
		if elapsed := now.Sub(node.StartedAt); elapsed > 0 {
			metrics.durationMS += elapsed.Milliseconds()
			metrics.durationReported = true
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

func (m *Model) summaryRunTotals(now time.Time) model.RunTotals {
	if m.tree != nil && m.tree.RunTotals != nil {
		return *m.tree.RunTotals
	}
	metrics := aggregateSummaryMetrics(m.tree.Root, now)
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
