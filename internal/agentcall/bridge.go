package agentcall

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codagent/agent-runner/internal/control"
)

const defaultProgressInterval = 15 * time.Second

type BridgeSender func(ctx context.Context, messageType, requestID string, payload json.RawMessage) (Response, error)

type BridgeOptions struct {
	Send             BridgeSender
	ProgressInterval time.Duration
	NewRequestID     func() string
}

// EnvironmentSender returns the bridge's sole transport operation. The
// supervising process authenticates, validates, accepts, and executes the
// call; this process only preserves typed framing.
func EnvironmentSender(getenv func(string) string) BridgeSender {
	return func(ctx context.Context, messageType, requestID string, payload json.RawMessage) (Response, error) {
		raw, err := control.SendAgentCallFromEnvironment(ctx, messageType, requestID, payload, getenv)
		if err != nil {
			return Response{}, err
		}
		var response Response
		if err := json.Unmarshal(raw, &response); err != nil {
			return Response{}, err
		}
		return response, nil
	}
}

// RunStdio performs MCP lifecycle negotiation over the official SDK's stdio
// transport and runs until the host disconnects.
func RunStdio(ctx context.Context, getenv func(string) string) error {
	return NewServer(BridgeOptions{Send: EnvironmentSender(getenv)}).Run(ctx, &mcp.StdioTransport{})
}

// NewServer constructs the process-local MCP bridge. It publishes the start,
// poll, and cancel tools and translates requests; execution policy remains in
// the supervising Runner reached by Send.
func NewServer(options BridgeOptions) *mcp.Server {
	if options.NewRequestID == nil {
		options.NewRequestID = uuid.NewString
	}
	if options.ProgressInterval <= 0 {
		options.ProgressInterval = defaultProgressInterval
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "agent-runner", Version: "1"}, nil)
	server.AddTool(Tool(), func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, validation := DecodeRequest(request.Params.Arguments)
		if validation != nil {
			return mcpErrorResult(validation), nil
		}
		payload, err := json.Marshal(input)
		if err != nil {
			return mcpErrorResult(&Error{Code: CodeControlFailure, Message: err.Error()}), nil
		}
		return invokeBridge(ctx, request, options, control.MessageAgentCall, payload, true)
	})
	server.AddTool(GetTool(), func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, validation := DecodeCallIDRequest(request.Params.Arguments)
		if validation != nil {
			return mcpErrorResult(validation), nil
		}
		payload, err := json.Marshal(input)
		if err != nil {
			return mcpErrorResult(&Error{Code: CodeControlFailure, Message: err.Error()}), nil
		}
		return invokeBridge(ctx, request, options, control.MessageAgentCallGet, payload, false)
	})
	server.AddTool(CancelTool(), func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, validation := DecodeCallIDRequest(request.Params.Arguments)
		if validation != nil {
			return mcpErrorResult(validation), nil
		}
		payload, err := json.Marshal(input)
		if err != nil {
			return mcpErrorResult(&Error{Code: CodeControlFailure, Message: err.Error()}), nil
		}
		return invokeBridge(ctx, request, options, control.MessageAgentCallCancel, payload, false)
	})
	return server
}

func invokeBridge(
	ctx context.Context,
	request *mcp.CallToolRequest,
	options BridgeOptions,
	messageType string,
	payload json.RawMessage,
	progress bool,
) (*mcp.CallToolResult, error) {
	if options.Send == nil {
		return mcpErrorResult(&Error{Code: CodeControlFailure, Message: "agent-call control sender is unavailable"}), nil
	}

	type sendResult struct {
		response Response
		err      error
	}
	requestID := options.NewRequestID()
	done := make(chan sendResult, 1)
	go func() {
		response, err := sendWithRetry(ctx, options.Send, messageType, requestID, payload)
		done <- sendResult{response: response, err: err}
	}()

	var ticker *time.Ticker
	var ticks <-chan time.Time
	progressToken := request.Params.GetProgressToken()
	if progress && progressToken != nil {
		ticker = time.NewTicker(options.ProgressInterval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	progressCount := float64(0)
	for {
		select {
		case outcome := <-done:
			if outcome.err != nil {
				if errors.Is(outcome.err, context.Canceled) || errors.Is(outcome.err, context.DeadlineExceeded) {
					return nil, outcome.err
				}
				return mcpErrorResult(&Error{Code: CodeControlFailure, Message: outcome.err.Error()}), nil
			}
			return mcpResponseResult(outcome.response), nil
		case <-ticks:
			progressCount++
			_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: progressToken,
				Progress:      progressCount,
				Message:       "called agent is still running",
			})
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func sendWithRetry(ctx context.Context, send BridgeSender, messageType, requestID string, payload json.RawMessage) (Response, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		response, err := send(ctx, messageType, requestID, payload)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

func mcpResponseResult(response Response) *mcp.CallToolResult {
	if response.Error != nil && response.CallID == "" {
		return mcpErrorResult(response.Error)
	}
	if response.Error == nil && response.CallID == "" && response.Result == nil {
		return mcpErrorResult(&Error{Code: CodeControlFailure, Message: "agent-call control response is missing call_id"})
	}
	return structuredMCPResult(response, response.Error != nil)
}

func mcpErrorResult(failure *Error) *mcp.CallToolResult {
	return structuredMCPResult(failure, true)
}

func structuredMCPResult(value any, isError bool) *mcp.CallToolResult {
	raw, err := json.Marshal(value)
	if err != nil {
		raw = []byte(`{"code":"control_failure","message":"encode call_agent MCP result"}`)
		isError = true
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: json.RawMessage(raw),
		IsError:           isError,
	}
}
