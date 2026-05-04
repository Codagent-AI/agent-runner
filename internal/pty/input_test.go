package pty

import (
	"testing"
)

func TestInputProcessor(t *testing.T) {
	t.Run("forwards regular text", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("hello"))
		if r.triggered {
			t.Fatal("unexpected trigger")
		}
		if string(r.forward) != "hello" {
			t.Fatalf("expected %q, got %q", "hello", string(r.forward))
		}
	})

	t.Run("detects /next followed by CR", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("/next\r"))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		// /next bytes are forwarded, Enter is not.
		if string(r.forward) != "/next" {
			t.Fatalf("expected %q forwarded, got %q", "/next", string(r.forward))
		}
	})

	t.Run("detects /next followed by LF", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("/next\n"))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
	})

	t.Run("detects /next across multiple chunks", func(t *testing.T) {
		p := &inputProcessor{}
		r1 := p.process([]byte("/nex"))
		if r1.triggered {
			t.Fatal("unexpected trigger on partial /next")
		}
		r2 := p.process([]byte("t\r"))
		if !r2.triggered {
			t.Fatal("expected trigger after Enter")
		}
	})

	t.Run("does not trigger on partial /next without Enter", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("/next"))
		if r.triggered {
			t.Fatal("unexpected trigger without Enter")
		}
		if string(r.forward) != "/next" {
			t.Fatalf("expected /next forwarded, got %q", string(r.forward))
		}
	})

	t.Run("does not trigger on non-matching text with Enter", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("hello\r"))
		if r.triggered {
			t.Fatal("unexpected trigger")
		}
	})

	t.Run("detects Ctrl-]", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte{0x1d})
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected no forwarded bytes, got %d", len(r.forward))
		}
	})

	t.Run("Ctrl-] forwards preceding bytes", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("abc\x1d"))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "abc" {
			t.Fatalf("expected %q forwarded, got %q", "abc", string(r.forward))
		}
	})

	t.Run("detects enhanced keyboard Ctrl-] CSI u", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x1b[93;5u"))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
	})

	t.Run("detects enhanced keyboard Ctrl-] xterm", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x1b[27;5;93~"))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
	})

	t.Run("passes through CSI sequences", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x1b[A"))
		if r.triggered {
			t.Fatal("unexpected trigger inside CSI")
		}
		if string(r.forward) != "\x1b[A" {
			t.Fatalf("expected CSI sequence forwarded, got %q", string(r.forward))
		}
	})

	t.Run("drops SGR mouse input sequences", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x1b[<0;31;58M\x1b[<0;31;58m"))
		if r.triggered {
			t.Fatal("unexpected trigger inside mouse input")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected mouse input dropped, got %q", string(r.forward))
		}
	})

	t.Run("drops split SGR mouse input sequence", func(t *testing.T) {
		p := &inputProcessor{}
		r1 := p.process([]byte("\x1b[<0;31"))
		if r1.triggered {
			t.Fatal("unexpected trigger inside partial mouse input")
		}
		if len(r1.forward) != 0 {
			t.Fatalf("expected partial mouse input buffered, got %q", string(r1.forward))
		}
		r2 := p.process([]byte(";58M"))
		if r2.triggered {
			t.Fatal("unexpected trigger inside mouse input")
		}
		if len(r2.forward) != 0 {
			t.Fatalf("expected split mouse input dropped, got %q", string(r2.forward))
		}
	})

	t.Run("does not trigger on Ctrl-] byte inside CSI", func(t *testing.T) {
		p := &inputProcessor{}
		// 0x1d is below the CSI final byte range (0x40-0x7e), so it stays
		// in CSI state and is forwarded without triggering.
		r := p.process([]byte{0x1b, '[', 0x1d, 'A'})
		if r.triggered {
			t.Fatal("unexpected trigger inside CSI sequence")
		}
	})

	t.Run("passes through OSC sequences", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x1b]0;title\x07"))
		if r.triggered {
			t.Fatal("unexpected trigger inside OSC")
		}
	})

	t.Run("backspace corrects line buffer", func(t *testing.T) {
		p := &inputProcessor{}
		// Type "/nextt", backspace, Enter → should match /next
		r := p.process([]byte("/nextt\x7f\r"))
		if !r.triggered {
			t.Fatal("expected trigger after backspace correction")
		}
	})

	t.Run("Ctrl-U clears line buffer", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("garbage\x15/next\r"))
		if !r.triggered {
			t.Fatal("expected trigger after Ctrl-U and /next")
		}
	})

	t.Run("Enter resets line buffer for next line", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("hello\r/next\r"))
		if !r.triggered {
			t.Fatal("expected trigger on second line /next")
		}
	})

	t.Run("simple two-byte escape passes through", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x1bOP"))
		if r.triggered {
			t.Fatal("unexpected trigger")
		}
		if string(r.forward) != "\x1bOP" {
			t.Fatalf("expected escape sequence forwarded, got %q", string(r.forward))
		}
	})

	t.Run("DCS sequence passes through", func(t *testing.T) {
		p := &inputProcessor{}
		// DCS q ... ST (\x1b\)
		r := p.process([]byte("\x1bPq\x1b\\"))
		if r.triggered {
			t.Fatal("unexpected trigger inside DCS")
		}
	})

	t.Run("buffers split ST inside string sequence", func(t *testing.T) {
		p := &inputProcessor{}
		r1 := p.process([]byte("\x1bPq\x1b"))
		if r1.triggered {
			t.Fatal("unexpected trigger inside DCS")
		}
		if string(r1.forward) != "\x1bPq" {
			t.Fatalf("expected DCS prefix without lone ESC, got %q", string(r1.forward))
		}

		r2 := p.process([]byte("\\"))
		if r2.triggered {
			t.Fatal("unexpected trigger inside split ST")
		}
		if string(r2.forward) != "\x1b\\" {
			t.Fatalf("expected split ST forwarded, got %q", string(r2.forward))
		}
	})

	t.Run("escape state persists across chunks", func(t *testing.T) {
		p := &inputProcessor{}
		// Send start of CSI in one chunk
		r1 := p.process([]byte{0x1b})
		if r1.triggered {
			t.Fatal("unexpected trigger")
		}
		// Continue CSI in next chunk — Ctrl-] byte should be consumed
		// as part of the CSI, not as a trigger.
		r2 := p.process([]byte{'[', 0x1d, 'A'})
		if r2.triggered {
			t.Fatal("unexpected trigger inside split CSI sequence")
		}
	})

	t.Run("delete key (0x08) corrects line buffer", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("/nextt\x08\r"))
		if !r.triggered {
			t.Fatal("expected trigger after delete correction")
		}
	})

	t.Run("backspace on empty line buffer is harmless", func(t *testing.T) {
		p := &inputProcessor{}
		r := p.process([]byte("\x7f\x7f/next\r"))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
	})
}
