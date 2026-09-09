package exec

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// nestedMetricsCapture retains the original attribution for a Validator
// delivery context. The name remains temporarily because shell and script
// execution share this capture lifecycle; it no longer describes JSONL.
type nestedMetricsCapture struct {
	path            string
	contextID       string
	parentAttemptID string
	command         string
}

// validatorMetricsLaunch is local recovery metadata. It never becomes part of
// exported model evidence: the opaque context is the only correlation value
// passed to Validator.
type validatorMetricsLaunch struct {
	Consumer           string `json:"consumer"`
	ContextID          string `json:"context_id"`
	ExecutionSessionID string `json:"execution_session_id"`
	ParentAttemptID    string `json:"parent_attempt_id"`
	StepID             string `json:"step_id"`
	Prefix             string `json:"prefix"`
	Project            string `json:"project"`
	Configuration      string `json:"configuration,omitempty"`
	CreatedAt          string `json:"created_at"`
}

func prepareNestedMetrics(step *model.Step, ctx *model.ExecutionContext, command string) (*nestedMetricsCapture, error) {
	capture, environment, err := prepareNestedMetricsEnvironment(step, ctx)
	if err != nil {
		return nil, err
	}
	if len(environment) == 0 {
		capture.command = command
		return capture, nil
	}
	capture.command = "env AGENT_RUNNER_METRICS_CONSUMER=agent-runner" +
		" AGENT_RUNNER_METRICS_CONTEXT=" + textfmt.ShellQuote(capture.contextID) +
		" sh -c " + textfmt.ShellQuote(command)
	return capture, nil
}

func prepareNestedMetricsEnvironment(step *model.Step, ctx *model.ExecutionContext) (*nestedMetricsCapture, []string, error) {
	if step.MetricsSource == "" {
		return &nestedMetricsCapture{}, nil, nil
	}
	contextID, err := newMetricsContextID()
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(ctx.SessionDir, "validator-metrics", "contexts")
	project := step.Workdir
	if project == "" {
		project = ctx.WorkingDir
	}
	if project == "" {
		project = "."
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve validator metrics project: %w", err)
	}
	launch := validatorMetricsLaunch{
		Consumer: "agent-runner", ContextID: contextID, ExecutionSessionID: ctx.ExecutionSessionID,
		ParentAttemptID: contextID, StepID: step.ID, Prefix: executionIdentityPrefix(ctx),
		Project: project, Configuration: validatorConfiguration(project), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	path := filepath.Join(dir, contextID+".json")
	if err := stateio.WriteJSONDurable(path, launch); err != nil {
		return nil, nil, fmt.Errorf("persist validator metrics launch: %w", err)
	}
	return &nestedMetricsCapture{path: path, contextID: contextID, parentAttemptID: contextID}, []string{
		"AGENT_RUNNER_METRICS_CONSUMER=agent-runner",
		"AGENT_RUNNER_METRICS_CONTEXT=" + contextID,
	}, nil
}

func newMetricsContextID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate validator metrics context: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func validatorConfiguration(project string) string {
	for _, candidate := range []string{
		filepath.Join(project, ".validator", "config.yml"),
		filepath.Join(project, ".gauntlet", "config.yml"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// emitNestedMetricCapture records an explicit delivery gap. A later delivery
// pass replaces this projection with accepted protocol records; console output
// and the retired JSONL handoff are deliberately never parsed.
func emitNestedMetricCapture(ctx *model.ExecutionContext, step *model.Step, prefix string, capture *nestedMetricsCapture) {
	if capture == nil || capture.contextID == "" {
		return
	}
	identityPrefix := executionIdentityPrefix(ctx)
	if identityPrefix != "" {
		identityPrefix += "/"
	}
	identityPrefix += step.ID
	identity := model.ExecutionIdentity{
		ExecutionSessionID: ctx.ExecutionSessionID, StepID: step.MetricsSource + "-metrics-gap", Prefix: identityPrefix,
		StepType: "agent", Kind: "nested-agent", AgentInvoked: true, Role: "implementation-validator", Tool: step.MetricsSource,
	}
	emitAudit(ctx, audit.Event{
		Timestamp: formatAuditTimestamp(time.Now()), Prefix: prefix, Type: audit.EventNestedAgentEnd,
		Data: map[string]any{
			"identity": identity,
			"usage": model.UsageRecord{Status: model.UsageUnavailable, Reason: model.UnavailableNestedMetricsMissing,
				CLI: step.MetricsSource, Source: "agent-runner:validator-metrics"},
			"estimated_api_cost_usd": (*float64)(nil), "outcome": "unavailable", "duration_ms": int64(0),
			"parent_attempt_id": capture.parentAttemptID, "metrics_context": capture.contextID,
		},
	})
}
