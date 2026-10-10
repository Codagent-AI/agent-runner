package exec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"

	"github.com/codagent/agent-runner/internal/interactive"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/model"
)

func TestInteractiveUsageGating(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter cli.Adapter
		context cli.InvocationContext
		want    model.UnavailableReason
	}{
		{"human-claude", &cli.ClaudeAdapter{}, cli.ContextInteractive, model.UnavailableInteractiveContext},
		{"autonomous-claude", &cli.ClaudeAdapter{}, cli.ContextAutonomousInteractive, model.UnavailableTranscriptMissing},
		{"autonomous-codex", &cli.CodexAdapter{}, cli.ContextAutonomousInteractive, model.UnavailableInteractiveContext},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := cli.InteractiveUsagePlan{SessionID: "11111111-1111-1111-1111-111111111111"}
			got, err := extractAgentUsage(tc.adapter, tc.name, tc.context, "", cli.UsageContext{Workdir: t.TempDir(), Env: []string{"CLAUDE_CONFIG_DIR=" + t.TempDir()}, InteractivePlan: &p})
			if err != nil {
				t.Fatal(err)
			}
			if got.Usage.Reason != tc.want {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestInteractiveClaudeUsageRetainedAfterProcessOutcome(t *testing.T) {
	original := interactiveRunnerFn
	defer func() { interactiveRunnerFn = original }()
	for _, completed := range []bool{true, false} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			config := t.TempDir()
			work := t.TempDir()
			session := "11111111-1111-1111-1111-111111111111"
			path := filepath.Join(config, "projects", "short", session+".jsonl")
			interactiveRunnerFn = func(_ []string, _ directRunOptions) (interactive.DirectResult, error) {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				raw := `{"type":"assistant","message":{"id":"m","model":"opus","content":[],"usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":8}}}` + "\n"
				if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
					t.Fatal(err)
				}
				if completed {
					return interactive.DirectResult{Started: true, Completed: true}, nil
				}
				return interactive.DirectResult{Started: true, DurabilityFailed: true, ExitCode: 3, DurabilityError: errors.New("failed")}, nil
			}
			if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte("invalid"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx := &model.ExecutionContext{SessionDir: t.TempDir()}
			result, _ := InvokeAgent(&AgentInvocation{Adapter: &cli.ClaudeAdapter{}, Args: []string{"claude"}, CLI: "claude", SessionID: session, InvocationContext: cli.ContextAutonomousInteractive, Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + config}, direct: &directInvocation{ctx: ctx}}, nil, &mockLogger{})
			if result.Usage.Status != model.UsageCollected || result.Usage.Tokens[model.TokenOutput] != 8 {
				t.Fatalf("%+v", result)
			}
			if !strings.Contains(result.CostReportError, filepath.Join(config, "settings.json")) {
				t.Fatalf("invocation lost settings diagnostic: %+v", result)
			}
			if (result.Outcome == OutcomeSuccess) != completed {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestInteractiveCostDiagnosticInTerminalAudit(t *testing.T) {
	ctx := makeCtx()
	logger := &recordingAuditLogger{}
	ctx.AuditLogger = logger
	step := model.Step{ID: "interactive", Mode: model.ModeAutonomous}
	detail := "invalid Claude settings /project/.claude/settings.json"
	invocation := AgentInvocationResult{Outcome: OutcomeSuccess, CLILaunched: true, CostUnavailableReason: model.UnavailableCostReportUnavailable, CostReportError: detail}
	_, err := finishAgentStep(ctx, "interactive", time.Now(), &step, "claude", "session", cli.ContextAutonomousInteractive, false, &invocation, nil, &mockLogger{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	end := findAuditEvent(logger.events, audit.EventStepEnd)
	if end.Data["cost_report_error"] != detail || end.Data["cost_unavailable_reason"] != model.UnavailableCostReportUnavailable {
		t.Fatalf("cost diagnostics missing: %+v", end.Data)
	}
}

func TestPrepareInvocationUsageExecutableFailure(t *testing.T) {
	original := osExecutableFn
	t.Cleanup(func() { osExecutableFn = original })
	t.Setenv("AGENT_RUNNER_EXECUTABLE", "")
	osExecutableFn = func() (string, error) { return "", errors.New("executable unavailable") }
	for _, invalidSettings := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidSettings), func(t *testing.T) {
			config := t.TempDir()
			if invalidSettings {
				if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte("invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			uc := cli.UsageContext{Workdir: t.TempDir(), StateDir: t.TempDir(), Env: []string{"CLAUDE_CONFIG_DIR=" + config}}
			input := &AgentInvocation{Adapter: &cli.ClaudeAdapter{}, SessionID: "11111111-1111-1111-1111-111111111111", InvocationContext: cli.ContextAutonomousInteractive}
			options := &AgentProcessOptions{Args: []string{"claude"}}
			direct := &directInvocation{}
			prepareInvocationUsage(input, options, direct, &uc)
			plan := uc.InteractivePlan
			if plan == nil || plan.ReportEnabled || plan.ReportReason != model.UnavailableCostReportUnavailable {
				t.Fatalf("plan: %+v", plan)
			}
			wantDiagnostic := "executable unavailable"
			if invalidSettings {
				wantDiagnostic = filepath.Join(config, "settings.json")
			}
			if !strings.Contains(plan.ReportError, wantDiagnostic) {
				t.Errorf("diagnostic lost: %+v; want %q", plan, wantDiagnostic)
			}
			if !invalidSettings && plan.ReportPath == "" {
				t.Fatal("expected prepared report path")
			}
			if plan.ReportPath != "" {
				if _, err := os.Stat(plan.ReportPath); !os.IsNotExist(err) {
					t.Errorf("disabled report still exists: %v", err)
				}
			}
			if direct.usagePlan.ReportError != plan.ReportError || direct.usagePlan.ReportEnabled {
				t.Errorf("direct plan differs: %+v", direct.usagePlan)
			}
		})
	}
}
