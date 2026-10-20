package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	stdexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// scriptCallRunner exercises the invocation contract with an actual subprocess,
// including the production process-group cleanup and output wrappers.
type scriptCallRunner struct {
	callTestRunner
	script string
}

func (r *scriptCallRunner) RunAgent(o *AgentProcessOptions) (ProcessResult, error) {
	cmd := stdexec.CommandContext(o.Context, r.script, o.Args...)
	cmd.Dir = o.Workdir
	cmd.Env = BuildAgentEnvironment(os.Environ(), o.DropEnv, o.Env)
	ConfigureAgentCommand(cmd, o.Supervision)
	var stdout, stderr bytes.Buffer
	out, errout := io.Discard, io.Discard
	if o.StdoutWrapper != nil {
		out = o.StdoutWrapper(out)
	}
	if o.StderrWrapper != nil {
		errout = o.StderrWrapper(errout)
	}
	cmd.Stdout = io.MultiWriter(out, &stdout)
	cmd.Stderr = io.MultiWriter(errout, &stderr)
	if err := cmd.Start(); err != nil {
		return ProcessResult{}, err
	}
	o.NotifyStarted()
	err := cmd.Wait()
	if closer, ok := out.(io.Closer); ok {
		_ = closer.Close()
	}
	if closer, ok := errout.(io.Closer); ok {
		_ = closer.Close()
	}
	return ProcessResult{Started: true, ExitCode: cmd.ProcessState.ExitCode(), Stdout: stdout.String(), Stderr: stderr.String()}, err
}
func fakeCallScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func bridgeCallClient(t *testing.T, h *AgentCallHandler, options *mcp.ClientOptions) (*mcp.ClientSession, agentcall.BridgeSender) {
	t.Helper()
	dir := t.TempDir()
	if _, err := runlock.Acquire(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runlock.Delete(dir) })
	proof, err := runlock.ProveHeld(dir)
	if err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("/tmp", "ac-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	s, err := control.NewControlServer(&control.ControlConfig{RunID: "test", RunDir: dir, TempDir: socketDir, LockProof: proof})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	attempt := s.ActivateAttempt(context.Background(), "parent", control.AttemptOptions{AgentCallEligible: true, AgentCallHandler: h})
	env := attempt.EnvironmentMap()
	send := agentcall.EnvironmentSender(func(k string) string { return env[k] })
	server := agentcall.NewServer(agentcall.BridgeOptions{Send: send, ProgressInterval: 100 * time.Millisecond})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, options)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, send
}
func mcpCallResponse(t *testing.T, r *mcp.CallToolResult) agentcall.Response {
	t.Helper()
	raw, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return decodeCallResponse(t, raw)
}
func TestDeadlineBridgeINT001(t *testing.T) {
	dir := t.TempDir()
	script := fakeCallScript(t, "echo $$ > pid\nexec sleep 60\n")
	options := testAgentCallOptions(dir, &scriptCallRunner{script: script}, &callTestAdapter{})
	options.Git = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("git unavailable in process-cleanup test")
	}
	logger := &recordingAuditLogger{}
	options.Context.AuditLogger = metrics.NewPipeline(metrics.NewCollector(dir, "test", "calls", time.Now()), logger)
	h := NewAgentCallHandler(options)
	client, _ := bridgeCallClient(t, h, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: agentcall.ToolName, Arguments: map[string]any{"agent": "implementor", "prompt": "x", "timeout": "2s"}})
	if err != nil {
		t.Fatal(err)
	}
	response := mcpCallResponse(t, result)
	if response.Error == nil || response.Error.Code != agentcall.CodeTimedOut || response.Status != agentcall.StatusFailed || response.Details.Exit != "terminated" {
		t.Fatalf("%+v", response)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("deadline failed to release blocked MCP request")
	}
	events := agentCallAuditEvents(logger.events)
	if len(events) != 2 || events[1].Data["outcome"] != "failed" || events[1].Data["error_code"] != "timed_out" || events[1].Data["timeout"] != "2s" || events[1].Data["exit"] != "terminated" {
		t.Fatalf("deadline audit: %+v", events)
	}
	var artifact metrics.Artifact
	data, err := os.ReadFile(filepath.Join(dir, "run-metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Steps) != 1 || artifact.Steps[0].ErrorCode != "timed_out" || artifact.Steps[0].Outcome != "failed" {
		t.Fatalf("deadline metrics: %+v", artifact.Steps)
	}
	pidBytes, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("process %d survived", pid)
	}
}
func TestActivityBridgeINT002(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"structured", `printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"SECRET"}}]}}'; sleep 1`, "tool_use: Bash"},
		{"raw", "echo SECRET; sleep 1", "last output"},
		{"stderr", "echo STDERR-SECRET >&2; sleep 1", "last output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			a := &cli.ClaudeAdapter{}
			h := NewAgentCallHandler(testAgentCallOptions(dir, &scriptCallRunner{script: fakeCallScript(t, tc.body)}, a))
			var mu sync.Mutex
			var messages []string
			client, send := bridgeCallClient(t, h, &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, r *mcp.ProgressNotificationClientRequest) {
				mu.Lock()
				messages = append(messages, r.Params.Message)
				mu.Unlock()
			}})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			params := &mcp.CallToolParams{Name: agentcall.ToolName, Arguments: map[string]any{"agent": "implementor", "prompt": "x"}}
			params.SetProgressToken("p")
			result, err := client.CallTool(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			response := mcpCallResponse(t, result)
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			mu.Lock()
			defer mu.Unlock()
			found := false
			for _, m := range messages {
				if strings.Contains(m, tc.want) {
					found = true
				}
				if strings.Contains(m, "SECRET") || strings.ContainsAny(m, "\n\r") || len([]rune(m)) > 200 {
					t.Fatal(m)
				}
			}
			if !found {
				t.Fatalf("missing %s: %v", tc.want, messages)
			}
			unknown, err := send(ctx, control.MessageAgentCallActivity, "unknown", []byte(`{"call_id":"missing"}`))
			if err != nil || unknown.Error == nil || unknown.Error.Code != agentcall.CodeUnknownCall {
				t.Fatalf("unknown: %+v %v", unknown, err)
			}
		})
	}
}

func TestFastAndCanceledBridgeINT001(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		cancel     bool
	}{
		{"fast", "echo done", false},
		{"cancel", "exec sleep 60", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := testAgentCallOptions(t.TempDir(), &scriptCallRunner{script: fakeCallScript(t, tc.body)}, &callTestAdapter{})
			if tc.cancel {
				o.WaitBudget = 100 * time.Millisecond
			}
			h := NewAgentCallHandler(o)
			client, _ := bridgeCallClient(t, h, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			raw, err := client.CallTool(ctx, &mcp.CallToolParams{Name: agentcall.ToolName, Arguments: map[string]any{"agent": "implementor", "prompt": "x", "timeout": "10s"}})
			if err != nil {
				t.Fatal(err)
			}
			r := mcpCallResponse(t, raw)
			if tc.cancel {
				raw, err = client.CallTool(ctx, &mcp.CallToolParams{Name: agentcall.CancelToolName, Arguments: map[string]any{"call_id": r.CallID}})
				if err != nil {
					t.Fatal(err)
				}
				r = mcpCallResponse(t, raw)
				if r.Error == nil || r.Error.Code != agentcall.CodeCallCanceled || r.Status != agentcall.StatusCanceled || r.Details.Exit != "terminated" {
					t.Fatalf("cancel: %+v", r)
				}
			} else if r.Error != nil || r.Status != agentcall.StatusSucceeded || r.Details.Exit != "exited" {
				t.Fatalf("fast: %+v", r)
			}
		})
	}
}

func TestSettlementRacesBridgeINT001(t *testing.T) {
	for _, kind := range []string{"deadline-after-exit", "cancel-after-exit", "deadline-before-exit"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			body := "echo done"
			if kind == "deadline-before-exit" {
				body = "echo $$ > pid; exec sleep 60"
			}
			o := testAgentCallOptions(dir, &scriptCallRunner{script: fakeCallScript(t, body)}, &callTestAdapter{})
			paused, release := make(chan struct{}), make(chan struct{})
			timerReady := make(chan func(), 1)
			o.AfterFunc = func(_ time.Duration, f func()) *time.Timer { timerReady <- f; return time.NewTimer(time.Hour) }
			o.afterSettle = func() { close(paused); <-release }
			logger := &recordingAuditLogger{}
			o.Context.AuditLogger = logger
			h := NewAgentCallHandler(o)
			client, _ := bridgeCallClient(t, h, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan *mcp.CallToolResult, 1)
			go func() {
				raw, err := client.CallTool(ctx, &mcp.CallToolParams{Name: agentcall.ToolName, Arguments: map[string]any{"agent": "implementor", "prompt": "x", "timeout": "10s"}})
				if err != nil {
					t.Error(err)
				}
				done <- raw
			}()
			fire := <-timerReady
			if kind == "deadline-before-exit" {
				deadline := time.Now().Add(2 * time.Second)
				for {
					if _, err := os.Stat(filepath.Join(dir, "pid")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("child did not launch")
					}
					time.Sleep(time.Millisecond)
				}
				go fire()
			}
			select {
			case <-paused:
			case <-ctx.Done():
				t.Fatal("settlement hook did not run")
			}
			var cancelDone chan *mcp.CallToolResult
			if kind == "deadline-after-exit" {
				fire()
			}
			if kind == "cancel-after-exit" {
				h.mu.Lock()
				id := h.active.callID
				h.mu.Unlock()
				cancelDone = make(chan *mcp.CallToolResult, 1)
				go func() {
					raw, err := client.CallTool(ctx, &mcp.CallToolParams{Name: agentcall.CancelToolName, Arguments: map[string]any{"call_id": id}})
					if err != nil {
						t.Error(err)
					}
					cancelDone <- raw
				}()
			}
			close(release)
			raw := <-done
			if raw == nil {
				t.Fatal("missing MCP result")
			}
			r := mcpCallResponse(t, raw)
			if kind == "deadline-before-exit" {
				if r.Error == nil || r.Error.Code != agentcall.CodeTimedOut || r.Details.Exit != "terminated" {
					t.Fatalf("%+v", r)
				}
			} else if r.Error != nil || r.Details.Exit != "exited" || *r.Details.ExitCode != 0 {
				t.Fatalf("%+v", r)
			}
			if cancelDone != nil {
				cr := mcpCallResponse(t, <-cancelDone)
				if cr.Status != r.Status || cr.Error != nil {
					t.Fatalf("late cancel: %+v", cr)
				}
			}
			events := agentCallAuditEvents(logger.events)
			want := "success"
			if kind == "deadline-before-exit" {
				want = "failed"
			}
			if len(events) != 2 || events[1].Data["outcome"] != want || events[1].Data["exit"] != r.Details.Exit {
				t.Fatalf("audit mismatch: %+v", events)
			}
		})
	}
}
