package pty

import "testing"

const sentinel = "\x1b]999;signal-continuation\x07"

func textOutputProcessor() *outputProcessor {
	return &outputProcessor{textSentinel: textSentinel}
}

func TestOutputProcessor(t *testing.T) {
	t.Run("zero value detects OSC sentinel but ignores replayable text marker", func(t *testing.T) {
		p := &outputProcessor{}
		text := p.process([]byte("before\n" + textSentinel + "\nafter"))
		if text.triggered {
			t.Fatal("unexpected trigger on replayable text marker")
		}
		if string(text.forward) != "before\n"+textSentinel+"\nafter" {
			t.Fatalf("expected text marker forwarded, got %q", string(text.forward))
		}

		osc := p.process([]byte(sentinel))
		if !osc.triggered {
			t.Fatal("expected OSC sentinel trigger")
		}
		if len(osc.forward) != 0 {
			t.Fatalf("expected OSC sentinel stripped, got %q", string(osc.forward))
		}
	})

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
		p := textOutputProcessor()
		r := p.process([]byte(sentinel))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected no forwarded bytes, got %q", string(r.forward))
		}
	})

	t.Run("sentinel embedded in other output", func(t *testing.T) {
		p := textOutputProcessor()
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

	t.Run("detects text sentinel line and strips it", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n" + textSentinel + "\nafter"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "before\nafter" {
			t.Fatalf("expected %q, got %q", "before\nafter", string(r.forward))
		}
	})

	t.Run("detects only configured text sentinel", func(t *testing.T) {
		current := textSentinel + "_current"
		stale := textSentinel + "_stale"
		p := &outputProcessor{textSentinel: current}

		old := p.process([]byte("before\n" + stale + "\nafter\n"))
		if old.triggered {
			t.Fatal("unexpected trigger on stale text marker")
		}
		if string(old.forward) != "before\n"+stale+"\nafter\n" {
			t.Fatalf("expected stale marker forwarded, got %q", string(old.forward))
		}

		next := p.process([]byte(current + "\n"))
		if !next.triggered {
			t.Fatal("expected trigger on configured text marker")
		}
		if len(next.forward) != 0 {
			t.Fatalf("expected configured marker stripped, got %q", string(next.forward))
		}
	})

	t.Run("detects codex bullet text sentinel line and strips it", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n• " + textSentinel + "\nafter"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "before\nafter" {
			t.Fatalf("expected %q, got %q", "before\nafter", string(r.forward))
		}
	})

	t.Run("detects styled codex text sentinel line and strips it", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n\x1b[2m• \x1b[22m" + textSentinel + "\x1b[39m\x1b[49m\x1b[0m\x1b[r" + "after"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "before\n\x1b[2m\x1b[22m\x1b[39m\x1b[49m\x1b[0m\x1b[rafter" {
			t.Fatalf("expected %q, got %q", "before\n\x1b[2m\x1b[22m\x1b[39m\x1b[49m\x1b[0m\x1b[rafter", string(r.forward))
		}
	})

	t.Run("detects text sentinel before terminal control sequence", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n" + textSentinel + "\x1b[2Kafter"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "before\n\x1b[2Kafter" {
			t.Fatalf("expected %q, got %q", "before\n\x1b[2Kafter", string(r.forward))
		}
	})

	t.Run("detects text sentinel with trailing spaces before terminal control sequence", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n" + textSentinel + "  \x1b[2Kafter"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "before\n\x1b[2Kafter" {
			t.Fatalf("expected %q, got %q", "before\n\x1b[2Kafter", string(r.forward))
		}
	})

	t.Run("does not trigger text sentinel before punctuation", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n" + textSentinel + ".\nafter"
		r := p.process([]byte(input))
		if r.triggered {
			t.Fatal("unexpected trigger before punctuation")
		}
		if string(r.forward) != input {
			t.Fatalf("expected %q, got %q", input, string(r.forward))
		}
	})

	t.Run("does not trigger backtick-wrapped text sentinel", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\n`" + textSentinel + "`\nafter"
		r := p.process([]byte(input))
		if r.triggered {
			t.Fatal("unexpected trigger on backtick-wrapped marker")
		}
		if string(r.forward) != input {
			t.Fatalf("expected %q, got %q", input, string(r.forward))
		}
	})

	t.Run("detects text sentinel after adapter prompt prefix", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before\nassistant> " + textSentinel + "\nafter"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "before\nassistant> after" {
			t.Fatalf("expected %q, got %q", "before\nassistant> after", string(r.forward))
		}
	})

	t.Run("detects text sentinel after cursor-painted prompt prefix", func(t *testing.T) {
		p := textOutputProcessor()
		input := "\x1b[12;1H> " + textSentinel + "\x1b[0m"
		r := p.process([]byte(input))
		if !r.triggered {
			t.Fatal("expected trigger")
		}
		if string(r.forward) != "\x1b[12;1H> \x1b[0m" {
			t.Fatalf("expected %q, got %q", "\x1b[12;1H> \x1b[0m", string(r.forward))
		}
	})

	t.Run("detects opencode split marker after cursor positioning", func(t *testing.T) {
		p := textOutputProcessor()

		r0 := p.process([]byte("Build"))
		if r0.triggered {
			t.Fatal("unexpected trigger before marker")
		}
		if string(r0.forward) != "Build" {
			t.Fatalf("expected %q, got %q", "Build", string(r0.forward))
		}

		first := "\x1b[30;6H\x1b[38;2;26;26;26mAGENT\x1b[0m\x1b[65;6H"
		r1 := p.process([]byte(first))
		if r1.triggered {
			t.Fatal("unexpected trigger on first marker chunk")
		}
		expectedFirst := "\x1b[30;6H\x1b[38;2;26;26;26m\x1b[0m\x1b[65;6H"
		if string(r1.forward) != expectedFirst {
			t.Fatalf("expected %q, got %q", expectedFirst, string(r1.forward))
		}

		second := "\x1b[30;11H\x1b[38;2;26;26;26m_RUNNER_CONTINUE\x1b[0m"
		r2 := p.process([]byte(second))
		if !r2.triggered {
			t.Fatal("expected trigger after second marker chunk")
		}
		expectedSecond := "\x1b[30;11H\x1b[38;2;26;26;26m\x1b[0m"
		if string(r2.forward) != expectedSecond {
			t.Fatalf("expected %q, got %q", expectedSecond, string(r2.forward))
		}
	})

	t.Run("does not treat styling as a text boundary", func(t *testing.T) {
		p := textOutputProcessor()

		r0 := p.process([]byte("Build"))
		if r0.triggered {
			t.Fatal("unexpected trigger before styled text")
		}

		input := "\x1b[38;2;26;26;26m" + textSentinel + "\n"
		r := p.process([]byte(input))
		if r.triggered {
			t.Fatal("unexpected trigger after style-only sequence")
		}
		if string(r.forward) != input {
			t.Fatalf("expected %q, got %q", input, string(r.forward))
		}
	})

	t.Run("does not trigger on embedded text sentinel", func(t *testing.T) {
		p := textOutputProcessor()
		input := "before" + textSentinel + "after"
		r := p.process([]byte(input))
		if r.triggered {
			t.Fatal("unexpected trigger")
		}
		if string(r.forward) != input {
			t.Fatalf("expected %q, got %q", input, string(r.forward))
		}
	})

	t.Run("text sentinel detection across chunk boundaries", func(t *testing.T) {
		p := textOutputProcessor()
		input := textSentinel + "\n"
		half := len(input) / 2
		r1 := p.process([]byte(input[:half]))
		if r1.triggered {
			t.Fatal("unexpected trigger on first half")
		}
		if len(r1.forward) != 0 {
			t.Fatalf("expected no forwarded bytes on first half, got %q", string(r1.forward))
		}

		r2 := p.process([]byte(input[half:]))
		if !r2.triggered {
			t.Fatal("expected trigger after second half")
		}
		if len(r2.forward) != 0 {
			t.Fatalf("expected no forwarded bytes on second half, got %q", string(r2.forward))
		}
	})

	t.Run("incomplete text sentinel at process exit is flushed", func(t *testing.T) {
		p := textOutputProcessor()
		partial := textSentinel[:10]
		r := p.process([]byte(partial))
		if r.triggered {
			t.Fatal("unexpected trigger on partial sentinel")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected no forwarded bytes (buffered), got %q", string(r.forward))
		}
		flushed := p.flush()
		if string(flushed) != partial {
			t.Fatalf("expected flushed %q, got %q", partial, string(flushed))
		}
	})

	t.Run("detects text sentinel at process exit without newline", func(t *testing.T) {
		p := textOutputProcessor()
		r := p.process([]byte(textSentinel))
		if r.triggered {
			t.Fatal("unexpected trigger before process exit")
		}
		if len(r.forward) != 0 {
			t.Fatalf("expected no forwarded bytes (buffered), got %q", string(r.forward))
		}
		finished := p.finish()
		if !finished.triggered {
			t.Fatal("expected trigger at process exit")
		}
		if len(finished.forward) != 0 {
			t.Fatalf("expected marker stripped at process exit, got %q", string(finished.forward))
		}
		if flushed := p.flush(); len(flushed) != 0 {
			t.Fatalf("expected no buffered output after marker trigger, got %q", string(flushed))
		}
	})

	t.Run("sentinel detection across chunk boundaries", func(t *testing.T) {
		p := textOutputProcessor()
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
		p := textOutputProcessor()
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
		p := textOutputProcessor()
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
		p := textOutputProcessor()
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
		p := textOutputProcessor()
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
		p := textOutputProcessor()
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
		p := textOutputProcessor()
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
		p := textOutputProcessor()
		if p.flush() != nil {
			t.Fatal("expected nil flush on empty buffer")
		}
	})

	t.Run("flush after normal output returns nil", func(t *testing.T) {
		p := textOutputProcessor()
		p.process([]byte("hello"))
		if p.flush() != nil {
			t.Fatal("expected nil flush after complete output")
		}
	})

	t.Run("OSC terminated by ST passed through", func(t *testing.T) {
		p := textOutputProcessor()
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
