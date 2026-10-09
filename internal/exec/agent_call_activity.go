package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/control"
)

const activityLineLimit = 1024 * 1024

type activityTracker struct {
	mu         sync.Mutex
	now        func() time.Time
	summarizer cli.HeadlessActivitySummarizer
	line       []byte
	discarding bool
	lastOutput time.Time
	summary    string
	at         time.Time
}

func newActivityTracker(a cli.Adapter, now func() time.Time) *activityTracker {
	s, _ := a.(cli.HeadlessActivitySummarizer)
	return &activityTracker{now: now, summarizer: s}
}
func (a *activityTracker) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastOutput = a.now()
	n := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		part := p
		if idx >= 0 {
			part = p[:idx]
		}
		if !a.discarding {
			if len(a.line)+len(part) > activityLineLimit {
				a.line = nil
				a.discarding = true
			} else {
				a.line = append(a.line, part...)
			}
		}
		if idx < 0 {
			break
		}
		if !a.discarding && a.summarizer != nil {
			if summary, ok := a.summarizer.SummarizeActivity(a.line); ok {
				a.summary = summary
				a.at = a.now()
			}
		}
		a.line = a.line[:0]
		a.discarding = false
		p = p[idx+1:]
	}
	return n, nil
}

type activityStderr struct{ a *activityTracker }

func (w activityStderr) Write(p []byte) (int, error) {
	w.a.mu.Lock()
	w.a.lastOutput = w.a.now()
	w.a.mu.Unlock()
	return len(p), nil
}
func (a *activityTracker) stderrWrapper(adapter cli.Adapter) func(io.Writer) io.Writer {
	return func(w io.Writer) io.Writer {
		if wrapper, ok := adapter.(cli.StderrWrapper); ok {
			w = wrapper.WrapStderr(w)
		}
		return agentCallTeeWriter{Writer: io.MultiWriter(w, activityStderr{a}), downstream: w}
	}
}
func (a *activityTracker) Describe(now time.Time) string {
	if a == nil {
		return "no output yet"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	summary := "no output yet"
	age := func(at time.Time) string { return max(now.Sub(at).Round(time.Second), 0).String() }
	if a.summary != "" {
		summary = a.summary + " (" + age(a.at) + " ago)"
	} else if !a.lastOutput.IsZero() {
		summary = "no recognized activity; last output " + age(a.lastOutput) + " ago"
	}
	r := []rune(strings.NewReplacer("\n", " ", "\r", " ").Replace(summary))
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}
func (h *AgentCallHandler) HandleAgentCallActivity(_ context.Context, envelope control.AgentCallRequest) json.RawMessage {
	var request struct {
		CallID         string `json:"call_id"`
		StartRequestID string `json:"start_request_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&request)
	var trailing any
	if err := decoder.Decode(&trailing); decodeErr != nil || err != io.EOF || (strings.TrimSpace(request.CallID) == "") == (strings.TrimSpace(request.StartRequestID) == "") {
		return marshalAgentCallResponse(agentcall.Response{Error: &agentcall.Error{Code: agentcall.CodeInvalidRequest, Message: "exactly one of call_id or start_request_id is required"}})
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.byCallID[request.CallID]
	if request.StartRequestID != "" {
		r = h.accepted[request.StartRequestID]
	}
	if r == nil {
		return marshalAgentCallResponse(agentcall.Response{Error: &agentcall.Error{Code: agentcall.CodeUnknownCall, Message: "unknown call in this parent attempt"}})
	}
	return h.marshalSnapshotLocked(r)
}

// Preserve adapter flush behavior when composing a raw-stream tee.
type agentCallTeeWriter struct {
	io.Writer
	downstream io.Writer
}

func (w agentCallTeeWriter) Close() error {
	if closer, ok := w.downstream.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
