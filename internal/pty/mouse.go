package pty

import (
	"bytes"
	"sync"
)

// mouseTracker records whether the child process has enabled terminal mouse
// tracking (DECSET 9, 1000, 1002, or 1003). The output goroutine updates it
// from the child's output stream; the input goroutine consults it to decide
// whether SGR mouse input should be forwarded to the child or dropped.
//
// Mode 1006 (SGR extended encoding) only changes how events are encoded, so
// it does not count as enabling tracking on its own.
type mouseTracker struct {
	mu    sync.Mutex
	modes map[string]bool
}

// enabled reports whether any mouse reporting mode is currently set. Safe on
// a nil receiver, which reports disabled.
func (t *mouseTracker) enabled() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.modes) > 0
}

// observeCSI inspects a complete CSI sequence from the child's output and
// updates tracking state when it is a DECSET/DECRST of a mouse reporting
// mode. Non-matching sequences are ignored. Safe on a nil receiver.
func (t *mouseTracker) observeCSI(seq []byte) {
	if t == nil || len(seq) < 4 {
		return
	}
	if seq[0] != 0x1b || seq[1] != '[' || seq[2] != '?' {
		return
	}
	final := seq[len(seq)-1]
	if final != 'h' && final != 'l' {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for param := range bytes.SplitSeq(seq[3:len(seq)-1], []byte(";")) {
		switch string(param) {
		case "9", "1000", "1002", "1003":
			if final == 'h' {
				if t.modes == nil {
					t.modes = make(map[string]bool)
				}
				t.modes[string(param)] = true
			} else {
				delete(t.modes, string(param))
			}
		}
	}
}
