package exec

import (
	"fmt"
	"strings"
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
		Timestamp: startTime.UTC().Format(time.RFC3339Nano),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      data,
	})
}

func emitStepEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, outcome string, data map[string]any, step *model.Step) {
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["identity"]; !ok {
		data["identity"] = executionIdentity(ctx, step, "step", 0, false, "", "")
	}
	if _, ok := data["usage"]; !ok {
		if step.StepType() == "agent" {
			data["usage"] = model.UsageRecord{
				Status: model.UsageUnavailable, Reason: model.UnavailableUnsupportedAdapter, CLI: step.CLI, Source: "agent-runner",
			}
		} else {
			data["usage"] = model.UsageRecord{
				Status: model.UsageCollected, Tokens: model.TokenCounts{}, Source: "agent-runner", Completeness: model.CompletenessComplete,
			}
		}
	}
	if _, ok := data["estimated_api_cost_usd"]; !ok {
		data["estimated_api_cost_usd"] = (*float64)(nil)
	}
	data["outcome"] = outcome
	data["duration_ms"] = time.Since(startTime).Milliseconds()
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      data,
	})
}

func executionIdentity(ctx *model.ExecutionContext, step *model.Step, kind string, iteration int, agentInvoked bool, cliName, sessionID string) model.ExecutionIdentity {
	return model.ExecutionIdentity{
		StepID: step.ID, Prefix: executionIdentityPrefix(ctx), StepType: step.StepType(), Kind: kind,
		Iteration: iteration, CLI: cliName, SessionID: sessionID, SessionStrategy: string(step.Session), AgentInvoked: agentInvoked,
	}
}

func executionIdentityPrefix(ctx *model.ExecutionContext) string {
	parts := make([]string, 0, len(ctx.NestingPath)*2)
	for _, segment := range ctx.NestingPath {
		stepID := segment.StepID
		if segment.Iteration != nil {
			stepID = fmt.Sprintf("%s:%d", stepID, *segment.Iteration)
		}
		if stepID != "" {
			parts = append(parts, stepID)
		}
		if segment.SubWorkflowName != "" {
			parts = append(parts, "sub:"+segment.SubWorkflowName)
		}
	}
	return strings.Join(parts, "/")
}
