package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/cli"
)

func TestProvisionedAgentCallPreservesExplicitClientDeadline(t *testing.T) {
	for _, adapterName := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		t.Run(adapterName, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			runner := filepath.Join(t.TempDir(), "agent-runner")
			if err := os.WriteFile(runner, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatalf("write test runner: %v", err)
			}
			adapter, err := cli.Get(adapterName)
			if err != nil {
				t.Fatalf("get adapter: %v", err)
			}
			input := &cli.BuildArgsInput{
				Prompt: "use call_agent", Context: cli.ContextAutonomousHeadless, RunID: "deadline-test",
				RunnerIntegration: &cli.RunnerIntegration{AgentCall: &cli.MCPServerCommand{
					Executable: runner, Args: []string{"internal", "call-agent-mcp"},
				}},
			}
			if _, err := cli.BuildInvocationArgs(adapter, input); err != nil {
				t.Fatalf("provision adapter args: %v", err)
			}
			if _, err := cli.SpawnEnvForInvocation(adapter, input); err != nil {
				t.Fatalf("provision adapter environment: %v", err)
			}

			assertExplicitClientDeadline(t)
		})
	}
}

func assertExplicitClientDeadline(t *testing.T) {
	t.Helper()
	serverCanceled := make(chan struct{})
	server := agentcall.NewServer(agentcall.BridgeOptions{Send: func(ctx context.Context, _ string, _ agentcall.Request) (agentcall.Response, error) {
		<-ctx.Done()
		close(serverCanceled)
		return agentcall.Response{}, ctx.Err()
	}})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "deadline-test", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: agentcall.ToolName, Arguments: map[string]any{"prompt": "wait", "agent": "implementor"},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallTool error = %v, want explicit client deadline", err)
	}
	select {
	case <-serverCanceled:
	case <-time.After(time.Second):
		t.Fatal("bridge did not propagate the explicit client deadline")
	}
}
