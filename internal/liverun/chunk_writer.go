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

	// (Re)start the idle timer.
	if w.timer != nil {
		w.timer.Reset(chunkIdleFlush)
	} else {
		w.timer = time.AfterFunc(chunkIdleFlush, func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.flushLocked()
		})
	}

	return len(p), nil
}

// Flush sends any remaining buffered bytes and stops the idle timer. Safe to
// call after the subprocess exits to drain the last partial chunk.
func (w *chunkWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

func (w *chunkWriter) flushLocked() {
	if len(w.buf) == 0 {
		return
	}
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
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
