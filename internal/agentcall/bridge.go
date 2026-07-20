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

type BridgeSender func(context.Context, string, Request) (Response, error)

type BridgeOptions struct {
	Send             BridgeSender
	ProgressInterval time.Duration
	NewRequestID     func() string
}

// EnvironmentSender returns the bridge's sole transport operation. The
// supervising process authenticates, validates, accepts, and executes the
// call; this process only preserves typed framing.
func EnvironmentSender(getenv func(string) string) BridgeSender {
	return func(ctx context.Context, requestID string, request Request) (Response, error) {
		payload, err := json.Marshal(request)
		if err != nil {
			return Response{}, err
		}
		raw, err := control.SendAgentCallFromEnvironment(ctx, requestID, payload, getenv)
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

// NewServer constructs the process-local MCP bridge. It publishes only the
// canonical call_agent tool and translates requests; execution policy remains
// in the supervising Runner reached by Send.
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
		if options.Send == nil {
			return mcpErrorResult(&Error{Code: CodeControlFailure, Message: "agent-call control sender is unavailable"}), nil
		}

		type sendResult struct {
			response Response
			err      error
		}
		done := make(chan sendResult, 1)
		go func() {
			response, err := options.Send(ctx, options.NewRequestID(), input)
			done <- sendResult{response: response, err: err}
		}()

		var ticker *time.Ticker
		var ticks <-chan time.Time
		progressToken := request.Params.GetProgressToken()
		if progressToken != nil {
			ticker = time.NewTicker(options.ProgressInterval)
			defer ticker.Stop()
			ticks = ticker.C
		}
		progress := float64(0)
		for {
			select {
			case outcome := <-done:
				if outcome.err != nil {
					if errors.Is(outcome.err, context.Canceled) || errors.Is(outcome.err, context.DeadlineExceeded) {
						return nil, outcome.err
					}
					return mcpErrorResult(&Error{Code: CodeControlFailure, Message: outcome.err.Error()}), nil
				}
				if outcome.response.Error != nil {
					return mcpErrorResult(outcome.response.Error), nil
				}
				if outcome.response.Result == nil {
					return mcpErrorResult(&Error{Code: CodeControlFailure, Message: "agent-call control response has no result"}), nil
				}
				return mcpSuccessResult(outcome.response.Result), nil
			case <-ticks:
				progress++
				_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Progress:      progress,
					Message:       "called agent is still running",
				})
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	})
	return server
}

func mcpSuccessResult(result *Result) *mcp.CallToolResult {
	return structuredMCPResult(result, false)
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
