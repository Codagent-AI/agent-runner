package flowctl

// ShouldSkip determines whether a step should be skipped.
func ShouldSkip(skipIf string, lastStepOutcome *string) bool {
	if skipIf == "" {
		return false
	}
	if skipIf == "previous_success" {
		return lastStepOutcome != nil && *lastStepOutcome == "success"
	}
	return false
}

// EvaluateBreakIf evaluates whether a loop should break.
func EvaluateBreakIf(breakIf string, outcome string) bool {
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
