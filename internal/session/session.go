package session

import (
	"fmt"

	"github.com/codagent/agent-runner/internal/model"
)

// ResolveInheritSession walks the parent context chain until crossing a
// sub-workflow boundary (different WorkflowFile), then returns that
// context's most recent session ID.
func ResolveInheritSession(ctx *model.ExecutionContext) (string, error) {
	if ctx.ParentContext == nil {
		return "", nil // warn caller: no parent
	}

	current := ctx.ParentContext
	for current != nil {
		if current.WorkflowFile != ctx.WorkflowFile {
			id := getMostRecentSessionID(current)
			if id != "" {
				return id, nil
			}
			return "", fmt.Errorf(`session "inherit" failed: no parent session exists`)
		}
		current = current.ParentContext
	}

	return "", fmt.Errorf(`session "inherit" failed: no parent session exists`)
}

// ResolveResumeSession returns the most recent session ID from the current
// context's SessionIDs. Does NOT cross sub-workflow boundaries.
func ResolveResumeSession(ctx *model.ExecutionContext) (string, error) {
	id := getMostRecentSessionID(ctx)
	if id == "" {
		return "", fmt.Errorf(`session "resume" failed: no prior session in current workflow`)
	}
	return id, nil
}

func getMostRecentSessionID(ctx *model.ExecutionContext) string {
	if ctx.LastSessionStepID == "" {
		return ""
	}
	return ctx.SessionIDs[ctx.LastSessionStepID]
}
