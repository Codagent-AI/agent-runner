package runlock

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

	t.Run("keeps a lock file whose OS lock is held by another owner", func(t *testing.T) {
		// Another acquirer has taken the OS lock but has not yet replaced the
		// stale PID; Delete must not unlink the inode it holds.
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "lock")
		if err := os.WriteFile(lockPath, []byte("999999999\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		other, outcome, err := lockStablePath(lockPath)
		if err != nil || outcome != lockAcquired {
			t.Fatalf("lockStablePath() = (%v, %v)", outcome, err)
		}
		defer release(other)

		Delete(dir)

		if _, err := os.Stat(lockPath); err != nil {
			t.Fatalf("Delete removed a lock file held by another owner: %v", err)
		}
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

func TestAcquire(t *testing.T) {
	t.Run("acquires when no lock exists", func(t *testing.T) {
		dir := t.TempDir()
		activePID, err := Acquire(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if activePID != 0 {
			t.Fatalf("expected activePID 0 (acquired), got %d", activePID)
		}
		data, readErr := os.ReadFile(filepath.Join(dir, "lock"))
		if readErr != nil {
			t.Fatalf("expected lock file to exist: %v", readErr)
		}
		gotPID, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if gotPID != os.Getpid() {
			t.Fatalf("expected lock to contain PID %d, got %d", os.Getpid(), gotPID)
		}
	})

	t.Run("replaces stale PID even when it belongs to a live process", func(t *testing.T) {
		dir := t.TempDir()
		content := fmt.Sprintf("%d\n", os.Getppid())
		if err := os.WriteFile(filepath.Join(dir, "lock"), []byte(content), 0o600); err != nil {
			t.Fatalf("seed lock: %v", err)
		}

		activePID, err := Acquire(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if activePID != 0 {
			t.Fatalf("expected acquired lock, got activePID %d", activePID)
		}
		data, err := os.ReadFile(filepath.Join(dir, "lock"))
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		if string(data) != fmt.Sprintf("%d\n", os.Getpid()) {
			t.Fatalf("expected current PID in lock, got %q", data)
		}
	})

	t.Run("replaces stale lock and acquires", func(t *testing.T) {
		dir := t.TempDir()
		// PID 999999999 is essentially guaranteed dead.
		if err := os.WriteFile(filepath.Join(dir, "lock"), []byte("999999999\n"), 0o600); err != nil {
			t.Fatalf("seed stale lock: %v", err)
		}

		activePID, err := Acquire(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if activePID != 0 {
			t.Fatalf("expected activePID 0 (acquired), got %d", activePID)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "lock"))
		gotPID, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if gotPID != os.Getpid() {
			t.Fatalf("expected stale lock replaced with our PID %d, got %d", os.Getpid(), gotPID)
		}
	})

	t.Run("atomicity: only one of two concurrent acquirers wins", func(t *testing.T) {
		// Proves O_CREATE|O_EXCL semantics — the old check-then-write path
		// let both callers observe "no active lock" and both succeed.
		dir := t.TempDir()
		const n = 16
		winners := 0
		losers := 0
		errs := 0
		var mu sync.Mutex
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				activePID, err := Acquire(dir)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err != nil:
					errs++
				case activePID == 0:
					winners++
				default:
					losers++
				}
			}()
		}
		close(start)
		wg.Wait()
		if errs != 0 {
			t.Fatalf("unexpected errors: %d", errs)
		}
		if winners != 1 {
			t.Fatalf("expected exactly 1 winner, got %d (losers=%d)", winners, losers)
		}
	})

	t.Run("returns error when lock path is unreadable directory", func(t *testing.T) {
		dir := t.TempDir()
		// Make "lock" a directory so both read and create fail deterministically.
		if err := os.Mkdir(filepath.Join(dir, "lock"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		activePID, err := Acquire(dir)
		if err == nil {
			t.Fatalf("expected error when lock path is a directory, got activePID=%d", activePID)
		}
		if activePID != 0 {
			t.Fatalf("expected activePID 0 on error, got %d", activePID)
		}
	})

	t.Run("sweeps stale lock-*.tmp files on acquire", func(t *testing.T) {
		dir := t.TempDir()
		staleTmp := filepath.Join(dir, "lock-abandoned.tmp")
		if err := os.WriteFile(staleTmp, []byte("leftover\n"), 0o600); err != nil {
			t.Fatalf("seed tmp: %v", err)
		}
		// Backdate to before the sweep cutoff.
		old := time.Now().Add(-30 * time.Minute)
		if err := os.Chtimes(staleTmp, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		// Also seed a fresh one; the sweep must leave it alone.
		freshTmp := filepath.Join(dir, "lock-fresh.tmp")
		if err := os.WriteFile(freshTmp, []byte("active\n"), 0o600); err != nil {
			t.Fatalf("seed fresh tmp: %v", err)
		}

		if _, err := Acquire(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
			t.Fatalf("expected stale tmp file to be swept, got stat err=%v", err)
		}
		if _, err := os.Stat(freshTmp); err != nil {
			t.Fatalf("expected recent tmp file preserved, got err=%v", err)
		}
	})
}

func TestStaleLockAcrossProcesses(t *testing.T) {
	if os.Getenv("RUNLOCK_STALE_HELPER") == "1" {
		gate := os.Getenv("RUNLOCK_GATE")
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		pid, err := Acquire(os.Getenv("RUNLOCK_DIR"))
		if err != nil {
			fmt.Println("error", err)
			return
		}
		if pid > 0 {
			fmt.Println("busy", pid)
			return
		}
		fmt.Println("acquired")
		time.Sleep(300 * time.Millisecond)
		Delete(os.Getenv("RUNLOCK_DIR"))
		return
	}
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "lock"), []byte("999999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gate := filepath.Join(t.TempDir(), "go")
	var outputs [2]bytes.Buffer
	var commands []*exec.Cmd
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=TestStaleLockAcrossProcesses$")
		cmd.Env = append(os.Environ(), "RUNLOCK_STALE_HELPER=1", "RUNLOCK_DIR="+dir, "RUNLOCK_GATE="+gate)
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %d: %v %s", i, err, outputs[i].String())
		}
	}
	winners := 0
	losers := 0
	for _, out := range outputs {
		if strings.Contains(out.String(), "acquired") {
			winners++
		}
		if strings.Contains(out.String(), "busy") {
			losers++
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("winners=%d losers=%d outputs=%q, %q", winners, losers, outputs[0].String(), outputs[1].String())
	}
}

func TestCrashedHolderReleasesLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGKILL fixture is POSIX-only")
	}
	if os.Getenv("RUNLOCK_CRASH_HELPER") == "1" {
		pid, err := Acquire(os.Getenv("RUNLOCK_DIR"))
		if err != nil || pid != 0 {
			fmt.Println("error", pid, err)
			return
		}
		fmt.Println("acquired")
		time.Sleep(10 * time.Second)
		return
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestCrashedHolderReleasesLock$")
	cmd.Env = append(os.Environ(), "RUNLOCK_CRASH_HELPER=1", "RUNLOCK_DIR="+dir)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil || !strings.Contains(line, "acquired") {
		_ = cmd.Process.Kill()
		t.Fatalf("helper did not acquire: %q %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	pid, err := Acquire(dir)
	if err != nil || pid != 0 {
		t.Fatalf("lock after crash: %d %v", pid, err)
	}
	Delete(dir)
}

func TestProveHeldRequiresCurrentProcessLock(t *testing.T) {
	dir := t.TempDir()
	if _, err := ProveHeld(dir); err == nil {
		t.Fatal("ProveHeld succeeded without an acquired run lock")
	}

	activePID, err := Acquire(dir)
	if err != nil || activePID != 0 {
		t.Fatalf("Acquire() = (%d, %v)", activePID, err)
	}
	proof, err := ProveHeld(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.Validate(dir); err != nil {
		t.Fatalf("Validate() with held lock: %v", err)
	}

	Delete(dir)
	if err := proof.Validate(dir); err == nil {
		t.Fatal("stale proof remained valid after the run lock was deleted")
	}
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
