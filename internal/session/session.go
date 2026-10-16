// Package session provides session ID resolution for agent steps.
package session

import (
	"fmt"

	"github.com/codagent/agent-runner/internal/model"
)

// ResolveInheritSession walks the parent context chain past the current
// sub-workflow boundary (different WorkflowFile) and returns the most recent
// session of the nearest enclosing context that has one. An enclosing
// sub-workflow that has run no session of its own (for example, because its
// agent steps were skipped) is transparent: the walk continues to its parent.
func ResolveInheritSession(ctx *model.ExecutionContext) (string, error) {
	if ctx.ParentContext == nil {
		return "", nil // warn caller: no parent
	}

	crossed := false
	for current := ctx.ParentContext; current != nil; current = current.ParentContext {
		if !crossed && current.WorkflowFile == ctx.WorkflowFile {
			continue
		}
		crossed = true
		if id := getMostRecentSessionID(current); id != "" {
			return id, nil
		}
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
	if ctx.NamedSessions == nil {
		return ""
	}
	return ctx.NamedSessions[name]
}

func getMostRecentSessionID(ctx *model.ExecutionContext) string {
	if ctx.LastSessionStepID == "" {
		return ""
	}
	return ctx.SessionIDs[ctx.LastSessionStepID]
}
