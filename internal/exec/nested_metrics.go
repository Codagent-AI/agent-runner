package exec

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
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
	Consumer           string            `json:"consumer"`
	ContextID          string            `json:"context_id"`
	ExecutionSessionID string            `json:"execution_session_id"`
	ParentAttemptID    string            `json:"parent_attempt_id"`
	StepID             string            `json:"step_id"`
	Prefix             string            `json:"prefix"`
	Project            string            `json:"project"`
	Configuration      string            `json:"configuration,omitempty"`
	CreatedAt          string            `json:"created_at"`
	AcceptedRecords    []json.RawMessage `json:"accepted_records,omitempty"`
	Receipt            string            `json:"receipt,omitempty"`
	Acknowledged       bool              `json:"acknowledged,omitempty"`
}

var runValidatorMetricsCommand = func(project string, args ...string) ([]byte, error) {
	command := osexec.Command("agent-validator", args...) // #nosec G204 -- fixed Validator executable and Runner-owned arguments.
	command.Dir = project
	return command.Output()
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

// emitNestedMetricCapture performs one bounded protocol export after the child
// exits. It journals verified records before acknowledgment; failures remain a
// visible gap and never affect the child command's result.
func emitNestedMetricCapture(ctx *model.ExecutionContext, step *model.Step, prefix string, capture *nestedMetricsCapture) {
	if capture == nil || capture.contextID == "" {
		return
	}
	launch, err := readValidatorMetricsLaunch(capture.path)
	if err == nil {
		records, receipt, exportErr := exportValidatorMetrics(launch)
		if exportErr == nil {
			launch.AcceptedRecords = records
			launch.Receipt = receipt
			if err = stateio.WriteJSONDurable(capture.path, launch); err == nil && receipt != "" {
				err = acknowledgeValidatorMetrics(launch)
				if err == nil {
					launch.Acknowledged = true
					err = stateio.WriteJSONDurable(capture.path, launch)
				}
			}
			if err == nil {
				for _, record := range records {
					emitValidatorAttempt(ctx, prefix, capture, record)
				}
				return
			}
		}
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

func readValidatorMetricsLaunch(path string) (validatorMetricsLaunch, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path was created under the current run session directory.
	if err != nil {
		return validatorMetricsLaunch{}, err
	}
	var launch validatorMetricsLaunch
	if err := json.Unmarshal(data, &launch); err != nil {
		return validatorMetricsLaunch{}, err
	}
	if launch.Consumer != "agent-runner" || launch.ContextID == "" || launch.Project == "" {
		return validatorMetricsLaunch{}, fmt.Errorf("invalid validator metrics launch")
	}
	return launch, nil
}

type validatorExport struct {
	OK              bool `json:"ok"`
	ConsumerContext struct {
		Consumer  string `json:"consumer"`
		ContextID string `json:"context_id"`
	} `json:"consumer_context"`
	Records []json.RawMessage `json:"records"`
	Receipt *string           `json:"receipt"`
}

func exportValidatorMetrics(launch validatorMetricsLaunch) ([]json.RawMessage, string, error) {
	args := []string{"metrics", "export", "--project", launch.Project, "--consumer", launch.Consumer, "--context", launch.ContextID, "--protocol-version", "1", "--measurement-version", "1", "--max-records", "100", "--max-bytes", "1000000"}
	if launch.Configuration != "" {
		args = append(args, "--config", launch.Configuration)
	}
	data, err := runValidatorMetricsCommand(launch.Project, args...)
	if err != nil {
		return nil, "", err
	}
	var response validatorExport
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, "", err
	}
	if !response.OK || response.ConsumerContext.Consumer != launch.Consumer || response.ConsumerContext.ContextID != launch.ContextID {
		return nil, "", fmt.Errorf("invalid validator metrics export scope")
	}
	for _, record := range response.Records {
		if err := validateValidatorRecord(record, launch.ContextID); err != nil {
			return nil, "", err
		}
	}
	receipt := ""
	if response.Receipt != nil {
		receipt = *response.Receipt
	}
	if len(response.Records) > 0 && receipt == "" {
		return nil, "", fmt.Errorf("validator metrics export has no receipt")
	}
	return response.Records, receipt, nil
}

func validateValidatorRecord(raw json.RawMessage, contextID string) error {
	var record struct {
		RecordType         string `json:"record_type"`
		RecordID           string `json:"record_id"`
		Revision           int    `json:"revision"`
		MeasurementVersion int    `json:"measurement_schema_version"`
		Context            struct {
			Consumer  string `json:"consumer"`
			ContextID string `json:"context_id"`
		} `json:"original_consumer_context"`
		Payload json.RawMessage                                     `json:"payload"`
		Digest  struct{ Algorithm, Canonicalization, Value string } `json:"digest"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	if record.RecordType != "model_attempt" || record.RecordID == "" || record.Revision < 1 || record.MeasurementVersion != 1 || record.Context.Consumer != "agent-runner" || record.Context.ContextID != contextID || len(record.Payload) == 0 || record.Digest.Algorithm != "sha256" || record.Digest.Canonicalization != "rfc8785" || len(record.Digest.Value) != 64 {
		return fmt.Errorf("invalid validator metrics record")
	}
	return nil
}

func acknowledgeValidatorMetrics(launch validatorMetricsLaunch) error {
	args := []string{"metrics", "acknowledge", "--project", launch.Project, "--consumer", launch.Consumer, "--context", launch.ContextID, "--protocol-version", "1", "--receipt", launch.Receipt}
	if launch.Configuration != "" {
		args = append(args, "--config", launch.Configuration)
	}
	_, err := runValidatorMetricsCommand(launch.Project, args...)
	return err
}

func emitValidatorAttempt(ctx *model.ExecutionContext, prefix string, capture *nestedMetricsCapture, raw json.RawMessage) {
	var record struct {
		RecordID string `json:"record_id"`
		Payload  struct {
			AttemptID, SessionID, InvocationID, Outcome string
			Tokens                                      map[string]struct {
				Availability string `json:"availability"`
				Value        *int64 `json:"value"`
			} `json:"tokens"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &record) != nil || record.Payload.AttemptID == "" {
		return
	}
	tokens := model.TokenCounts{}
	for source, target := range map[string]string{"input_total": model.TokenInput, "cache_read": model.TokenCachedInput, "cache_write": model.TokenCacheWrite, "output": model.TokenOutput, "reasoning": model.TokenReasoning} {
		if value := record.Payload.Tokens[source]; value.Availability != "unavailable" && value.Value != nil {
			tokens[target] = *value.Value
		}
	}
	usage := model.UsageRecord{Status: model.UsageCollected, CLI: "agent-validator", Source: "agent-validator:metrics", Tokens: tokens, Completeness: model.CompletenessPartial}
	if total := record.Payload.Tokens["normalized_total"]; total.Availability == "available" && total.Value != nil {
		usage.TokenTotals = &model.TokenTotals{Total: *total.Value}
	}
	identity := model.ExecutionIdentity{ExecutionSessionID: ctx.ExecutionSessionID, StepID: record.Payload.AttemptID, Prefix: executionIdentityPrefix(ctx) + "/" + record.Payload.InvocationID, StepType: "agent", Kind: "nested-agent", SessionID: record.Payload.SessionID, AgentInvoked: true, Role: "implementation-validator", Tool: "agent-validator"}
	emitAudit(ctx, audit.Event{Timestamp: formatAuditTimestamp(time.Now()), Prefix: prefix, Type: audit.EventNestedAgentEnd, Data: map[string]any{"identity": identity, "usage": usage, "outcome": record.Payload.Outcome, "duration_ms": int64(0), "invocation_id": record.Payload.InvocationID, "parent_attempt_id": capture.parentAttemptID}})
}
