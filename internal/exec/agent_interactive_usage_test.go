package exec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
			ctx := &model.ExecutionContext{SessionDir: t.TempDir()}
			result, _ := InvokeAgent(&AgentInvocation{Adapter: &cli.ClaudeAdapter{}, Args: []string{"claude"}, CLI: "claude", SessionID: session, InvocationContext: cli.ContextAutonomousInteractive, Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + config}, direct: &directInvocation{ctx: ctx}}, nil, &mockLogger{})
			if result.Usage.Status != model.UsageCollected || result.Usage.Tokens[model.TokenOutput] != 8 {
				t.Fatalf("%+v", result)
			}
			if (result.Outcome == OutcomeSuccess) != completed {
				t.Fatalf("%+v", result)
			}
		})
	}
}
