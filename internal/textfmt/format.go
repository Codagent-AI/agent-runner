// Package textfmt provides text formatting utilities for console output.
package textfmt

import (
	"fmt"
	"strings"
)

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
