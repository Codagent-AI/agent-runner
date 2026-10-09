// Package agentcall defines the Runner-owned call_agent contract and runtime.
package agentcall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolName       = "call_agent"
	GetToolName    = "get_agent_call"
	CancelToolName = "cancel_agent_call"

	StatusAccepted  = "accepted"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"

	CodeInvalidRequest  = "invalid_request"
	CodeInvalidTarget   = "invalid_target"
	CodeInvalidSession  = "invalid_session"
	CodeIneligible      = "call_agent_unavailable"
	CodeUnknownAgent    = "unknown_agent"
	CodeUnknownSession  = "unknown_session"
	CodeUnknownCall     = "unknown_call"
	CodeInvalidCLI      = "invalid_cli"
	CodeInvalidModel    = "invalid_model"
	CodeInvalidWorkdir  = "invalid_workdir"
	CodeSelfSession     = "self_session"
	CodeCallInProgress  = "call_in_progress"
	CodeExecutionFailed = "execution_failed"
	CodeCallCanceled    = "call_canceled"
	CodeControlFailure  = "control_failure"
	CodeResultTooLarge  = "result_too_large"
	CodeTimedOut        = "timed_out"
	CodeNotResumable    = "not_resumable"
)

const (
	toolDescription = "Start an agent profile, named session, or follow_up of an earlier call_id. Optional timeout bounds duration (1s–24h). Terminal responses include details; details.git observes HEAD movement and is not authorship. Returns the " +
		"child's terminal result when the child finishes first; otherwise returns a call_id with a " +
		"non-terminal status, and you must then poll get_agent_call with that call_id until the call is " +
		"terminal. A non-terminal status is never the child's answer. Calls are serial: finish or cancel " +
		"the active call before starting another. " +
		"The child receives the profile system prompt and supplied prompt without workflow-step enrichment."
	getToolDescription = "Return the terminal result for a call_id from this parent attempt, waiting for the child " +
		"where the host allows it and otherwise returning the current non-terminal status. Repeat until the " +
		"status is terminal. Running snapshots include activity. Does not start another call."
	cancelToolDescription = "Terminate a running child for call_id and return terminal details. A finished call returns its cached terminal result. " +
		"Does not start another call."
	callIDSchema = `{
  "type": "object",
  "properties": {
    "call_id": {"type": "string", "minLength": 1}
  },
  "required": ["call_id"],
  "additionalProperties": false
}`
)

// Request is the canonical call_agent input. Pointer fields distinguish an
// omitted optional field from an explicitly empty value during validation.
type Request struct {
	FollowUp *string `json:"follow_up,omitempty"`
	Timeout  *string `json:"timeout,omitempty"`
	Prompt   string  `json:"prompt" jsonschema:"the task prompt for the called agent"`
	Agent    *string `json:"agent,omitempty" jsonschema:"agent profile name for a fresh session"`
	Session  *string `json:"session,omitempty" jsonschema:"workflow-declared named session"`
	CLI      *string `json:"cli,omitempty" jsonschema:"CLI override for an agent target"`
	Model    *string `json:"model,omitempty" jsonschema:"model override"`
	Workdir  *string `json:"workdir,omitempty" jsonschema:"working directory override"`

	// Mode is deliberately absent from the MCP schema. It exists only so the
	// Runner's authoritative validator can reject a forbidden field received
	// through a non-MCP or version-skewed bridge.
	Mode *string `json:"-"`
}

type TargetKind string

const (
	TargetAgent    TargetKind = "agent"
	TargetSession  TargetKind = "session"
	TargetFollowUp TargetKind = "follow_up"
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
	CallID  string  `json:"call_id,omitempty"`
	Elapsed string  `json:"elapsed,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Details struct {
	Exit     string         `json:"exit"`
	ExitCode *int           `json:"exit_code,omitempty"`
	Duration string         `json:"duration"`
	Session  SessionDetails `json:"session"`
	Git      GitDelta       `json:"git"`
}
type SessionDetails struct {
	CLI      string `json:"cli"`
	Model    string `json:"model,omitempty"`
	Resumed  bool   `json:"resumed"`
	FollowUp bool   `json:"follow_up"`
}
type GitDelta struct {
	State     string      `json:"state"`
	StartHead string      `json:"start_head,omitempty"`
	EndHead   string      `json:"end_head,omitempty"`
	Commits   []GitCommit `json:"commits,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}
type GitCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}
type Response struct {
	Activity string   `json:"activity,omitempty"`
	Details  *Details `json:"details,omitempty"`
	CallID   string   `json:"call_id,omitempty"`
	Status   string   `json:"status,omitempty"`
	Target   *Target  `json:"target,omitempty"`
	Elapsed  string   `json:"elapsed,omitempty"`
	Result   *Result  `json:"result,omitempty"`
	Error    *Error   `json:"error,omitempty"`
}

// CallIDRequest is the canonical input for get_agent_call and cancel_agent_call.
type CallIDRequest struct {
	CallID string `json:"call_id"`
}

func (r CallIDRequest) Validate() *Error {
	if strings.TrimSpace(r.CallID) == "" {
		return &Error{Code: CodeInvalidRequest, Message: "call_id is required"}
	}
	return nil
}

func IsTerminalStatus(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

func (r *Request) Target() Target {
	if r.Agent != nil {
		return Target{Kind: TargetAgent, Name: strings.TrimSpace(*r.Agent)}
	}
	if r.Session != nil {
		return Target{Kind: TargetSession, Name: strings.TrimSpace(*r.Session)}
	}
	if r.FollowUp != nil {
		return Target{Kind: TargetFollowUp, Name: strings.TrimSpace(*r.FollowUp)}
	}
	return Target{}
}

func (r *Request) Validate() *Error {
	if strings.TrimSpace(r.Prompt) == "" {
		return &Error{Code: CodeInvalidRequest, Message: "prompt is required"}
	}
	targets := 0
	for _, v := range []*string{r.Agent, r.Session, r.FollowUp} {
		if v != nil {
			targets++
		}
	}
	if targets != 1 {
		return &Error{Code: CodeInvalidTarget, Message: "exactly one of agent, session, or follow_up is required"}
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
	if r.FollowUp != nil && r.CLI != nil {
		return &Error{Code: CodeInvalidRequest, Message: "cli is not allowed with follow_up", Target: &target}
	}
	if r.Timeout != nil {
		d, err := time.ParseDuration(*r.Timeout)
		if err != nil || d < time.Second || d > 24*time.Hour {
			return &Error{Code: CodeInvalidRequest, Message: "timeout must be a duration from 1s to 24h", Target: &target}
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

func (r *Request) TimeoutDuration() time.Duration {
	if r.Timeout == nil {
		return 0
	}
	d, _ := time.ParseDuration(*r.Timeout)
	return d
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

// DecodeCallIDRequest applies strict JSON decoding for poll and cancel tools.
func DecodeCallIDRequest(raw json.RawMessage) (CallIDRequest, *Error) {
	var request CallIDRequest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return CallIDRequest{}, &Error{Code: CodeInvalidRequest, Message: "invalid call_id request: " + err.Error()}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return CallIDRequest{}, &Error{Code: CodeInvalidRequest, Message: "invalid call_id request: multiple JSON values"}
	}
	if validation := request.Validate(); validation != nil {
		return CallIDRequest{}, validation
	}
	return request, nil
}

func Tools() []*mcp.Tool {
	return []*mcp.Tool{Tool(), GetTool(), CancelTool()}
}

func GetTool() *mcp.Tool {
	return &mcp.Tool{Name: GetToolName, Description: getToolDescription, InputSchema: json.RawMessage(callIDSchema)}
}

func CancelTool() *mcp.Tool {
	return &mcp.Tool{Name: CancelToolName, Description: cancelToolDescription, InputSchema: json.RawMessage(callIDSchema)}
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
    "follow_up": {"type": "string", "minLength": 1},
    "timeout": {"type": "string", "minLength": 1},
    "cli": {"type": "string", "minLength": 1},
    "model": {"type": "string", "minLength": 1},
    "workdir": {"type": "string", "minLength": 1}
  },
  "required": ["prompt"],
	"oneOf": [
		{"required": ["agent"], "not": {"anyOf": [{"required": ["session"]}, {"required": ["follow_up"]}]}},
		{"required": ["session"], "not": {"anyOf": [{"required": ["agent"]}, {"required": ["cli"]}, {"required": ["follow_up"]}]}},
		{"required": ["follow_up"], "not": {"anyOf": [{"required": ["agent"]}, {"required": ["session"]}, {"required": ["cli"]}]}}
	],
  "additionalProperties": false
}`)}
}
