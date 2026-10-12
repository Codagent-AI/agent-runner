package exec

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/control"

	"github.com/codagent/agent-runner/internal/cli"
)

func TestActivityOutputEquivalenceINT005(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("../cli/testdata/activity/" + name + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			a, _ := cli.Get(name)
			tracker := newActivityTracker(a, time.Now)
			var baseline, observed, capture1, capture2 bytes.Buffer
			before := childStdoutCapture(a, &capture1)(&baseline)
			after := childStdoutCapture(a, io.MultiWriter(&capture2, tracker))(&observed)
			for _, b := range data {
				_, _ = before.Write([]byte{b})
				_, _ = after.Write([]byte{b})
			}
			if !bytes.Equal(baseline.Bytes(), observed.Bytes()) || !bytes.Equal(capture1.Bytes(), capture2.Bytes()) || !bytes.Equal(data, capture2.Bytes()) {
				t.Fatal("activity altered output")
			}
		})
	}
}
func TestActivityLimitsAndStderr(t *testing.T) {
	now := time.Now()
	a := newActivityTracker(&cli.ClaudeAdapter{}, func() time.Time { return now })
	if a.Describe(now) != "no output yet" {
		t.Fatal(a.Describe(now))
	}
	var raw bytes.Buffer
	w := a.stderrWrapper(&cli.ClaudeAdapter{})(&raw)
	_, _ = w.Write([]byte("STDERR-SECRET"))
	if raw.String() != "STDERR-SECRET" || !strings.Contains(a.Describe(now), "last output 0s ago") {
		t.Fatal(a.Describe(now))
	}
	_, _ = a.Write([]byte(strings.Repeat("x", activityLineLimit+1) + "\n"))
	_, _ = a.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"SECRET"}}]}}` + "\n"))
	summary := a.Describe(now)
	_, _ = a.Write([]byte("bad json\n"))
	if summary != a.Describe(now) || !strings.Contains(summary, "tool_use: Bash") || strings.Contains(summary, "SECRET") {
		t.Fatal(a.Describe(now))
	}
	a.mu.Lock()
	a.summary = strings.Repeat("界", 220) + "\nSECRET"
	a.mu.Unlock()
	if len([]rune(a.Describe(now))) > 200 || strings.ContainsAny(a.Describe(now), "\r\n") {
		t.Fatal("summary bound")
	}
}

func TestActivitySnapshotsAreImmediateAndStrict(t *testing.T) {
	runner := &callTestRunner{release: make(chan struct{}), started: make(chan AgentProcessOptions, 1), result: ProcessResult{Started: true}}
	o := pollingAgentCallOptions(t.TempDir(), runner, &callTestAdapter{})
	h := NewAgentCallHandler(o)
	start := startAgentCall(t, h, control.AgentCallRequest{RequestID: "start", Payload: []byte(`{"agent":"implementor","prompt":"x"}`)})
	<-runner.started
	for _, payload := range []string{`{"call_id":"` + start.CallID + `"}`, `{"start_request_id":"start"}`} {
		before := time.Now()
		r := decodeCallResponse(t, h.HandleAgentCallActivity(context.Background(), control.AgentCallRequest{Payload: []byte(payload)}))
		if time.Since(before) > 100*time.Millisecond || r.Status != agentcall.StatusRunning || r.Activity != "no output yet" || r.Details != nil {
			t.Fatalf("snapshot: %+v", r)
		}
	}
	for _, payload := range []string{`{}`, `{"call_id":"x","start_request_id":"s"}`, `{"call_id":"x"} {}`, `{"unknown":true}`} {
		r := decodeCallResponse(t, h.HandleAgentCallActivity(context.Background(), control.AgentCallRequest{Payload: []byte(payload)}))
		if r.Error == nil || r.Error.Code != agentcall.CodeInvalidRequest {
			t.Fatalf("%s: %+v", payload, r)
		}
	}
	close(runner.release)
	terminal := awaitAgentCall(t, h, start.CallID)
	r := decodeCallResponse(t, h.HandleAgentCallActivity(context.Background(), control.AgentCallRequest{Payload: []byte(`{"call_id":"` + start.CallID + `"}`)}))
	if r.Status != terminal.Status || r.Activity != "" || r.Details == nil {
		t.Fatalf("terminal: %+v", r)
	}
}

func TestActivityTeeFlushesAdapterWrapper(t *testing.T) {
	a := newActivityTracker(&cli.CodexAdapter{}, time.Now)
	var plain, withTee bytes.Buffer
	adapter := &cli.CodexAdapter{}
	before := adapter.WrapStderr(&plain)
	after := a.stderrWrapper(adapter)(&withTee)
	_, _ = before.Write([]byte("partial without newline"))
	_, _ = after.Write([]byte("partial without newline"))
	if c, ok := before.(io.Closer); ok {
		_ = c.Close()
	}
	if c, ok := after.(io.Closer); ok {
		_ = c.Close()
	}
	if plain.String() != withTee.String() {
		t.Fatalf("tee dropped trailing stderr: %q %q", plain.String(), withTee.String())
	}
}
