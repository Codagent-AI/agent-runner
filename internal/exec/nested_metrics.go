package exec

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/metrics"
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
	Executable         string                  `json:"executable"`
	ConfigurationHash  string                  `json:"configuration_hash"`
	StoreID            string                  `json:"store_id,omitempty"`
	RunID              string                  `json:"run_id"`
	Batches            []validatorMetricsBatch `json:"batches,omitempty"`
	Delivery           metrics.DeliveryContext `json:"delivery"`
	Consumer           string                  `json:"consumer"`
	ContextID          string                  `json:"context_id"`
	ExecutionSessionID string                  `json:"execution_session_id"`
	ParentAttemptID    string                  `json:"parent_attempt_id"`
	StepID             string                  `json:"step_id"`
	Prefix             string                  `json:"prefix"`
	Project            string                  `json:"project"`
	Configuration      string                  `json:"configuration,omitempty"`
	CreatedAt          string                  `json:"created_at"`
	AcceptedRecords    []json.RawMessage       `json:"accepted_records,omitempty"`
	Receipt            string                  `json:"receipt,omitempty"`
	Acknowledged       bool                    `json:"acknowledged,omitempty"`
}

var runValidatorMetricsCommand = func(launch *validatorMetricsLaunch, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := osexec.CommandContext(ctx, launch.Executable, args...) // #nosec G204 -- persisted, explicitly selected Validator executable.
	command.Dir = launch.Project
	var output boundedMetricsOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	return output.Bytes(), err
}

type boundedMetricsOutput struct{ bytes.Buffer }

func (b *boundedMetricsOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4000000 {
		return 0, fmt.Errorf("metrics_output_limit")
	}
	return b.Buffer.Write(p)
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
		" AGENT_RUNNER_VALIDATOR_EXECUTABLE=" + textfmt.ShellQuote(strings.TrimPrefix(environment[2], "AGENT_RUNNER_VALIDATOR_EXECUTABLE=")) +
		" AGENT_RUNNER_METRICS_CONTEXT=" + textfmt.ShellQuote(capture.contextID) +
		" sh -c " + textfmt.ShellQuote(command)
	return capture, nil
}

func prepareNestedMetricsEnvironment(step *model.Step, ctx *model.ExecutionContext) (*nestedMetricsCapture, []string, error) {
	capture, environment, err := prepareValidatorLaunch(step, ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-runner: warning: validator metrics instrumentation unavailable; validation will proceed")
		if sink, ok := ctx.AuditLogger.(validatorMetricsSink); ok {
			attr := metrics.Attribution{RunID: filepath.Base(ctx.SessionDir), ExecutionSessionID: ctx.ExecutionSessionID, StepID: step.ID, Prefix: executionIdentityPrefix(ctx)}
			_ = sink.IncorporateValidator(attr, "", nil, metrics.DeliveryContext{Attribution: attr, Delivery: "blocked", History: "partial", Gaps: []string{"instrumentation_unavailable"}})
		}
		return &nestedMetricsCapture{}, nil, nil
	}
	return capture, environment, nil
}
func prepareValidatorLaunch(step *model.Step, ctx *model.ExecutionContext) (*nestedMetricsCapture, []string, error) {
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
	launch.RunID = filepath.Base(ctx.SessionDir)
	launch.ConfigurationHash = configurationFingerprint(project, launch.Configuration)
	executable := os.Getenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE")
	if executable == "" {
		executable = "agent-validator"
	}
	launch.Executable, err = osexec.LookPath(executable)
	if err != nil {
		return nil, nil, fmt.Errorf("validator_executable_unavailable")
	}
	launch.Executable, err = filepath.Abs(launch.Executable)
	if err != nil {
		return nil, nil, err
	}
	if err := probeValidatorCapabilities(&launch); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, contextID+".json")
	if err := stateio.WriteJSONDurable(path, launch); err != nil {
		return nil, nil, fmt.Errorf("persist validator metrics launch: %w", err)
	}
	return &nestedMetricsCapture{path: path, contextID: contextID, parentAttemptID: contextID}, []string{
		"AGENT_RUNNER_METRICS_CONSUMER=agent-runner",
		"AGENT_RUNNER_METRICS_CONTEXT=" + contextID,
		"AGENT_RUNNER_VALIDATOR_EXECUTABLE=" + launch.Executable,
	}, nil
}

func newMetricsContextID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate validator metrics context: %w", err)
	}
	return hex.EncodeToString(random), nil
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
