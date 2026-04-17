package liverun

import (
	"sync"
	"time"
)

const (
	chunkMaxBytes  = 4 * 1024        // flush when buffer hits 4 KB
	chunkIdleFlush = 50 * time.Millisecond
)

// chunkWriter is an io.Writer that coalesces subprocess output into ~4 KB
// batches (or flushes on a 50 ms idle timer) and delivers them as
// OutputChunkMsg via the coordinator. ANSI sequences have already been
// stripped upstream; chunkWriter forwards clean text to the TUI.
type chunkWriter struct {
	coord      *Coordinator
	stepPrefix string
	stream     string // "stdout" or "stderr"

	mu    sync.Mutex
	buf   []byte
	timer *time.Timer
}

func newChunkWriter(coord *Coordinator, stepPrefix, stream string) *chunkWriter {
	return &chunkWriter{
		coord:      coord,
		stepPrefix: stepPrefix,
		stream:     stream,
	}
}

// Write buffers p and flushes when the buffer exceeds chunkMaxBytes or after
// chunkIdleFlush has elapsed since the last write. Always returns len(p), nil.
func (w *chunkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)

	if len(w.buf) >= chunkMaxBytes {
		w.flushLocked()
		return len(p), nil
	}

	// Cancel any pending timer and schedule a fresh one. Stop() may return
	// false if the old timer already fired — its callback will race to
	// acquire w.mu, find either our appended buffer (and flush it, harmless)
	// or an empty buffer (and no-op). Creating a new timer each call avoids
	// the time.Timer.Reset race in AfterFunc timers.
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(chunkIdleFlush, w.onIdle)

	return len(p), nil
}

// onIdle is the AfterFunc callback for the idle timer.
func (w *chunkWriter) onIdle() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

// Flush sends any remaining buffered bytes and stops the idle timer. Safe to
// call after the subprocess exits to drain the last partial chunk.
func (w *chunkWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.flushLocked()
}

func (w *chunkWriter) flushLocked() {
	if len(w.buf) == 0 {
		return
	}
	data := make([]byte, len(w.buf))
	copy(data, w.buf)
	w.buf = w.buf[:0]
	w.coord.send(OutputChunkMsg{
		StepPrefix: w.stepPrefix,
		Stream:     w.stream,
		Bytes:      data,
	})
}
