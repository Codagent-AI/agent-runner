// Package session provides session ID resolution for agent steps.
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

	activeRepository := ""
	if ctx.ActiveRepository != nil && ctx.ActiveRepository.Name != "default" {
		activeRepository = ctx.ActiveRepository.Name
	}
	current := ctx.ParentContext
	for current != nil {
		if activeRepository != "" && (current.ActiveRepository == nil || current.ActiveRepository.Name != activeRepository) {
			return "", fmt.Errorf(`session "inherit" failed: no parent session exists in repository %q`, activeRepository)
		}
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
// Returns empty string (no error) when no prior session exists, allowing
// the CLI adapter to start a fresh session.
func ResolveResumeSession(ctx *model.ExecutionContext) (string, error) {
	return getMostRecentSessionID(ctx), nil
}

// ResolveNamedSession returns the persisted session ID for the given role name,
// or empty string if the named session has not been created yet in this run.
func ResolveNamedSession(name string, ctx *model.ExecutionContext) string {
	return ctx.LookupNamedSession(name)
}

func getMostRecentSessionID(ctx *model.ExecutionContext) string {
	if ctx.LastSessionStepID == "" {
		return ""
	}
	return ctx.SessionIDs[ctx.LastSessionStepID]
}
