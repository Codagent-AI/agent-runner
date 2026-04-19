// Package flowctl provides skip and break condition evaluation for workflow steps.
package flowctl

import "strings"

// SkipIfShellPrefix marks the shell-expression form of skip_if.
// A value of "sh: <cmd>" runs <cmd> and skips the step when it exits 0.
const SkipIfShellPrefix = "sh:"

// ShouldSkip determines whether a step should be skipped based on the
// "previous_success" keyword form. Callers that support the shell-expression
// form must handle it separately; use ShellSkipCommand to detect and extract it.
func ShouldSkip(skipIf string, lastStepOutcome *string) bool {
	if skipIf == "" {
		return false
	}
	if skipIf == "previous_success" {
		return lastStepOutcome != nil && *lastStepOutcome == "success"
	}
	return false
}

// ShellSkipCommand returns the shell command portion of a "sh:<cmd>"
// skip_if expression, or ("", false) if the value is not in that form.
func ShellSkipCommand(skipIf string) (string, bool) {
	cmd, ok := strings.CutPrefix(skipIf, SkipIfShellPrefix)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(cmd), true
}

// EvaluateBreakIf evaluates whether a loop should break.
func EvaluateBreakIf(breakIf, outcome string) bool {
	if breakIf == "" {
		return false
	}
	if breakIf == "success" {
		return outcome == "success"
	}
	if breakIf == "failure" {
		return outcome == "failed"
	}
	return false
}
