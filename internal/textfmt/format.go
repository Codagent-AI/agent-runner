// Package textfmt provides text formatting utilities for console output.
package textfmt

import (
	"fmt"
	"regexp"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\][^\x1b]*\x1b\\|\x1b[(\)*+][A-Z0-9]|\x1b[A-Z0-9=>]`)

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

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
