package agentcall

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBridgePublishesOnlyCallAgentAndMapsStructuredSuccess(t *testing.T) {
	server := NewServer(BridgeOptions{Send: func(_ context.Context, requestID string, request Request) (Response, error) {
		if requestID == "" || request.Prompt != "do it" || request.Agent == nil || *request.Agent != "implementor" {
			t.Fatalf("forwarded request id=%q request=%#v", requestID, request)
		}
		return Response{CallID: "internal-call-id", Result: &Result{Target: request.Target(), Response: "done"}}, nil
	}})
	clientSession, closeSessions := connectBridgeTest(t, server, nil)
	defer closeSessions()

	tools, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != ToolName {
		t.Fatalf("tools = %#v", tools.Tools)
	}
	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolName, Arguments: map[string]any{"prompt": "do it", "agent": "implementor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool result = %#v", result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got != (Result{Target: Target{Kind: TargetAgent, Name: "implementor"}, Response: "done"}) {
		t.Fatalf("structured result = %#v", got)
	}
}

func TestBridgeMapsStructuredToolFailure(t *testing.T) {
	server := NewServer(BridgeOptions{Send: func(context.Context, string, Request) (Response, error) {
		return Response{CallID: "internal", Error: &Error{Code: CodeExecutionFailed, Message: "child failed"}}, nil
	}})
	clientSession, closeSessions := connectBridgeTest(t, server, nil)
	defer closeSessions()
	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolName, Arguments: map[string]any{"prompt": "do it", "agent": "implementor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("tool result = %#v, want IsError", result)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	if string(raw) != `{"code":"execution_failed","message":"child failed"}` {
		t.Fatalf("structured error = %s", raw)
	}
}

func TestBridgeMapsStructuralValidationToStableToolError(t *testing.T) {
	called := false
	server := NewServer(BridgeOptions{Send: func(context.Context, string, Request) (Response, error) {
		called = true
		return Response{}, nil
	}})
	clientSession, closeSessions := connectBridgeTest(t, server, nil)
	defer closeSessions()
	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolName, Arguments: map[string]any{"prompt": "do it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || called {
		t.Fatalf("tool result=%#v forwarded=%v", result, called)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var failure Error
	if err := json.Unmarshal(raw, &failure); err != nil || failure.Code != CodeInvalidTarget {
		t.Fatalf("structured validation error = %s (%v)", raw, err)
	}
}

func TestBridgeReportsRateLimitedProgressAndPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := NewServer(BridgeOptions{
		ProgressInterval: 10 * time.Millisecond,
		Send: func(ctx context.Context, _ string, _ Request) (Response, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return Response{}, ctx.Err()
		},
	})
	var mu sync.Mutex
	var progress []float64
	clientSession, closeSessions := connectBridgeTest(t, server, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, request *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			progress = append(progress, request.Params.Progress)
			mu.Unlock()
		},
	})
	defer closeSessions()
	ctx, cancel := context.WithCancel(context.Background())
	params := &mcp.CallToolParams{Name: ToolName, Arguments: map[string]any{"prompt": "do it", "agent": "implementor"}}
	params.SetProgressToken("progress-token")
	done := make(chan error, 1)
	go func() { _, err := clientSession.CallTool(ctx, params); done <- err }()
	<-started
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		count := len(progress)
		mu.Unlock()
		if count >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge emitted fewer than two progress notifications")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("CallTool error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("bridge did not propagate MCP cancellation")
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(progress); i++ {
		if progress[i] <= progress[i-1] {
			t.Fatalf("progress is not increasing: %v", progress)
		}
	}
}

func connectBridgeTest(t *testing.T, server *mcp.Server, options *mcp.ClientOptions) (clientSession *mcp.ClientSession, closeSessions func()) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, options)
	clientSession, err = client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}
