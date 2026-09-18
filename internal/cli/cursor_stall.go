package cli

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

// ErrCursorResultStall is returned when a Cursor headless stream emitted
// useful events, completed thinking with no in-flight tools, and then stayed
// silent without a terminal result event. The child is still running in that
// state, so waiting for process exit is unbounded.
var ErrCursorResultStall = errors.New("cursor: stream stalled after completed thinking without a terminal result")

// cursorResultStallGrace is how long a Cursor stream may stay idle after a
// completed thinking block with no in-flight tools and no result event.
const cursorResultStallGrace = 2 * time.Minute

type cursorResultStallWatch struct {
	grace   time.Duration
	onStall func()

	mu                sync.Mutex
	pendingTools      int
	sawUseful         bool
	sawResult         bool
	thinkingCompleted bool
	stopped           bool
	generation        uint64
	timer             *time.Timer
}

func newCursorResultStallWatch(grace time.Duration, onStall func()) *cursorResultStallWatch {
	if grace <= 0 {
		grace = cursorResultStallGrace
	}
	return &cursorResultStallWatch{grace: grace, onStall: onStall}
}

func (w *cursorResultStallWatch) observeLine(line []byte) {
	var event struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.sawResult {
		return
	}

	switch event.Type {
	case "result":
		w.sawResult = true
		w.thinkingCompleted = false
		w.disarmLocked()
		return
	case "tool_call":
		w.sawUseful = true
		switch event.Subtype {
		case "started":
			w.pendingTools++
			w.thinkingCompleted = false
			w.disarmLocked()
		case "completed":
			if w.pendingTools > 0 {
				w.pendingTools--
			}
			if w.pendingTools == 0 && w.thinkingCompleted {
				w.armLocked()
			} else {
				w.disarmLocked()
			}
		}
		return
	case "thinking", "assistant":
		w.sawUseful = true
		if event.Type == "thinking" && event.Subtype == "completed" {
			w.thinkingCompleted = true
			if w.pendingTools == 0 {
				w.armLocked()
				return
			}
		} else {
			w.thinkingCompleted = false
		}
		w.disarmLocked()
	}
}

func (w *cursorResultStallWatch) armLocked() {
	w.disarmLocked()
	w.generation++
	gen := w.generation
	w.timer = time.AfterFunc(w.grace, func() { w.fireGeneration(gen) })
}

func (w *cursorResultStallWatch) disarmLocked() {
	w.generation++
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *cursorResultStallWatch) fire() {
	w.mu.Lock()
	gen := w.generation
	w.mu.Unlock()
	w.fireGeneration(gen)
}

func (w *cursorResultStallWatch) fireGeneration(gen uint64) {
	w.mu.Lock()
	if w.stopped || w.sawResult || w.pendingTools > 0 || !w.thinkingCompleted || w.generation != gen {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.timer = nil
	onStall := w.onStall
	w.mu.Unlock()
	if onStall != nil {
		onStall()
	}
}

func (w *cursorResultStallWatch) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	w.disarmLocked()
}

type cursorStallSupervisedWriter struct {
	lineBufferedWriter
	watch *cursorResultStallWatch
}

func watchCursorHeadlessStream(downstream io.Writer, grace time.Duration, onStall func()) io.Writer {
	watch := newCursorResultStallWatch(grace, onStall)
	writer := &cursorStallSupervisedWriter{watch: watch}
	writer.downstream = downstream
	writer.onLine = writer.processLine
	return writer
}

func (w *cursorStallSupervisedWriter) processLine(line []byte) error {
	w.watch.observeLine(line)
	if len(line) == 0 {
		return w.writeDownstream(nil)
	}
	out := append(append([]byte{}, line...), '\n')
	return w.writeDownstream(out)
}

func (w *cursorStallSupervisedWriter) Close() error {
	w.watch.stop()
	return w.lineBufferedWriter.Close()
}
