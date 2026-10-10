package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/externaluser"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
)

func TestExternalUserInput(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{ExternalUser: &model.ExternalUserSettings{Dir: t.TempDir()}})
	if got := resolveInvocationContext(model.ModeInteractive, ctx, "claude", false, nil); got != cli.ContextExternalUser {
		t.Fatalf("context = %s", got)
	}
	if got := resolveInvocationContext(model.ModeAutonomous, ctx, "claude", false, nil); got != cli.ContextAutonomousHeadless {
		t.Fatalf("autonomous context = %s", got)
	}
	input := buildAdapterInput(&model.Step{ID: "proposal"}, ctx, &config.ResolvedAgent{}, &cli.ClaudeAdapter{}, "Ask about scope", "", "session", false, cli.ContextExternalUser, "/runner")
	if input.Prompt != "Let's start the proposal step" || !strings.Contains(input.SystemPrompt, "Ask about scope") || strings.Contains(input.SystemPrompt, autonomyPreamble) {
		t.Fatalf("input = %#v", input)
	}
	if len(input.DisallowedTools) != 1 || input.DisallowedTools[0] != "AskUserQuestion" {
		t.Fatalf("disallowed = %v", input.DisallowedTools)
	}
	if err := validateCompletionIntegration(&input); err != nil {
		t.Fatal(err)
	}
}

type externalTestAdapter struct{ cli.CodexAdapter }

func (*externalTestAdapter) Checkpoint(context.Context, string) (cli.Checkpoint, error) {
	return cli.Checkpoint{Artifact: "test"}, nil
}
func (*externalTestAdapter) WaitForCommittedTurn(context.Context, string, cli.Checkpoint) error {
	return nil
}
func (*externalTestAdapter) WaitForCommittedTurnWithReceipt(context.Context, string, cli.Checkpoint, string) error {
	return nil
}

type externalTestRunner struct {
	mockRunner
	turns      int
	prompts    []string
	suffixes   []string
	fail       bool
	completeAt int
}

func (r *externalTestRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	r.turns++
	r.prompts = append(r.prompts, options.Args[len(options.Args)-1])
	r.suffixes = append(r.suffixes, options.OutputCopySuffix)
	options.NotifyStarted()
	if r.fail {
		return ProcessResult{Started: true, ExitCode: 2, Stderr: "CLI failed"}, nil
	}
	if r.turns == r.completeAt {
		env := map[string]string{}
		for _, entry := range options.Env {
			key, value, _ := strings.Cut(entry, "=")
			env[key] = value
		}
		if _, err := control.SendControlEventFromEnvironment(context.Background(), "complete_step", func(key string) string { return env[key] }); err != nil {
			return ProcessResult{}, err
		}
	}
	return ProcessResult{Started: true, Stdout: `{"type":"thread.started","thread_id":"session"}` + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":"Question?"}}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":2}}`}, nil
}

type externalCompletionGate struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (g externalCompletionGate) Emit(event audit.Event) {
	if event.Type == audit.EventCompletionAcknowledged {
		close(g.started)
		<-g.release
	}
}

func TestExternalTurnCompletionDelivery(t *testing.T) {
	for _, scenario := range []string{"delayed", "stalled", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			release := make(chan struct{})
			started := make(chan struct{})
			ctx := model.NewRootContext(&model.RootContextOptions{SessionDir: t.TempDir()})
			if pid, err := runlock.Acquire(ctx.SessionDir); err != nil || pid != 0 {
				t.Fatalf("lock pid=%d error=%v", pid, err)
			}
			defer runlock.Delete(ctx.SessionDir)
			ctx.AuditLogger = externalCompletionGate{started: started, release: release}
			server, err := controlServerForContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			adapter := &externalTestAdapter{}
			attempt := server.ActivateAttempt(runCtx, "proposal", control.AttemptOptions{CompletionEligible: true})
			defer server.Deactivate()
			input := &AgentInvocation{Context: runCtx, Adapter: adapter, Args: []string{"codex", "initial"}, Env: attempt.Environment(), CLI: "codex", SessionID: "session", InvocationContext: cli.ContextExternalUser}
			done := make(chan struct{})
			var result AgentInvocationResult
			go func() {
				result, err = invokeExternalTurn(input, &externalTestRunner{completeAt: 1}, &mockLogger{}, server, &attempt, adapter, func() string { return "session" }, ctx)
				close(done)
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("completion was not acknowledged")
			}
			select {
			case <-done:
				t.Fatal("turn finished before accepted completion was delivered")
			case <-time.After(50 * time.Millisecond):
			}
			if scenario == "delayed" {
				close(release)
			} else if scenario == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				if scenario == "delayed" {
					if err != nil || result.Outcome != OutcomeSuccess {
						t.Fatalf("acknowledged completion lost when process exited: outcome=%s err=%v", result.Outcome, err)
					}
				} else {
					if result.Outcome != OutcomeFailed || err == nil {
						t.Fatalf("undelivered completion must fail: outcome=%s err=%v", result.Outcome, err)
					}
					if scenario == "stalled" && !strings.Contains(err.Error(), "completion delivery") {
						t.Fatalf("missing delivery timeout diagnostic: %v", err)
					}
					if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
						t.Fatalf("missing cancellation: %v", err)
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("accepted completion wait was not bounded")
			}
		})
	}
}

func TestExternalUserLoop(t *testing.T) {
	for _, scenario := range []string{"complete", "abort", "malformed", "timeout", "process-failure", "stale-reply", "pending-request", "pending-reply"} {
		t.Run(scenario, func(t *testing.T) {
			exchange := t.TempDir()
			runDir := t.TempDir()
			if pid, err := runlock.Acquire(runDir); err != nil || pid != 0 {
				t.Fatalf("lock pid=%d error=%v", pid, err)
			}
			defer runlock.Delete(runDir)
			ctx := model.NewRootContext(&model.RootContextOptions{SessionDir: runDir, ExternalUser: &model.ExternalUserSettings{Dir: exchange, Timeout: "1s"}})
			if scenario == "timeout" {
				ctx.ExternalUser.Timeout = "10ms"
			}
			step := &model.Step{ID: "proposal", Session: model.SessionNew, Workdir: t.TempDir()}
			adapter := &externalTestAdapter{}
			runner := &externalTestRunner{completeAt: 2, fail: scenario == "process-failure"}
			id := externaluser.Identity{StepKey: "proposal", Attempt: 1, Turn: 1}
			command, err := completionExecutableForContext(cli.ContextExternalUser)
			if err != nil {
				t.Fatal(err)
			}
			delivered := cli.CompletionCommand{Executable: command, Args: []string{"step", "complete"}}.ShellCommand()
			if scenario == "pending-request" || scenario == "pending-reply" {
				ctx.SessionIDs[step.ID] = "session"
				runner.completeAt = 1
				event := externaluser.Event{Type: "request_written", Identity: id, Request: &externaluser.Request{SchemaVersion: 1, Step: "proposal", SessionID: "session"}, CompletionCommand: delivered}
				if scenario == "pending-reply" {
					event.Type = "reply_acted"
					event.Reply = &externaluser.Reply{SchemaVersion: 1, Text: "- option a"}
				}
				if err := externaluser.Append(runDir, &event); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "stale-reply" {
				os.WriteFile(filepath.Join(exchange, id.ReplyName()), []byte(`{"schema_version":1,"text":"stale"}`), 0o644)
			}
			if scenario != "timeout" && scenario != "process-failure" && scenario != "stale-reply" && scenario != "pending-reply" {
				done := make(chan struct{})
				defer close(done)
				go func() {
					for {
						select {
						case <-done:
							return
						case <-time.After(time.Millisecond):
						}
						paths, _ := filepath.Glob(filepath.Join(exchange, "*.request.json"))
						if len(paths) == 0 {
							continue
						}
						body := `{"schema_version":1,"text":"- option a"}`
						if scenario == "abort" {
							body = `{"schema_version":1,"action":"abort","reason":"turn cap reached"}`
						}
						if scenario == "malformed" {
							body = `{"schema_version":1}`
						}
						externaluser.AtomicWrite(strings.TrimSuffix(paths[0], ".request.json")+".reply.json", []byte(body), 0o644)
						return
					}
				}()
			}
			initialSession := "session"
			outcome, err := executeExternalUser(step, ctx, runner, &mockLogger{}, adapter, &config.ResolvedAgent{}, []string{"codex", "--", "initial"}, nil, "codex", initialSession, false, "[proposal]", time.Now(), nil, nil)
			if server, ok := ctx.Control.(*control.ControlServer); ok {
				defer server.Close()
			}
			success := scenario == "complete" || strings.HasPrefix(scenario, "pending-")
			if success {
				if err != nil || outcome != OutcomeSuccess {
					t.Fatalf("outcome=%s err=%v", outcome, err)
				}
				if runner.prompts[len(runner.prompts)-1] != "- option a" {
					t.Fatalf("prompts=%v", runner.prompts)
				}
				if runner.turns != runner.completeAt {
					t.Fatal(runner.turns)
				}
			} else if err == nil || outcome != OutcomeFailed {
				t.Fatalf("outcome=%s err=%v", outcome, err)
			}
			if diff := cmp.Diff([]string{".attempt-1.turn-1", ".attempt-1.turn-2"}, runner.suffixes); scenario == "complete" && diff != "" {
				t.Fatalf("output copy suffixes (-want +got):\n%s", diff)
			}
			if scenario == "pending-reply" {
				paths, _ := filepath.Glob(filepath.Join(exchange, "*.request.json"))
				if len(paths) != 0 {
					t.Fatal(paths)
				}
			}
			if scenario == "process-failure" {
				paths, _ := filepath.Glob(filepath.Join(exchange, "*.request.json"))
				if len(paths) != 0 {
					t.Fatal(paths)
				}
			}
		})
	}
}

func TestExternalUserUnsupportedSteps(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{ExternalUser: &model.ExternalUserSettings{Dir: t.TempDir()}})
	runner := &mockRunner{}
	log := &mockLogger{}
	if outcome, err := ExecuteShellStep(&model.Step{ID: "shell", Command: "echo forbidden", Mode: model.ModeInteractive}, ctx, runner, log); outcome != OutcomeFailed || err == nil {
		t.Fatalf("shell outcome=%s err=%v", outcome, err)
	}
	if outcome, err := ExecuteCheckStep(&model.Step{ID: "check", Command: "echo forbidden", Mode: model.ModeInteractive, Repair: &model.Repair{Prompt: "repair", Session: model.SessionNew}}, ctx, runner, log); outcome != OutcomeFailed || err == nil || !strings.Contains(err.Error(), "external-user mode") {
		t.Fatalf("check outcome=%s err=%v", outcome, err)
	}
	if outcome, err := ExecuteUIStep(&model.Step{ID: "ui"}, ctx, log); outcome != OutcomeFailed || err == nil {
		t.Fatalf("UI outcome=%s err=%v", outcome, err)
	}
	if outcome, err := ExecuteAgentStep(&model.Step{ID: "agent", CLI: "cursor", Prompt: "hello", Mode: model.ModeInteractive}, ctx, runner, log); outcome != OutcomeFailed || err != nil {
		t.Fatalf("agent outcome=%s err=%v", outcome, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsupported step launched %v", runner.calls)
	}
}

func TestExternalUserNestedKeysAreUnique(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{ExternalUser: &model.ExternalUserSettings{}})
	root, err := externalUserExchangeOptions(ctx, &model.Step{ID: "define.proposal"}, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx.NestingPath = []model.NestingSegment{{StepID: "define"}}
	nested, err := externalUserExchangeOptions(ctx, &model.Step{ID: "proposal"}, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if root.key == nested.key {
		t.Fatalf("root and nested keys collide: %s", root.key)
	}
	if nested.key != "define.proposal" {
		t.Fatal(nested.key)
	}
}

func TestExternalUserReplayCommandRefreshAndAbort(t *testing.T) {
	for _, action := range []string{"", "abort"} {
		t.Run(action, func(t *testing.T) {
			ctx := model.NewRootContext(&model.RootContextOptions{SessionDir: t.TempDir(), ExternalUser: &model.ExternalUserSettings{Dir: t.TempDir()}})
			logger := &recordingAuditLogger{}
			ctx.AuditLogger = logger
			id := externaluser.Identity{StepKey: "proposal", Attempt: 1, Turn: 1}
			event := externaluser.Event{Type: "reply_acted", Identity: id, Reply: &externaluser.Reply{SchemaVersion: 1, Text: "- option a", Action: action, Reason: "turn cap reached"}, CompletionCommand: "/old step complete"}
			if err := externaluser.Append(ctx.SessionDir, &event); err != nil {
				t.Fatal(err)
			}
			prompt, gotID, err := externalUserReplay(context.Background(), ctx, "[proposal]", "proposal", 0, "/current step complete", "/current")
			if err != nil || gotID != id {
				t.Fatalf("id=%+v error=%v", gotID, err)
			}
			if action == "abort" {
				if prompt != nil {
					t.Fatal(*prompt)
				}
				return
			}
			if prompt == nil || *prompt != "- option a"+completionInstruction("/current") {
				t.Fatalf("prompt=%v", prompt)
			}
			if findAuditEvent(logger.events, audit.EventType("external_user_completion_refreshed")) == nil {
				t.Fatal("missing refresh event")
			}
			last, err := externaluser.Last(ctx.SessionDir, "proposal")
			if err != nil || last.CompletionCommand != "/current step complete" || last.Reply.Text != "- option a" {
				t.Fatalf("last=%+v error=%v", last, err)
			}
		})
	}
}

func TestExternalUserReplyTurnCompletion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    AgentInvocationResult
		sessionID string
		wantError bool
		wantDone  bool
	}{
		{name: "process failure", result: AgentInvocationResult{Outcome: OutcomeFailed, ExitCode: 2}, sessionID: "session", wantError: true, wantDone: true},
		{name: "missing session", result: AgentInvocationResult{Outcome: OutcomeFailed}, wantError: true, wantDone: true},
		{name: "next exchange", result: AgentInvocationResult{Outcome: OutcomeFailed}, sessionID: "session"},
		{name: "accepted completion", result: AgentInvocationResult{Outcome: OutcomeSuccess, ExitCode: -1}, wantDone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := model.NewRootContext(&model.RootContextOptions{SessionDir: t.TempDir(), ExternalUser: &model.ExternalUserSettings{Dir: t.TempDir()}})
			id := externaluser.Identity{StepKey: "proposal", Attempt: 1, Turn: 1}
			text := "- option a\ncontinue verbatim"
			command := "/runner step complete"
			if err := externaluser.Append(ctx.SessionDir, &externaluser.Event{Type: "reply_acted", Identity: id, Reply: &externaluser.Reply{SchemaVersion: 1, Text: text}, CompletionCommand: command}); err != nil {
				t.Fatal(err)
			}
			done, err := externalTurnFinished(ctx, &tc.result, &text, id, command, tc.sessionID)
			if done != tc.wantDone || (err != nil) != tc.wantError {
				t.Fatalf("done=%v error=%v", done, err)
			}
			prompt, _, err := externalUserReplay(context.Background(), ctx, "[proposal]", id.StepKey, 0, command, "/runner")
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError {
				if prompt == nil || *prompt != text {
					t.Fatalf("failed turn lost original reply: %v", prompt)
				}
			} else if prompt != nil {
				t.Fatal("completed turn replayed reply")
			}
		})
	}
}
