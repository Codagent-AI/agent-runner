package pty

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPTYDebugLogger(t *testing.T) {
	t.Run("disabled without env", func(t *testing.T) {
		t.Setenv(ptyDebugLogEnv, "")
		if got := openPTYDebugLogger(""); got != nil {
			got.close()
			t.Fatal("expected debug logger to be disabled")
		}
	})

	t.Run("logs raw chunks and processed results", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pty-debug.log")
		t.Setenv(ptyDebugLogEnv, path)

		l := openPTYDebugLogger("cli=claude step=review")
		if l == nil {
			t.Fatal("expected debug logger")
		}
		l.logChunk("pty_output_raw", []byte("A\x1b[2K"))
		l.logResult(outputResult{forward: []byte("A"), triggered: true})
		l.close()

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read debug log: %v", err)
		}
		log := string(data)
		for _, want := range []string{
			`opened PTY debug log context="cli=claude step=review"`,
			"pty_output_raw len=5 hex=41 1b 5b 32 4b",
			`text="A\x1b[2K"`,
			"processed triggered=true forward_len=1",
			"closed PTY debug log",
		} {
			if !strings.Contains(log, want) {
				t.Fatalf("debug log missing %q:\n%s", want, log)
			}
		}
	})

	t.Run("logs marker near misses", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pty-debug.log")
		t.Setenv(ptyDebugLogEnv, path)

		l := openPTYDebugLogger("cli=opencode")
		if l == nil {
			t.Fatal("expected debug logger")
		}
		proc := &outputProcessor{textSentinel: textSentinel, textBuf: []byte("AGENT_RUNNER"), textStartBoundary: true, textSawVisible: true}
		raw := []byte("prefix " + textSentinel + " suffix")
		result := outputResult{forward: raw}
		l.logMarkerNearMiss(raw, result, proc)
		l.close()

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read debug log: %v", err)
		}
		log := string(data)
		for _, want := range []string{
			"marker visible but not triggered",
			`text_buf="AGENT_RUNNER"`,
			"raw_contains_marker=true",
			"forward_contains_marker=true",
		} {
			if !strings.Contains(log, want) {
				t.Fatalf("debug log missing %q:\n%s", want, log)
			}
		}
	})
}
