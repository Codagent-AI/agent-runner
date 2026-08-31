package exec

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

const nestedMetricsSchemaVersion = 1

type nestedInvocationRecord struct {
	SchemaVersion       int               `json:"schema_version"`
	InvocationID        string            `json:"invocation_id"`
	Role                string            `json:"role"`
	Tool                string            `json:"tool"`
	Outcome             string            `json:"outcome"`
	DurationMS          int64             `json:"duration_ms"`
	SessionID           string            `json:"session_id,omitempty"`
	Usage               model.UsageRecord `json:"usage"`
	EstimatedAPICostUSD *float64          `json:"estimated_api_cost_usd,omitempty"`
}

type nestedMetricsCapture struct {
	path            string
	parentAttemptID string
	command         string
}

func prepareNestedMetrics(step *model.Step, ctx *model.ExecutionContext, command string) (*nestedMetricsCapture, error) {
	if step.MetricsSource == "" {
		return &nestedMetricsCapture{command: command}, nil
	}
	dir := filepath.Join(ctx.SessionDir, "nested-metrics")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create nested metrics directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "handoff-*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("create nested metrics handoff: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close nested metrics handoff: %w", err)
	}
	instrumented := "env AGENT_RUNNER_NESTED_METRICS_PATH=" + textfmt.ShellQuote(path) +
		" AGENT_RUNNER_NESTED_METRICS_ROLE='implementation-validator'" +
		" AGENT_RUNNER_NESTED_METRICS_TOOL=" + textfmt.ShellQuote(step.MetricsSource) +
		" sh -c " + textfmt.ShellQuote(command)
	return &nestedMetricsCapture{path: path, parentAttemptID: filepath.Base(path), command: instrumented}, nil
}

func readNestedMetrics(path, expectedRole, expectedTool string) (records []nestedInvocationRecord, invalid bool) {
	file, err := os.Open(path) // #nosec G304 -- path is created beneath the run session directory.
	if err != nil {
		return nil, true
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	seen := make(map[string]struct{})
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var record nestedInvocationRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || !validNestedInvocation(&record, expectedRole, expectedTool) {
			invalid = true
			continue
		}
		if _, duplicate := seen[record.InvocationID]; duplicate {
			invalid = true
			continue
		}
		seen[record.InvocationID] = struct{}{}
		records = append(records, record)
	}
	return records, invalid || scanner.Err() != nil
}

func validNestedInvocation(record *nestedInvocationRecord, expectedRole, expectedTool string) bool {
	usage := record.Usage
	identity := usage.Identity
	if record.SchemaVersion != nestedMetricsSchemaVersion || record.InvocationID == "" ||
		record.Role != expectedRole || record.Tool != expectedTool || record.DurationMS < 0 ||
		!validNestedOutcome(record.Outcome) ||
		(usage.Status != model.UsageCollected && usage.Status != model.UsageUnavailable) ||
		usage.CLI == "" || usage.Provider == "" || usage.Model == "" ||
		identity.RequestedCLI == "" || identity.RequestedModel == "" ||
		identity.EffectiveCLI == "" || identity.EffectiveProvider == "" || identity.EffectiveModel == "" ||
		usage.CLI != identity.EffectiveCLI || usage.Provider != identity.EffectiveProvider || usage.Model != identity.EffectiveModel ||
		!validTokenCounts(usage.Tokens) || !validTokenCounts(usage.RawCumulative) ||
		!validTokenTotals(usage.Tokens, usage.TokenTotals) || !validTokenTotals(usage.RawCumulative, usage.RawCumulativeTokenTotals) {
		return false
	}
	return record.EstimatedAPICostUSD == nil ||
		(*record.EstimatedAPICostUSD >= 0 && !math.IsNaN(*record.EstimatedAPICostUSD) && !math.IsInf(*record.EstimatedAPICostUSD, 0))
}

func validNestedOutcome(outcome string) bool {
	return outcome == string(OutcomeSuccess) || outcome == string(OutcomeFailed) || outcome == string(OutcomeAborted)
}

func validTokenCounts(counts model.TokenCounts) bool {
	for _, count := range counts {
		if count < 0 {
			return false
		}
	}
	return true
}

func validTokenTotals(counts model.TokenCounts, totals *model.TokenTotals) bool {
	if totals == nil {
		return true
	}
	inputMax, inputOK := tokenCategorySum(counts, model.TokenInput, model.TokenCachedInput, model.TokenCacheWrite)
	outputMax, outputOK := tokenCategorySum(counts, model.TokenOutput, model.TokenReasoning)
	return inputOK && outputOK &&
		totals.Input >= counts[model.TokenInput] && totals.Input <= inputMax &&
		totals.Output >= counts[model.TokenOutput] && totals.Output <= outputMax &&
		totals.Total >= totals.Input && totals.Total-totals.Input == totals.Output
}

func tokenCategorySum(counts model.TokenCounts, categories ...string) (int64, bool) {
	var total int64
	for _, category := range categories {
		count := counts[category]
		if count > math.MaxInt64-total {
			return 0, false
		}
		total += count
	}
	return total, true
}

func emitNestedMetricCapture(ctx *model.ExecutionContext, step *model.Step, prefix string, capture *nestedMetricsCapture) {
	records, invalid := readNestedMetrics(capture.path, "implementation-validator", step.MetricsSource)
	_ = os.Remove(capture.path)
	identityPrefix := executionIdentityPrefix(ctx)
	if identityPrefix != "" {
		identityPrefix += "/"
	}
	identityPrefix += step.ID
	for i := range records {
		record := &records[i]
		identity := model.ExecutionIdentity{
			StepID: record.InvocationID, Prefix: identityPrefix,
			StepType: "agent", Kind: "nested-agent", CLI: record.Usage.CLI,
			SessionID: record.SessionID, AgentInvoked: true, Role: record.Role, Tool: record.Tool,
		}
		emitAudit(ctx, audit.Event{
			Timestamp: formatAuditTimestamp(time.Now()), Prefix: prefix,
			Type: audit.EventNestedAgentEnd, Data: map[string]any{
				"identity": identity, "usage": record.Usage,
				"estimated_api_cost_usd": record.EstimatedAPICostUSD,
				"outcome":                record.Outcome, "duration_ms": record.DurationMS,
				"invocation_id": record.InvocationID, "parent_attempt_id": capture.parentAttemptID,
			},
		})
	}
	if len(records) == 0 || invalid {
		reason := model.UnavailableNestedMetricsMissing
		if invalid {
			reason = model.UnavailableNestedMetricsInvalid
		}
		identity := model.ExecutionIdentity{
			StepID: step.MetricsSource + "-metrics-gap", Prefix: identityPrefix,
			StepType: "agent", Kind: "nested-agent", AgentInvoked: true,
			Role: "implementation-validator", Tool: step.MetricsSource,
		}
		emitAudit(ctx, audit.Event{
			Timestamp: formatAuditTimestamp(time.Now()), Prefix: prefix,
			Type: audit.EventNestedAgentEnd, Data: map[string]any{
				"identity":               identity,
				"usage":                  model.UsageRecord{Status: model.UsageUnavailable, Reason: reason, CLI: step.MetricsSource, Source: "agent-runner:nested-metrics"},
				"estimated_api_cost_usd": (*float64)(nil), "outcome": "unavailable", "duration_ms": int64(0),
				"parent_attempt_id": capture.parentAttemptID,
			},
		})
	}
}
