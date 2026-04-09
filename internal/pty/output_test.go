package pty

import "testing"

const sentinel = "\x1b]999;red-slippers\x07"

func TestOutputProcessor(t *testing.T) {
	t.Run("forwards regular text", func(t *testing.T) {
		p := &outputProcessor{}
		r := p.process([]byte("hello world"))
		if r.triggered {
			t.Fatal("unexpected trigger")
		}
		if string(r.forward) != "hello world" {
			t.Fatalf("expected %q, got %q", "hello world", string(r.forward))
		}
	})

	t.Run("detects sentinel and strips it", func(t *testing.T) {
		p := &outputProcessor{}
		r := p.process([]byte(sentinel))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected no forwarded bytes, got %q", string(r.forward))
		}
	})

	t.Run("sentinel embedded in other output", func(t *testing.T) {
		p := &outputProcessor{}
		input := "before" + sentinel + "after"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		// Bytes before AND after the sentinel are forwarded.
		if string(r.forward) != "beforeafter" {
			t.Fatalf("expected %q, got %q", "beforeafter", string(r.forward))
		}
	})

	t.Run("sentinel detection across chunk boundaries", func(t *testing.T) {
		p := &outputProcessor{}
		// Split the sentinel in the middle of the payload.
		half := len(sentinel) / 2
		r1 := p.process([]byte(sentinel[:half]))
		if r1.triggered {
			t.Fatal("unexpected trigger on first half")
		}
		if len(r1.forward) != 0 {
			t.Fatalf("expected no forwarded bytes on first half, got %q", string(r1.forward))
		}
		r2 := p.process([]byte(sentinel[half:]))
		if !r2.triggered {
			t.Fatal("expected trigger after second half")
		}
		if len(r2.forward) != 0 {
			t.Fatalf("expected no forwarded bytes on second half, got %q", string(r2.forward))
		}
	})

	t.Run("sentinel split byte by byte across chunks", func(t *testing.T) {
		p := &outputProcessor{}
		for i := 0; i < len(sentinel)-1; i++ {
			r := p.process([]byte{sentinel[i]})
			if r.triggered {
				t.Fatalf("unexpected trigger at byte %d", i)
			}
		}
		r := p.process([]byte{sentinel[len(sentinel)-1]})
		if !r.triggered {
			t.Fatal("expected trigger after final byte")
		}
	})

	t.Run("incomplete sentinel at process exit is flushed", func(t *testing.T) {
		p := &outputProcessor{}
		// Write the start of the sentinel but don't complete it.
		partial := sentinel[:10]
		r := p.process([]byte(partial))
		if r.triggered {
			t.Fatal("unexpected trigger on partial sentinel")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected no forwarded bytes (buffered), got %q", string(r.forward))
		}
		// flush() returns the buffered partial bytes.
		flushed := p.flush()
		if string(flushed) != partial {
			t.Fatalf("expected flushed %q, got %q", partial, string(flushed))
		}
	})

	t.Run("non-matching OSC sequence passed through", func(t *testing.T) {
		p := &outputProcessor{}
		osc := "\x1b]0;window title\x07"
		r := p.process([]byte(osc))
		if r.triggered {
			t.Fatal("unexpected trigger on non-matching OSC")
		}
		if string(r.forward) != osc {
			t.Fatalf("expected %q forwarded, got %q", osc, string(r.forward))
		}
	})

	t.Run("non-matching OSC sequence surrounded by text passed through", func(t *testing.T) {
		p := &outputProcessor{}
		osc := "\x1b]0;title\x07"
		input := "pre" + osc + "post"
		r := p.process([]byte(input))
		if r.triggered {
			t.Fatal("unexpected trigger")
		}
		if string(r.forward) != input {
			t.Fatalf("expected %q forwarded, got %q", input, string(r.forward))
		}
	})

	t.Run("CSI sequence passed through", func(t *testing.T) {
		p := &outputProcessor{}
		csi := "\x1b[32mgreen\x1b[0m"
		r := p.process([]byte(csi))
		if r.triggered {
			t.Fatal("unexpected trigger inside CSI")
		}
		if string(r.forward) != csi {
			t.Fatalf("expected %q forwarded, got %q", csi, string(r.forward))
		}
	})

	t.Run("escape state persists across chunks", func(t *testing.T) {
		p := &outputProcessor{}
		// Send just the ESC byte in one chunk.
		r1 := p.process([]byte{0x1b})
		if r1.triggered {
			t.Fatal("unexpected trigger on bare ESC")
		}
		if len(r1.forward) != 0 {
			t.Fatalf("expected ESC buffered, got %q", string(r1.forward))
		}
		// Complete a CSI sequence in the next chunk.
		r2 := p.process([]byte("[2J"))
		if r2.triggered {
			t.Fatal("unexpected trigger inside split CSI")
		}
		if string(r2.forward) != "\x1b[2J" {
			t.Fatalf("expected %q, got %q", "\x1b[2J", string(r2.forward))
		}
	})

	t.Run("flush on empty buffer returns nil", func(t *testing.T) {
		p := &outputProcessor{}
		if p.flush() != nil {
			t.Fatal("expected nil flush on empty buffer")
		}
	})

	t.Run("flush after normal output returns nil", func(t *testing.T) {
		p := &outputProcessor{}
		p.process([]byte("hello"))
		if p.flush() != nil {
			t.Fatal("expected nil flush after complete output")
		}
	})

	t.Run("OSC terminated by ST passed through", func(t *testing.T) {
		p := &outputProcessor{}
		// OSC terminated by ST (\x1b\) rather than BEL.
		osc := "\x1b]0;title\x1b\\"
		r := p.process([]byte(osc))
		if r.triggered {
			t.Fatal("unexpected trigger on ST-terminated OSC")
		}
		if string(r.forward) != osc {
			t.Fatalf("expected %q forwarded, got %q", osc, string(r.forward))
		}
	})
}
