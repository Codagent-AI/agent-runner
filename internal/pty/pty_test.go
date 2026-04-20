package pty

import (
	"errors"
	"os/exec"
	"testing"
)

func TestShellResultFromWait(t *testing.T) {
	t.Run("returns exit code zero for nil error", func(t *testing.T) {
		result, err := shellResultFromWait(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", result.ExitCode)
		}
	})

	t.Run("returns exit code for process exit error", func(t *testing.T) {
		cmd := exec.Command("sh", "-c", "exit 7")
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected process to fail")
		}

		result, mappedErr := shellResultFromWait(err)
		if mappedErr != nil {
			t.Fatalf("unexpected error: %v", mappedErr)
		}
		if result.ExitCode != 7 {
			t.Fatalf("expected exit code 7, got %d", result.ExitCode)
		}
	})

	t.Run("returns wrapped error for unexpected wait failures", func(t *testing.T) {
		waitErr := errors.New("wait failure")

		result, err := shellResultFromWait(waitErr)
		if err == nil {
			t.Fatal("expected error")
		}
		if result.ExitCode != 0 {
			t.Fatalf("expected zero-value result on error, got %d", result.ExitCode)
		}
		if !errors.Is(err, waitErr) {
			t.Fatalf("expected wrapped wait error, got %v", err)
		}
	})
}
