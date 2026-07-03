package pty

import "testing"

func TestMouseTracker(t *testing.T) {
	t.Run("nil tracker reports disabled", func(t *testing.T) {
		var tr *mouseTracker
		if tr.enabled() {
			t.Fatal("expected nil tracker to report disabled")
		}
		tr.observeCSI([]byte("\x1b[?1000h")) // must not panic
	})

	t.Run("zero value reports disabled", func(t *testing.T) {
		tr := &mouseTracker{}
		if tr.enabled() {
			t.Fatal("expected zero-value tracker to report disabled")
		}
	})

	t.Run("DECSET of a mouse reporting mode enables tracking", func(t *testing.T) {
		for _, seq := range []string{"\x1b[?9h", "\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h"} {
			tr := &mouseTracker{}
			tr.observeCSI([]byte(seq))
			if !tr.enabled() {
				t.Fatalf("expected %q to enable mouse tracking", seq)
			}
		}
	})

	t.Run("DECRST of the enabled mode disables tracking", func(t *testing.T) {
		tr := &mouseTracker{}
		tr.observeCSI([]byte("\x1b[?1002h"))
		tr.observeCSI([]byte("\x1b[?1002l"))
		if tr.enabled() {
			t.Fatal("expected mouse tracking disabled after DECRST")
		}
	})

	t.Run("tracking stays enabled while any mode remains set", func(t *testing.T) {
		tr := &mouseTracker{}
		tr.observeCSI([]byte("\x1b[?1000h"))
		tr.observeCSI([]byte("\x1b[?1002h"))
		tr.observeCSI([]byte("\x1b[?1000l"))
		if !tr.enabled() {
			t.Fatal("expected mouse tracking still enabled while 1002 is set")
		}
	})

	t.Run("multi-parameter DECSET enables tracking", func(t *testing.T) {
		tr := &mouseTracker{}
		tr.observeCSI([]byte("\x1b[?1002;1006h"))
		if !tr.enabled() {
			t.Fatal("expected multi-parameter DECSET to enable mouse tracking")
		}
	})

	t.Run("SGR encoding mode alone does not enable tracking", func(t *testing.T) {
		tr := &mouseTracker{}
		tr.observeCSI([]byte("\x1b[?1006h"))
		if tr.enabled() {
			t.Fatal("expected encoding-only mode 1006 not to enable tracking")
		}
	})

	t.Run("unrelated DECSET does not enable tracking", func(t *testing.T) {
		tr := &mouseTracker{}
		tr.observeCSI([]byte("\x1b[?1049h"))
		tr.observeCSI([]byte("\x1b[2J"))
		if tr.enabled() {
			t.Fatal("expected unrelated sequences not to enable tracking")
		}
	})
}
