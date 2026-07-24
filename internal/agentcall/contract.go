// Package agentcall defines the Runner-owned call_agent contract and runtime.
package agentcall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolName = "call_agent"

	CodeInvalidRequest  = "invalid_request"
	CodeInvalidTarget   = "invalid_target"
	CodeInvalidSession  = "invalid_session"
	CodeIneligible      = "call_agent_unavailable"
	CodeUnknownAgent    = "unknown_agent"
	CodeUnknownSession  = "unknown_session"
	CodeInvalidCLI      = "invalid_cli"
	CodeInvalidModel    = "invalid_model"
	CodeInvalidWorkdir  = "invalid_workdir"
	CodeSelfSession     = "self_session"
	CodeCallInProgress  = "call_in_progress"
	CodeExecutionFailed = "execution_failed"
	CodeCallCanceled    = "call_canceled"
	CodeControlFailure  = "control_failure"
)

const toolDescription = "Call one Agent Runner profile or declared named session. The tool is synchronous and serial: " +
	"wait for the active call to finish or cancel it before starting another. " +
	"The child receives the profile system prompt and supplied prompt without workflow-step enrichment."

// Request is the canonical call_agent input. Pointer fields distinguish an
// omitted optional field from an explicitly empty value during validation.
type Request struct {
	Prompt  string  `json:"prompt" jsonschema:"the task prompt for the called agent"`
	Agent   *string `json:"agent,omitempty" jsonschema:"agent profile name for a fresh session"`
	Session *string `json:"session,omitempty" jsonschema:"workflow-declared named session"`
	CLI     *string `json:"cli,omitempty" jsonschema:"CLI override for an agent target"`
	Model   *string `json:"model,omitempty" jsonschema:"model override"`
	Workdir *string `json:"workdir,omitempty" jsonschema:"working directory override"`

	// Mode is deliberately absent from the MCP schema. It exists only so the
	// Runner's authoritative validator can reject a forbidden field received
	// through a non-MCP or version-skewed bridge.
	Mode *string `json:"-"`
}

type TargetKind string

const (
	TargetAgent   TargetKind = "agent"
	TargetSession TargetKind = "session"
)

type Target struct {
	Kind TargetKind `json:"kind"`
	Name string     `json:"name"`
}

type Result struct {
	Target   Target `json:"target"`
	Response string `json:"response"`
}

// Error is a stable tool-facing failure. Details intentionally remain narrow
// so raw CLI session IDs, usage, and cost cannot leak through the MCP result.
type Error struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Target  *Target `json:"target,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Response struct {
	CallID string  `json:"call_id,omitempty"`
	Result *Result `json:"result,omitempty"`
	Error  *Error  `json:"error,omitempty"`
}

func (r Request) Target() Target {
	if r.Agent != nil {
		return Target{Kind: TargetAgent, Name: strings.TrimSpace(*r.Agent)}
	}
	if r.Session != nil {
		return Target{Kind: TargetSession, Name: strings.TrimSpace(*r.Session)}
	}
	return Target{}
}

func (r Request) Validate() *Error {
	if strings.TrimSpace(r.Prompt) == "" {
		return &Error{Code: CodeInvalidRequest, Message: "prompt is required"}
	}
	if (r.Agent == nil) == (r.Session == nil) {
		return &Error{Code: CodeInvalidTarget, Message: "exactly one of agent or session is required"}
	}
	target := r.Target()
	if target.Name == "" {
		return &Error{Code: CodeInvalidTarget, Message: fmt.Sprintf("%s target must not be empty", target.Kind), Target: &target}
	}
	if target.Kind == TargetSession {
		switch target.Name {
		case "new", "resume", "inherit":
			return &Error{Code: CodeInvalidSession, Message: fmt.Sprintf("session %q is reserved and cannot be called", target.Name), Target: &target}
		}
		if r.CLI != nil {
			return &Error{Code: CodeInvalidRequest, Message: "cli is not allowed with a named session target", Target: &target}
		}
	}
	if r.Mode != nil {
		return &Error{Code: CodeInvalidRequest, Message: "mode is not supported; called agents always run autonomous-headless", Target: &target}
	}
	for name, value := range map[string]*string{"cli": r.CLI, "model": r.Model, "workdir": r.Workdir} {
		if value != nil && strings.TrimSpace(*value) == "" {
			return &Error{Code: CodeInvalidRequest, Message: name + " must not be empty when provided", Target: &target}
		}
	}
	return nil
}

// DecodeRequest applies strict JSON decoding at the supervising Runner
// boundary, independent of validation already performed by the MCP SDK.
func DecodeRequest(raw json.RawMessage) (Request, *Error) {
	var request Request
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "invalid call_agent request: " + err.Error()}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "invalid call_agent request: multiple JSON values"}
	}
	if validation := request.Validate(); validation != nil {
		return Request{}, validation
	}
	return request, nil
}

// Tool returns the one canonical schema and description shared by every
// process-local adapter integration.
func Tool() *mcp.Tool {
	return &mcp.Tool{Name: ToolName, Description: toolDescription, InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "minLength": 1},
    "agent": {"type": "string", "minLength": 1},
    "session": {"type": "string", "minLength": 1},
    "cli": {"type": "string", "minLength": 1},
    "model": {"type": "string", "minLength": 1},
    "workdir": {"type": "string", "minLength": 1}
  },
  "required": ["prompt"],
	"oneOf": [
		{"required": ["agent"], "not": {"required": ["session"]}},
		{"required": ["session"], "not": {"anyOf": [{"required": ["agent"]}, {"required": ["cli"]}]}}
	],
  "additionalProperties": false
}`)}
}
