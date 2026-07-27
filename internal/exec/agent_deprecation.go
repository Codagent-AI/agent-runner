package exec

import (
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
)

// EmitAgentDeprecations surfaces structured profile alias warnings once per
// workflow run and retains the same evidence in the audit trail.
func EmitAgentDeprecations(ctx *model.ExecutionContext, log Logger, warnings []config.Deprecation) {
	if ctx == nil {
		return
	}
	if ctx.AgentDeprecations == nil {
		ctx.AgentDeprecations = make(map[string]bool)
	}
	for _, warning := range warnings {
		if ctx.AgentDeprecations[warning.Alias] {
			continue
		}
		ctx.AgentDeprecations[warning.Alias] = true
		message := warning.String()
		if log != nil {
			log.Printf("agent-runner: warning: %s\n", message)
		}
		if ctx.AuditLogger != nil {
			ctx.AuditLogger.Emit(audit.Event{
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Type:      audit.EventWarning,
				Data: map[string]any{
					"message":   message,
					"alias":     warning.Alias,
					"canonical": warning.Canonical,
				},
			})
		}
	}
}
