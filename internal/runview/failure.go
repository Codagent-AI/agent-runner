package runview

import "fmt"

func failureReason(root *StepNode) string {
	if root == nil {
		return ""
	}
	if failed := findConcreteFailedCause(root); failed != nil {
		return failedReason(failed)
	}
	if exhausted := findExhaustedLoop(root); exhausted != nil {
		return exhaustedReason(exhausted)
	}
	if failed := findFailedCause(root); failed != nil {
		return failedReason(failed)
	}
	return ""
}

func exhaustedReason(node *StepNode) string {
	if total := loopTotal(node); total > 0 {
		return fmt.Sprintf("%s exhausted after %d of %d iterations without reaching a passing break condition", node.ID, node.IterationsCompleted, total)
	}
	return fmt.Sprintf("%s exhausted without reaching a passing break condition", node.ID)
}

func failedReason(node *StepNode) string {
	if node.ErrorMessage != "" {
		return fmt.Sprintf("%s failed: %s", node.ID, node.ErrorMessage)
	}
	if node.ExitCode != nil {
		return fmt.Sprintf("%s failed with exit code %d", node.ID, *node.ExitCode)
	}
	return fmt.Sprintf("%s failed", node.ID)
}

func findExhaustedLoop(root *StepNode) *StepNode {
	if root == nil {
		return nil
	}
	if root.Outcome == "exhausted" {
		return root
	}
	for i := len(root.Children) - 1; i >= 0; i-- {
		if found := findExhaustedLoop(root.Children[i]); found != nil {
			return found
		}
	}
	return nil
}

func findFailedCause(root *StepNode) *StepNode {
	if root == nil {
		return nil
	}
	for i := len(root.Children) - 1; i >= 0; i-- {
		if found := findFailedCause(root.Children[i]); found != nil {
			return found
		}
	}
	if root.Status == StatusFailed {
		return root
	}
	return nil
}

func findConcreteFailedCause(root *StepNode) *StepNode {
	if root == nil {
		return nil
	}
	for i := len(root.Children) - 1; i >= 0; i-- {
		if found := findConcreteFailedCause(root.Children[i]); found != nil {
			return found
		}
	}
	if root.Status != StatusFailed {
		return nil
	}
	if root.ErrorMessage != "" || root.ExitCode != nil || len(root.Children) == 0 {
		return root
	}
	return nil
}
