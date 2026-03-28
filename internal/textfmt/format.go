package textfmt

import (
	"fmt"
	"strings"
)

const separatorWidth = 60

// NestingInfo holds the minimum info needed to build a breadcrumb.
type NestingInfo struct {
	StepID          string
	Iteration       *int
	SubWorkflowName string
}

// BuildBreadcrumb creates a human-readable path from nesting segments and current step ID.
func BuildBreadcrumb(nestingPath []NestingInfo, stepID string) string {
	parts := make([]string, 0, len(nestingPath)*2+1)

	for _, seg := range nestingPath {
		parts = append(parts, seg.StepID)
		if seg.Iteration != nil {
			parts = append(parts, fmt.Sprintf("iteration %d", *seg.Iteration+1))
		}
		if seg.SubWorkflowName != "" {
			parts = append(parts, seg.SubWorkflowName)
		}
	}

	parts = append(parts, stepID)
	return strings.Join(parts, " > ")
}

// Separator returns a fixed-width horizontal rule of ━ characters.
func Separator() string {
	return strings.Repeat("━", separatorWidth)
}

// StepHeading returns a formatted step heading string.
func StepHeading(index, total int, breadcrumb, stepType string, skipped bool) string {
	label := stepType
	if skipped {
		label = "skipped"
	}
	return fmt.Sprintf("━━ step %d/%d: %s [%s] ━━", index+1, total, breadcrumb, label)
}
