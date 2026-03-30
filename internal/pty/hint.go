package pty

import (
	"fmt"
	"os"
	"sync"
	"time"

	gopty "github.com/creack/pty"
)

// idleHint renders a continue-trigger hint bar at the bottom of the terminal
// after a period of PTY silence.
type idleHint struct {
	delay time.Duration
	timer *time.Timer
	shown bool
	mu    sync.Mutex
}

func newIdleHint(delay time.Duration) *idleHint {
	return &idleHint{delay: delay}
}

func (h *idleHint) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(h.delay, h.draw)
}

func (h *idleHint) cancel() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
}

func (h *idleHint) clearIfShown() {
	h.mu.Lock()
	wasShown := h.shown
	h.shown = false
	h.mu.Unlock()

	if wasShown {
		size, err := gopty.GetsizeFull(os.Stdin)
		if err != nil {
			return
		}
		// Save cursor, move to bottom row, clear line, restore cursor.
		_, _ = fmt.Fprintf(os.Stdout, "\x1b7\x1b[%d;1H\x1b[K\x1b8", size.Rows)
	}
}

func (h *idleHint) draw() {
	h.mu.Lock()
	defer h.mu.Unlock()

	size, err := gopty.GetsizeFull(os.Stdin)
	if err != nil {
		return
	}

	hint := " /next or Ctrl-] to continue to next step"
	cols := int(size.Cols)
	if len(hint) > cols {
		hint = hint[:cols]
	}

	// Save cursor, move to bottom row, dim+reverse bar, restore cursor.
	_, _ = fmt.Fprintf(os.Stdout, "\x1b7\x1b[%d;1H\x1b[2;7m%-*s\x1b[0m\x1b8", size.Rows, cols, hint)
	h.shown = true
}
