package exec

import (
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

func emitStepStart(ctx *model.ExecutionContext, prefix string, startTime time.Time, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["context"]; !ok {
		data["context"] = contextSnapshot(ctx)
	}
	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      data,
	})
}

func emitStepEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, outcome string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["outcome"] = outcome
	data["duration_ms"] = time.Since(startTime).Milliseconds()
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      data,
	})
}
