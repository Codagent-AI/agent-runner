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
		ctx.AgentDeprecations = model.NewAgentDeprecationState()
	}
	for _, warning := range warnings {
		if !ctx.AgentDeprecations.Mark(warning.Alias) {
			continue
		}
		message := warning.String()
		if log != nil {
			log.Printf("agent-runner: warning: %s\n", message)
		}
		if ctx.AuditLogger != nil {
			emitAudit(ctx, audit.Event{
				Timestamp: formatAuditTimestamp(time.Now()),
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
