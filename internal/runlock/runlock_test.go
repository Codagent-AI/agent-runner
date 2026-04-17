package runlock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	t.Run("creates lock file with current PID", func(t *testing.T) {
		dir := t.TempDir()
		if err := Write(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dir, "lock"))
		if err != nil {
			t.Fatalf("failed to read lock file: %v", err)
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatalf("lock file does not contain valid PID: %v", err)
		}
		if pid != os.Getpid() {
			t.Fatalf("expected PID %d, got %d", os.Getpid(), pid)
		}
	})

	t.Run("returns error for invalid directory", func(t *testing.T) {
		err := Write("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Fatal("expected error for invalid directory")
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("removes lock file", func(t *testing.T) {
		dir := t.TempDir()
		Write(dir)

		Delete(dir)

		if _, err := os.Stat(filepath.Join(dir, "lock")); !os.IsNotExist(err) {
			t.Fatal("expected lock file to be deleted")
		}
	})

	t.Run("does not panic when lock file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		Delete(dir) // should not panic
	})
}

func TestCheck(t *testing.T) {
	t.Run("returns LockNone when no lock file exists", func(t *testing.T) {
		dir := t.TempDir()
		status := Check(dir)
		if status != LockNone {
			t.Fatalf("expected LockNone, got %d", status)
		}
	})

	t.Run("returns LockActive for current process PID", func(t *testing.T) {
		dir := t.TempDir()
		Write(dir)

		status := Check(dir)
		if status != LockActive {
			t.Fatalf("expected LockActive, got %d", status)
		}
	})

	t.Run("returns LockStale for dead PID", func(t *testing.T) {
		dir := t.TempDir()
		// Use a very high PID that is extremely unlikely to be alive.
		os.WriteFile(filepath.Join(dir, "lock"), []byte("999999999\n"), 0o600)

		status := Check(dir)
		if status != LockStale {
			t.Fatalf("expected LockStale, got %d", status)
		}
	})

	t.Run("returns LockStale for non-numeric content", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte("not-a-pid\n"), 0o600)

		status := Check(dir)
		if status != LockStale {
			t.Fatalf("expected LockStale for non-numeric content, got %d", status)
		}
	})

	t.Run("handles lock file with extra whitespace", func(t *testing.T) {
		dir := t.TempDir()
		content := fmt.Sprintf("  %d  \n", os.Getpid())
		os.WriteFile(filepath.Join(dir, "lock"), []byte(content), 0o600)

		status := Check(dir)
		if status != LockActive {
			t.Fatalf("expected LockActive with whitespace, got %d", status)
		}
	})

	t.Run("returns LockStale for PID zero", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte("0\n"), 0o600)

		status := Check(dir)
		if status != LockStale {
			t.Fatalf("expected LockStale for PID 0, got %d", status)
		}
	})

	t.Run("returns LockStale for negative PID", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte("-1\n"), 0o600)

		status := Check(dir)
		if status != LockStale {
			t.Fatalf("expected LockStale for negative PID, got %d", status)
		}
	})

	t.Run("returns LockStale for unreadable lock file", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "lock")
		// Make "lock" a directory rather than a regular file so the read
		// attempt deterministically fails on all platforms (chmod 0o000 is
		// unreliable on Windows and bypassable by root on Unix).
		if err := os.Mkdir(lockPath, 0o755); err != nil {
			t.Fatalf("failed to create lock directory: %v", err)
		}

		status := Check(dir)
		if status != LockStale {
			t.Fatalf("expected LockStale for unreadable lock, got %d", status)
		}
	})
}

func TestCheckOwnedByOther(t *testing.T) {
	t.Run("returns false when no lock file exists", func(t *testing.T) {
		dir := t.TempDir()
		if CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected false for missing lock file")
		}
	})

	t.Run("returns false when lock belongs to self", func(t *testing.T) {
		dir := t.TempDir()
		Write(dir)
		if CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected false when lock belongs to self")
		}
	})

	t.Run("returns false when lock PID is dead", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte("999999999\n"), 0o600)
		if CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected false for dead PID")
		}
	})

	t.Run("returns false for non-numeric content", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte("not-a-pid\n"), 0o600)
		if CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected false for non-numeric content")
		}
	})

	t.Run("returns false for PID zero", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte("0\n"), 0o600)
		if CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected false for PID zero")
		}
	})

	t.Run("returns true when lock belongs to another live process", func(t *testing.T) {
		dir := t.TempDir()
		cmd := exec.Command("sleep", "60")
		if err := cmd.Start(); err != nil {
			t.Skip("cannot start subprocess:", err)
		}
		defer cmd.Process.Kill()

		pid := cmd.Process.Pid
		os.WriteFile(filepath.Join(dir, "lock"), []byte(fmt.Sprintf("%d\n", pid)), 0o600)
		if !CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected true when lock belongs to another live process")
		}
	})

	t.Run("returns false when lock PID matches selfPID even if alive", func(t *testing.T) {
		dir := t.TempDir()
		// Write a PID matching selfPID — even though we're alive, it's our own lock.
		os.WriteFile(filepath.Join(dir, "lock"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
		if CheckOwnedByOther(dir, os.Getpid()) {
			t.Fatal("expected false when PID matches selfPID")
		}
	})
}
