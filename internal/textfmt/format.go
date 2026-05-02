// Package textfmt provides text formatting utilities for console output.
package textfmt

import (
	"fmt"
	"regexp"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\x1b\][^\x07]*\x07|\x1b\][^\x1b]*\x1b\\|\x1b[(\)*+][A-Z0-9]|\x1b[\x40-\x5f]`)

// c0Re matches C0 control characters except TAB (0x09) and LF (0x0A).
var c0Re = regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)

// StripANSI removes ANSI escape sequences and C0 control characters from s,
// preserving TAB and LF.
func StripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return c0Re.ReplaceAllString(s, "")
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
