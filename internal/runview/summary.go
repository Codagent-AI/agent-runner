package runview

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

type summaryMetrics struct {
	durationMS          int64
	durationReported    bool
	costUSD             float64
	pricedAttempts      int
	totalAttempts       int
	tokens              model.TokenCounts
	agentAttempts       int
	usageReported       int
	costReported        int
	tokenTotals         model.TokenTotals
	tokenTotalsReported int
}

type summaryRow struct {
	label    string
	duration string
	tokens   map[string]string
	cost     string
	status   NodeStatus
	selected bool
}

type summaryColumns struct {
	step       int
	duration   int
	input      int
	cacheRead  int
	cacheWrite int
	output     int
	reasoning  int
	cost       int
}

var summaryTokenColumns = []struct {
	key    string
	header string
}{
	{model.TokenInput, "Input"},
	{model.TokenCachedInput, "Cache read"},
	{model.TokenCacheWrite, "Cache write"},
	{model.TokenOutput, "Output"},
	{model.TokenReasoning, "Reasoning"},
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
	header = append(header,
		tuistyle.ScreenMargin+tuistyle.SectionStyle.Render("Run summary"),
		"")

	totals := m.summaryScopeTotals(now)
	children := m.currentChildren()
	rows := make([]summaryRow, 0, len(children))
	for i, child := range children {
		rows = append(rows, makeSummaryRow(child, i == m.cursor, now))
	}
	columns := measureSummaryColumns(rows, &totals, m.termWidth)
	header = append(header, renderSummaryHeader(columns))

	var rowLines []string
	if len(rows) == 0 {
		rowLines = []string{tuistyle.ScreenMargin + tuistyle.DimStyle.Render("No steps to display.")}
	} else {
		rowLines = make([]string, 0, len(rows))
		for _, row := range rows {
			rowLines = append(rowLines, renderSummaryRow(row, columns))
		}
	}

	footer := m.summaryFooterLines(&totals, columns)

	// Window the rows to the space left between header and footer. termHeight 0
	// (unsized, e.g. in tests) means render everything so nothing is hidden.
	budget := len(rowLines)
	if m.termHeight > 0 {
		budget = max(1, m.termHeight-len(header)-len(footer))
	}
	maxOffset := max(0, len(rowLines)-budget)
	if m.cursor < m.summaryOffset {
		m.summaryOffset = m.cursor
	} else if m.cursor >= m.summaryOffset+budget {
		m.summaryOffset = m.cursor - budget + 1
	}
	if m.summaryOffset > maxOffset {
		m.summaryOffset = maxOffset
	}
	off := m.summaryOffset
	end := min(off+budget, len(rowLines))
	visible := rowLines[off:end]

	if maxOffset > 0 {
		header[titleIdx] += tuistyle.DimStyle.Render(fmt.Sprintf("  rows %d–%d of %d · ↑/↓ select", off+1, end, len(rowLines)))
	}

	lines := make([]string, 0, len(header)+len(visible)+len(footer))
	lines = append(lines, header...)
	lines = append(lines, visible...)
	lines = append(lines, footer...)
	return strings.Join(lines, "\n") + "\n"
}

// summaryFooterLines builds the pinned footer: the run-totals line, any
// error/notice, and the rule plus help bar.
func (m *Model) summaryFooterLines(totals *model.RunTotals, columns summaryColumns) []string {
	totalRow := summaryRow{
		label: "Total", duration: formatDuration(totals.ActiveDurationMS),
		tokens: summaryTotalTokenCells(totals.Tokens),
		cost:   formatCoveredCost(totals.EstimatedAPICostUSD, totals.CostCoverage),
	}
	footer := []string{"", renderSummaryTotalRow(totalRow, columns)}
	footer = append(footer,
		tuistyle.ScreenMargin+"  "+tuistyle.DimStyle.Render(formatProcessedTokenTotals(totals)),
		tuistyle.ScreenMargin+"  "+tuistyle.DimStyle.Render(
			"usage "+string(totals.UsageCoverage)+"  ·  cost "+string(totals.CostCoverage)))
	if m.loadErr != "" {
		footer = append(footer, "", tuistyle.ScreenMargin+tuistyle.DimStyle.Render("Error: "+m.loadErr))
	}
	if m.notice != "" {
		footer = append(footer, "", tuistyle.ScreenMargin+tuistyle.DimStyle.Render(m.notice))
	}
	footer = append(footer, "", m.renderRule(), m.renderHelpBarWithCwd())
	return footer
}

func makeSummaryRow(node *StepNode, selected bool, now time.Time) summaryRow {
	metrics := aggregateSummaryMetrics(node, now)
	label := node.ID
	if node.Type == NodeIteration {
		label = fmt.Sprintf("iteration %d", node.IterationIndex+1)
	}
	if node.IsContainer() {
		label += " ›"
	}
	duration := "—"
	if metrics.durationReported {
		duration = formatDuration(metrics.durationMS)
	}
	tokens := make(map[string]string, len(summaryTokenColumns))
	for _, column := range summaryTokenColumns {
		tokens[column.key] = formatSummaryToken(&metrics, column.key)
	}
	return summaryRow{label: label, duration: duration, tokens: tokens, cost: formatSummaryCost(&metrics), status: node.Status, selected: selected}
}

func measureSummaryColumns(rows []summaryRow, totals *model.RunTotals, termWidth int) summaryColumns {
	columns := summaryColumns{
		step:     runewidth.StringWidth("Step") + 2,
		duration: runewidth.StringWidth("Duration"),
		input:    runewidth.StringWidth("Input"), cacheRead: runewidth.StringWidth("Cache read"),
		cacheWrite: runewidth.StringWidth("Cache write"), output: runewidth.StringWidth("Output"),
		reasoning: runewidth.StringWidth("Reasoning"),
		cost:      runewidth.StringWidth("Cost"),
	}
	for _, row := range rows {
		columns.step = max(columns.step, runewidth.StringWidth(row.label)+2)
		columns.duration = max(columns.duration, runewidth.StringWidth(row.duration))
		measureSummaryTokenColumns(&columns, row.tokens)
		columns.cost = max(columns.cost, runewidth.StringWidth(row.cost))
	}
	columns.step = max(columns.step, runewidth.StringWidth("Total"))
	columns.duration = max(columns.duration, runewidth.StringWidth(formatDuration(totals.ActiveDurationMS)))
	measureSummaryTokenColumns(&columns, summaryTotalTokenCells(totals.Tokens))
	columns.cost = max(columns.cost, runewidth.StringWidth(formatCoveredCost(totals.EstimatedAPICostUSD, totals.CostCoverage)))

	// Keep the numeric columns intact on narrow terminals and spend whatever
	// remains on the step label, truncating only that free-form column.
	if termWidth > 0 {
		const separators = 14 // two spaces between each of the eight columns
		fixed := columns.duration + columns.input + columns.cacheRead + columns.cacheWrite + columns.output + columns.reasoning + columns.cost
		availableStep := termWidth - runewidth.StringWidth(tuistyle.ScreenMargin) - separators - fixed
		columns.step = min(columns.step, max(runewidth.StringWidth("Step")+2, availableStep))
	}
	return columns
}

func renderSummaryHeader(columns summaryColumns) string {
	values := formatSummaryCells(columns, "  "+tuistyle.FitCell("Step", columns.step-2), "Duration",
		map[string]string{model.TokenInput: "Input", model.TokenCachedInput: "Cache read", model.TokenCacheWrite: "Cache write", model.TokenOutput: "Output", model.TokenReasoning: "Reasoning"}, "Cost")
	for i := range values {
		values[i] = tuistyle.ColumnHeader.Render(values[i])
	}
	return tuistyle.ScreenMargin + strings.Join(values, "  ")
}

func renderSummaryRow(row summaryRow, columns summaryColumns) string {
	prefix := "  "
	if row.selected {
		prefix = "▶ "
	}
	values := formatSummaryCells(columns, prefix+tuistyle.FitCell(row.label, columns.step-2), row.duration, row.tokens, row.cost)
	switch row.status {
	case StatusFailed:
		values[0] = tuistyle.StatusFailed.Render(values[0])
	case StatusPending, StatusSkipped:
		values[0] = tuistyle.DimStyle.Render(values[0])
	default:
		values[0] = tuistyle.NormalStyle.Render(values[0])
	}
	if row.duration == "—" {
		values[1] = tuistyle.DimStyle.Render(values[1])
	}
	return tuistyle.ScreenMargin + strings.Join(values, "  ")
}

func renderSummaryTotalRow(row summaryRow, columns summaryColumns) string {
	values := formatSummaryCells(columns, "  "+tuistyle.FitCell(row.label, columns.step-2), row.duration, row.tokens, row.cost)
	values[0] = tuistyle.SectionStyle.Render(values[0])
	return tuistyle.ScreenMargin + strings.Join(values, "  ")
}

func formatSummaryCells(columns summaryColumns, label, duration string, tokens map[string]string, cost string) []string {
	return []string{
		tuistyle.FitCell(label, columns.step), rightAlignSummaryCell(duration, columns.duration),
		rightAlignSummaryCell(tokens[model.TokenInput], columns.input), rightAlignSummaryCell(tokens[model.TokenCachedInput], columns.cacheRead),
		rightAlignSummaryCell(tokens[model.TokenCacheWrite], columns.cacheWrite), rightAlignSummaryCell(tokens[model.TokenOutput], columns.output),
		rightAlignSummaryCell(tokens[model.TokenReasoning], columns.reasoning), rightAlignSummaryCell(cost, columns.cost),
	}
}

func measureSummaryTokenColumns(columns *summaryColumns, tokens map[string]string) {
	columns.input = max(columns.input, runewidth.StringWidth(tokens[model.TokenInput]))
	columns.cacheRead = max(columns.cacheRead, runewidth.StringWidth(tokens[model.TokenCachedInput]))
	columns.cacheWrite = max(columns.cacheWrite, runewidth.StringWidth(tokens[model.TokenCacheWrite]))
	columns.output = max(columns.output, runewidth.StringWidth(tokens[model.TokenOutput]))
	columns.reasoning = max(columns.reasoning, runewidth.StringWidth(tokens[model.TokenReasoning]))
}

func summaryTotalTokenCells(tokens model.TokenCounts) map[string]string {
	cells := make(map[string]string, len(summaryTokenColumns))
	for _, column := range summaryTokenColumns {
		if value, ok := tokens[column.key]; ok {
			cells[column.key] = formatCount(value)
		} else {
			cells[column.key] = "—"
		}
	}
	return cells
}

func formatSummaryToken(metrics *summaryMetrics, category string) string {
	if value, ok := metrics.tokens[category]; ok {
		return formatCount(value)
	}
	if metrics.agentAttempts > 0 && metrics.usageReported == 0 {
		return "unavailable"
	}
	if metrics.agentAttempts == 0 && metrics.totalAttempts > 0 {
		return "0"
	}
	return "—"
}

func rightAlignSummaryCell(value string, width int) string {
	visibleWidth := runewidth.StringWidth(value)
	if visibleWidth >= width {
		return value
	}
	return strings.Repeat(" ", width-visibleWidth) + value
}

func aggregateSummaryMetrics(node *StepNode, now time.Time) summaryMetrics {
	metrics := summaryMetrics{tokens: model.TokenCounts{}}
	if node == nil {
		return metrics
	}
	if node.IsContainer() {
		for _, child := range node.Children {
			childMetrics := aggregateSummaryMetrics(child, now)
			metrics.add(&childMetrics)
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
				if attempt.Usage.TokenTotals != nil {
					metrics.tokenTotalsReported++
					metrics.tokenTotals.Input += attempt.Usage.TokenTotals.Input
					metrics.tokenTotals.Output += attempt.Usage.TokenTotals.Output
					metrics.tokenTotals.Total += attempt.Usage.TokenTotals.Total
				}
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

func (m *summaryMetrics) add(other *summaryMetrics) {
	m.durationMS += other.durationMS
	m.durationReported = m.durationReported || other.durationReported
	m.costUSD += other.costUSD
	m.pricedAttempts += other.pricedAttempts
	m.totalAttempts += other.totalAttempts
	m.agentAttempts += other.agentAttempts
	m.usageReported += other.usageReported
	m.costReported += other.costReported
	m.tokenTotalsReported += other.tokenTotalsReported
	m.tokenTotals.Input += other.tokenTotals.Input
	m.tokenTotals.Output += other.tokenTotals.Output
	m.tokenTotals.Total += other.tokenTotals.Total
	for category, count := range other.tokens {
		m.tokens[category] += count
	}
}

func formatSummaryCost(metrics *summaryMetrics) string {
	if metrics.pricedAttempts == 0 {
		if metrics.agentAttempts > 0 {
			return "unavailable"
		}
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
	return summaryTotalsFromMetrics(&metrics)
}

func (m *Model) summaryScopeTotals(now time.Time) model.RunTotals {
	scope := m.currentContainer()
	if scope == nil {
		metrics := summaryMetrics{tokens: model.TokenCounts{}}
		return summaryTotalsFromMetrics(&metrics)
	}
	if scope == m.tree.Root && m.tree.RunTotals != nil {
		return *m.tree.RunTotals
	}
	metrics := aggregateSummaryMetrics(scope, now)
	return summaryTotalsFromMetrics(&metrics)
}

func summaryTotalsFromMetrics(metrics *summaryMetrics) model.RunTotals {
	totals := model.RunTotals{
		ActiveDurationMS:   metrics.durationMS,
		Tokens:             metrics.tokens,
		UsageCoverage:      summaryCoverage(metrics.agentAttempts, metrics.usageReported),
		TokenTotalCoverage: summaryCoverage(metrics.agentAttempts, metrics.tokenTotalsReported),
		CostCoverage:       summaryCoverage(metrics.agentAttempts, metrics.costReported),
	}
	if metrics.tokenTotalsReported > 0 {
		value := metrics.tokenTotals
		totals.TokenTotals = &value
	}
	if metrics.costReported > 0 {
		cost := metrics.costUSD
		totals.EstimatedAPICostUSD = &cost
	}
	return totals
}

func formatProcessedTokenTotals(totals *model.RunTotals) string {
	coverage := totals.TokenTotalCoverage
	if coverage == "" {
		coverage = model.CoverageNone
	}
	if totals.TokenTotals == nil {
		return "Processed tokens: unavailable (" + string(coverage) + ")"
	}
	value := fmt.Sprintf("Processed tokens: input %s · output %s · total %s",
		formatCount(totals.TokenTotals.Input), formatCount(totals.TokenTotals.Output), formatCount(totals.TokenTotals.Total))
	return value + " (" + string(coverage) + ")"
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
