package resumehandoff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkerPath(t *testing.T) {
	got := MarkerPath(filepath.Join("runs", "debug-1"))
	want := filepath.Join("runs", "debug-1", "resume-target")
	if got != want {
		t.Fatalf("MarkerPath() = %q, want %q", got, want)
	}
}

func TestRead(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		runID, ok, err := Read(t.TempDir())
		if err != nil {
			t.Fatalf("Read returned error: %v", err)
		}
		if ok || runID != "" {
			t.Fatalf("Read() = %q, %v, want empty false", runID, ok)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(MarkerPath(dir), []byte("  run-123\n"), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		runID, ok, err := Read(dir)
		if err != nil {
			t.Fatalf("Read returned error: %v", err)
		}
		if !ok || runID != "run-123" {
			t.Fatalf("Read() = %q, %v, want run-123 true", runID, ok)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(MarkerPath(dir), []byte(" \n\t"), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		runID, ok, err := Read(dir)
		if err != nil {
			t.Fatalf("Read returned error: %v", err)
		}
		if ok || runID != "" {
			t.Fatalf("Read() = %q, %v, want empty false", runID, ok)
		}
	})

	t.Run("read error ignored", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(MarkerPath(dir), 0o700); err != nil {
			t.Fatalf("create marker directory: %v", err)
		}
		runID, ok, err := Read(dir)
		if err != nil {
			t.Fatalf("Read returned error: %v", err)
		}
		if ok || runID != "" {
			t.Fatalf("Read() = %q, %v, want empty false", runID, ok)
		}
	})
}
