package pty

import (
	"testing"
	"time"
)

func TestIdleHintReset(t *testing.T) {
	t.Run("arms timer when not suppressed", func(t *testing.T) {
		h := newIdleHint(time.Hour)
		h.reset()
		if h.timer == nil {
			t.Fatal("expected reset to arm timer")
		}
		h.cancel()
	})

	t.Run("does not suppress when clearing hidden hint", func(t *testing.T) {
		h := newIdleHint(time.Hour)
		h.clearIfShown()
		h.reset()
		if h.timer == nil {
			t.Fatal("expected reset after hidden clear to arm timer")
		}
		h.cancel()
	})

	t.Run("suppresses redraw after visible hint is cleared", func(t *testing.T) {
		h := newIdleHint(time.Hour)
		h.shown = true
		h.clearIfShown()
		h.reset()
		if h.timer != nil {
			t.Fatal("expected reset after visible clear to stay suppressed")
		}
	})

	t.Run("visible clear starts twenty second suppression window", func(t *testing.T) {
		h := newIdleHint(time.Hour)
		start := time.Now()
		h.shown = true
		h.clearIfShown()
		if h.suppressUntil.Before(start.Add(19 * time.Second)) {
			t.Fatalf("expected suppression near 20s, got %s", h.suppressUntil.Sub(start))
		}
		if h.suppressUntil.After(start.Add(21 * time.Second)) {
			t.Fatalf("expected suppression near 20s, got %s", h.suppressUntil.Sub(start))
		}
	})

	t.Run("arms timer after suppression expires", func(t *testing.T) {
		h := newIdleHint(time.Hour)
		h.suppressUntil = time.Now().Add(-time.Millisecond)
		h.reset()
		if h.timer == nil {
			t.Fatal("expected reset after suppression expires to arm timer")
		}
		h.cancel()
	})
}
