package agentcall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBridgePublishesStartPollAndCancelAndMapsStructuredSuccess(t *testing.T) {
	server := NewServer(BridgeOptions{Send: func(_ context.Context, messageType, requestID string, payload json.RawMessage) (Response, error) {
		if messageType != "agent_call" || requestID == "" {
			t.Fatalf("forwarded type=%q id=%q", messageType, requestID)
		}
		var request Request
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatal(err)
		}
		if request.Prompt != "do it" || request.Agent == nil || *request.Agent != "implementor" {
			t.Fatalf("forwarded request=%#v", request)
		}
		return Response{CallID: "call-1", Status: StatusAccepted, Target: &Target{Kind: TargetAgent, Name: "implementor"}}, nil
	}})
	clientSession, closeSessions := connectBridgeTest(t, server, nil)
	defer closeSessions()

	tools, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range tools.Tools {
		got[tool.Name] = true
	}
	for _, name := range []string{ToolName, GetToolName, CancelToolName} {
		if !got[name] {
			t.Fatalf("tools = %#v, missing %s", tools.Tools, name)
		}
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
	var gotResponse Response
	if err := json.Unmarshal(raw, &gotResponse); err != nil {
		t.Fatal(err)
	}
	if gotResponse.CallID != "call-1" || gotResponse.Status != StatusAccepted || gotResponse.Result != nil {
		t.Fatalf("structured result = %#v", gotResponse)
	}
}

func TestBridgeGetAndCancelUseCallIDWithoutStartingAChild(t *testing.T) {
	var calls []string
	server := NewServer(BridgeOptions{Send: func(_ context.Context, messageType, _ string, payload json.RawMessage) (Response, error) {
		calls = append(calls, messageType)
		var request CallIDRequest
		if err := json.Unmarshal(payload, &request); err != nil || request.CallID != "call-1" {
			t.Fatalf("payload = %s", payload)
		}
		if messageType == "agent_call_cancel" {
			return Response{CallID: "call-1", Status: StatusCanceled, Error: &Error{Code: CodeCallCanceled, Message: "canceled", CallID: "call-1"}}, nil
		}
		return Response{CallID: "call-1", Status: StatusRunning, Target: &Target{Kind: TargetAgent, Name: "implementor"}, Elapsed: "1s"}, nil
	}})
	clientSession, closeSessions := connectBridgeTest(t, server, nil)
	defer closeSessions()

	getResult, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: GetToolName, Arguments: map[string]any{"call_id": "call-1"},
	})
	if err != nil || getResult.IsError {
		t.Fatalf("get result = %#v err=%v", getResult, err)
	}
	cancelResult, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: CancelToolName, Arguments: map[string]any{"call_id": "call-1"},
	})
	if err != nil || !cancelResult.IsError {
		t.Fatalf("cancel result = %#v err=%v", cancelResult, err)
	}
	if diff := cmp.Diff([]string{"agent_call_get", "agent_call_cancel"}, calls); diff != "" {
		t.Fatalf("control RPCs mismatch (-want +got):\n%s", diff)
	}
}

func TestBridgeMapsStructuredToolFailure(t *testing.T) {
	server := NewServer(BridgeOptions{Send: func(context.Context, string, string, json.RawMessage) (Response, error) {
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
	var got Response
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.CallID != "internal" || got.Error == nil || got.Error.Code != CodeExecutionFailed || got.Error.Message != "child failed" {
		t.Fatalf("structured error = %s", raw)
	}
}

func TestBridgeRetriesLostControlResponseWithSameRequestID(t *testing.T) {
	var requestIDs []string
	generated := 0
	launches := 0
	accepted := make(map[string]Response)
	server := NewServer(BridgeOptions{
		NewRequestID: func() string {
			generated++
			return fmt.Sprintf("request-%d", generated)
		},
		Send: func(_ context.Context, _ string, requestID string, _ json.RawMessage) (Response, error) {
			requestIDs = append(requestIDs, requestID)
			response, duplicate := accepted[requestID]
			if !duplicate {
				launches++
				response = Response{CallID: "call-1", Status: StatusAccepted}
				accepted[requestID] = response
				return Response{}, errors.New("lost control response")
			}
			return response, nil
		},
	})
	clientSession, closeSessions := connectBridgeTest(t, server, nil)
	defer closeSessions()

	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolName, Arguments: map[string]any{"prompt": "do it", "agent": "implementor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool result = %#v", result)
	}
	if diff := cmp.Diff([]string{"request-1", "request-1"}, requestIDs); diff != "" {
		t.Fatalf("control request IDs mismatch (-want +got):\n%s", diff)
	}
	if generated != 1 {
		t.Fatalf("generated request IDs = %d, want 1", generated)
	}
	if launches != 1 {
		t.Fatalf("supervised launches = %d, want 1", launches)
	}
}

func TestBridgeMapsStructuralValidationToStableToolError(t *testing.T) {
	called := false
	server := NewServer(BridgeOptions{Send: func(context.Context, string, string, json.RawMessage) (Response, error) {
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
		Send: func(ctx context.Context, _ string, _ string, _ json.RawMessage) (Response, error) {
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
