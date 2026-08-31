package runview

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func blockTypeGlyph(t NodeType) string {
	switch t {
	case NodeShell:
		return "$"
	case NodeScript:
		return "#"
	case NodeUI:
		return "▣"
	case NodeHeadlessAgent:
		return "⚙"
	case NodeInteractiveAgent:
		return "❯"
	case NodeAgentCall:
		return "↗"
	case NodeSubWorkflow:
		return "↳"
	case NodeLoop:
		return "↺"
	case NodeIteration:
		return "»"
	case NodeGroup:
		return "▾"
	case NodeRepository:
		return "◆"
	}
	return "·"
}

func formatTokenCounts(tokens model.TokenCounts) string {
	if len(tokens) == 0 {
		return "none reported"
	}
	keys := orderedTokenCategories(tokens)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, tokenCategoryLabel(key)+" "+formatCount(tokens[key]))
	}
	return strings.Join(parts, " · ")
}

func orderedTokenCategories(tokens model.TokenCounts) []string {
	canonical := []string{model.TokenInput, model.TokenCachedInput, model.TokenCacheWrite, model.TokenOutput, model.TokenReasoning}
	keys := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(canonical))
	for _, key := range canonical {
		seen[key] = true
		if _, ok := tokens[key]; ok {
			keys = append(keys, key)
		}
	}
	var other []string
	for key := range tokens {
		if !seen[key] {
			other = append(other, key)
		}
	}
	sort.Strings(other)
	return append(keys, other...)
}

func tokenCategoryLabel(category string) string { return strings.ReplaceAll(category, "_", " ") }

func formatCount(value int64) string {
	raw := strconv.FormatInt(value, 10)
	start := 0
	if strings.HasPrefix(raw, "-") {
		start = 1
	}
	for i := len(raw) - 3; i > start; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return raw
}

func formatUSD(value float64) string { return fmt.Sprintf("$%.2f", value) }

func itNum(idx int) string { return fmt.Sprintf("%d", idx+1) }

func loopTotal(n *StepNode) int {
	if len(n.LoopMatches) > 0 {
		return len(n.LoopMatches)
	}
	if n.StaticLoopMax != nil {
		return *n.StaticLoopMax
	}
	return 0
}

func statusLabel(s NodeStatus) string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusFailed:
		return "failed"
	case StatusSkipped:
		return "skipped"
	case StatusInProgress:
		return "in progress"
	default:
		return "pending"
	}
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	totalSecs := int(secs)
	if totalSecs < 3600 {
		return fmt.Sprintf("%dm %ds", totalSecs/60, totalSecs%60)
	}
	hours, mins, remainSecs := totalSecs/3600, (totalSecs%3600)/60, totalSecs%60
	parts := []string{fmt.Sprintf("%dh", hours)}
	if mins > 0 || remainSecs > 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if remainSecs > 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainSecs))
	}
	return strings.Join(parts, " ")
}

func fitDetailLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(tuistyle.Sanitize(s)) <= width {
		return s
	}
	return runewidth.Truncate(tuistyle.Sanitize(s), width, "…")
}

func wrapLine(s string, width int) []string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var out []string
	var current strings.Builder
	currentWidth := 0
	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
			currentWidth = 0
		}
	}
	for _, word := range words {
		wordWidth := runewidth.StringWidth(word)
		if wordWidth > width {
			flush()
			remaining := word
			for runewidth.StringWidth(remaining) > width {
				chunk := runewidth.Truncate(remaining, width, "")
				if chunk == "" {
					_, size := utf8.DecodeRuneInString(remaining)
					chunk = remaining[:size]
				}
				out = append(out, chunk)
				remaining = remaining[len(chunk):]
			}
			if remaining != "" {
				current.WriteString(remaining)
				currentWidth = runewidth.StringWidth(remaining)
			}
			continue
		}
		switch {
		case currentWidth == 0:
			current.WriteString(word)
			currentWidth = wordWidth
		case currentWidth+1+wordWidth > width:
			flush()
			current.WriteString(word)
			currentWidth = wordWidth
		default:
			current.WriteByte(' ')
			current.WriteString(word)
			currentWidth += 1 + wordWidth
		}
	}
	flush()
	return out
}

func bareWorkflowName(s string) string {
	if s == "" {
		return ""
	}
	base := filepath.Base(s)
	if ext := filepath.Ext(base); ext == ".yaml" || ext == ".yml" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
