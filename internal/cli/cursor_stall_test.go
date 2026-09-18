package cli

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestCursorResultStallWatch(t *testing.T) {
	t.Parallel()

	t.Run("completed thinking without result or pending tools stalls", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(8*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"thinking","subtype":"delta","text":"done"}`))
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		select {
		case <-stalled:
		case <-time.After(150 * time.Millisecond):
			t.Fatal("expected stall after completed thinking with no result")
		}
	})

	t.Run("result event prevents stall", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(8*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		watch.observeLine([]byte(`{"type":"result","result":"ok"}`))
		select {
		case <-stalled:
			t.Fatal("result event should prevent stall")
		case <-time.After(40 * time.Millisecond):
		}
	})

	t.Run("thinking completed during in-flight tool stalls after the tool completes", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(8*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"tool_call","subtype":"started"}`))
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		watch.observeLine([]byte(`{"type":"tool_call","subtype":"completed"}`))
		select {
		case <-stalled:
		case <-time.After(150 * time.Millisecond):
			t.Fatal("expected stall after the last in-flight tool completed with no result")
		}
	})

	t.Run("tool completion after later activity does not stall", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(8*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		watch.observeLine([]byte(`{"type":"tool_call","subtype":"started"}`))
		watch.observeLine([]byte(`{"type":"tool_call","subtype":"completed"}`))
		select {
		case <-stalled:
			t.Fatal("a long pause after a tool should not stall unless thinking already completed during that tool")
		case <-time.After(40 * time.Millisecond):
		}
	})

	t.Run("in-flight tool call does not stall", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(8*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"tool_call","subtype":"started"}`))
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		select {
		case <-stalled:
			t.Fatal("in-flight tool should keep the invocation alive")
		case <-time.After(40 * time.Millisecond):
		}
	})

	t.Run("late timer callback after later activity does not stall", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(time.Hour, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		watch.observeLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`))
		watch.fire()
		select {
		case <-stalled:
			t.Fatal("a timer that expired after later activity should not stall")
		case <-time.After(20 * time.Millisecond):
		}
	})

	t.Run("new stream activity after thinking resets grace", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(25*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		time.Sleep(10 * time.Millisecond)
		watch.observeLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`))
		select {
		case <-stalled:
			t.Fatal("later stream activity should reset the stall timer")
		case <-time.After(20 * time.Millisecond):
		}
		watch.observeLine([]byte(`{"type":"thinking","subtype":"completed"}`))
		select {
		case <-stalled:
		case <-time.After(150 * time.Millisecond):
			t.Fatal("expected stall after a later completed thinking block")
		}
	})

	t.Run("supervised writer forwards raw lines and close stops the timer", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		var downstream bytes.Buffer
		writer := watchCursorHeadlessStream(&downstream, 8*time.Millisecond, func() { stalled <- struct{}{} })
		line := `{"type":"thinking","subtype":"completed"}` + "\n"
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if downstream.String() != line {
			t.Fatalf("downstream = %q, want forwarded JSONL", downstream.String())
		}
		if err := writer.(io.Closer).Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		select {
		case <-stalled:
			t.Fatal("Close should stop the stall timer")
		case <-time.After(40 * time.Millisecond):
		}
	})

	t.Run("init-only stream does not stall", func(t *testing.T) {
		t.Parallel()
		stalled := make(chan struct{}, 1)
		watch := newCursorResultStallWatch(8*time.Millisecond, func() { stalled <- struct{}{} })
		defer watch.stop()
		watch.observeLine([]byte(`{"type":"system","subtype":"init","session_id":"abc"}`))
		select {
		case <-stalled:
			t.Fatal("init-only stream should not be treated as a completed turn")
		case <-time.After(40 * time.Millisecond):
		}
	})
}
