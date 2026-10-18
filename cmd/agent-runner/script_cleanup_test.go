package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const cleanupFixtureEnv = "AGENT_RUNNER_TESTSCRIPT_CLEANUP_FIXTURE"

func TestSweepScriptTempDirs(t *testing.T) {
	for _, tt := range []struct {
		name          string
		entry         string
		marker        string
		worker        string
		missingWorker bool
		scope         string
		missingScope  bool
		checkErr      error
		file          bool
		wantRemove    bool
		wantCheck     bool
	}{
		{name: "dead", marker: "12345", checkErr: syscall.ESRCH, wantRemove: true, wantCheck: true},
		{name: "live", marker: "12345", wantCheck: true},
		{name: "self", marker: strconv.Itoa(os.Getpid())},
		{name: "missing"},
		{name: "garbage", marker: "not a pid"},
		{name: "zero", marker: "0"},
		{name: "negative", marker: "-1"},
		{name: "permission denied", marker: "12345", checkErr: syscall.EPERM, wantCheck: true},
		{name: "unknown error", marker: "12345", checkErr: syscall.EIO, wantCheck: true},
		{name: "nonmatching", entry: "unrelated", marker: "12345", checkErr: syscall.ESRCH},
		{name: "file", file: true},
		{name: "orphan live worker", marker: "12345", worker: "12346", checkErr: syscall.ESRCH, wantCheck: true},
		{name: "orphan self worker", marker: "12345", worker: strconv.Itoa(os.Getpid()), checkErr: syscall.ESRCH, wantCheck: true},
		{name: "missing worker", marker: "12345", missingWorker: true, checkErr: syscall.ESRCH, wantCheck: true},
		{name: "garbage worker", marker: "12345", worker: "garbage", checkErr: syscall.ESRCH, wantCheck: true},
		{name: "foreign scope", marker: "12345", scope: "other-scope", checkErr: syscall.ESRCH},
		{name: "missing scope", marker: "12345", missingScope: true, checkErr: syscall.ESRCH},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			name := tt.entry
			if name == "" {
				name = "agent-runner-testscript-fixture"
			}
			dir := filepath.Join(root, name)
			if tt.file {
				if err := os.WriteFile(dir, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			worker := tt.worker
			if worker == "" && !tt.missingWorker {
				worker = tt.marker
			}
			scope := tt.scope
			if scope == "" && !tt.missingScope {
				scope = "test-scope"
			}
			if !tt.file {
				writeScriptSweepMarkers(t, dir, tt.marker, worker, scope)
			}
			checked := false
			sweepScriptTempDirs(root, "test-scope", func(pid int) error {
				checked = true
				if pid == 12346 {
					return nil
				}
				if pid != 12345 {
					t.Fatalf("checked pid = %d, want 12345", pid)
				}
				return tt.checkErr
			})
			if checked != tt.wantCheck {
				t.Errorf("process checked = %v, want %v", checked, tt.wantCheck)
			}
			_, err := os.Stat(dir)
			if tt.wantRemove {
				if !os.IsNotExist(err) {
					t.Fatalf("stale directory remains: %v", err)
				}
			} else if err != nil {
				t.Fatalf("entry not preserved: %v", err)
			}
		})
	}
}

func writeScriptSweepMarkers(t *testing.T, dir, supervisor, worker, scope string) {
	t.Helper()
	for name, value := range map[string]string{
		scriptPIDMarker:       supervisor,
		scriptWorkerPIDMarker: worker,
		scriptPIDScopeMarker:  scope,
	} {
		if value == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScriptSweepStartup(t *testing.T) {
	scope := scriptPIDScope()
	if scope == "" {
		t.Skip("PID scope unavailable")
	}
	// Reap a real subprocess, then use its PID to exercise signal 0 via TestMain.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(dead.Process.Pid)
	root := t.TempDir()
	stale := filepath.Join(root, "agent-runner-testscript-stale")
	foreign := filepath.Join(root, "agent-runner-testscript-foreign")
	for dir, ownerScope := range map[string]string{stale: scope, foreign: scope + "-foreign"} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeScriptSweepMarkers(t, dir, pid, pid, ownerScope)
	}
	child := startCleanupFixture(t, root, "normal")
	if err := child.wait(t); err != nil {
		t.Fatalf("startup fixture: %v, %s", err, child.output.String())
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup did not sweep stale directory: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("startup removed foreign PID scope: %v", err)
	}
}

func TestScriptSweepPreservesOrphanWorker(t *testing.T) {
	root := t.TempDir()
	live := startCleanupFixture(t, root, "terminate")
	markers, err := filepath.Glob(filepath.Join(root, "agent-runner-testscript-*", scriptWorkerPIDMarker))
	if err != nil || len(markers) != 1 {
		t.Fatalf("worker marker: %v, %v", markers, err)
	}
	data, err := os.ReadFile(markers[0])
	if err != nil {
		t.Fatal(err)
	}
	workerPID, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	// The worker owns its process group. Always terminate it even if an assertion fails.
	t.Cleanup(func() { _ = syscall.Kill(-workerPID, syscall.SIGKILL) })
	if err := live.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(live.cmd.Process.Pid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatal("supervisor was not reaped")
		}
		time.Sleep(10 * time.Millisecond)
	}
	other := startCleanupFixture(t, root, "normal")
	if err := other.wait(t); err != nil {
		t.Fatalf("startup fixture: %v, %s", err, other.output.String())
	}
	if err := syscall.Kill(workerPID, 0); err != nil {
		t.Fatalf("orphan worker stopped before sweep: %v", err)
	}
	if _, err := os.Stat(markers[0]); err != nil {
		t.Fatalf("startup swept a live orphan worker: %v", err)
	}
	if err := syscall.Kill(-workerPID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	live.mode = "killed"
	if err := live.wait(t); err == nil {
		t.Fatal("expected killed supervisor")
	}
}

// This test only performs the requested termination inside an isolated worker.
func TestScriptCleanupFixture(t *testing.T) {
	mode := os.Getenv(cleanupFixtureEnv)
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	marker, err := os.ReadFile(filepath.Join(os.Getenv(scriptTempDirEnv), scriptPIDMarker))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(marker), strconv.Itoa(os.Getppid()); got != want {
		t.Fatalf("supervisor pid marker = %q, want %q", got, want)
	}
	workerMarker, err := os.ReadFile(filepath.Join(os.Getenv(scriptTempDirEnv), scriptWorkerPIDMarker))
	if err != nil || string(workerMarker) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("worker pid marker = %q, error = %v", workerMarker, err)
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got, want := os.Getenv(key), os.Getenv("AGENT_RUNNER_TESTSCRIPT_EXPECT_TEMP"); got != want {
			t.Fatalf("%s = %q, want original temporary directory %q", key, got, want)
		}
	}
	if mode == "ignore-terminate" {
		signal.Ignore(syscall.SIGTERM)
	}
	if mode == "descendant" {
		// A foreground testscript exec is a child of this worker; model one that
		// does not exit when the worker is signalled.
		child := exec.Command("sleep", "60")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		pid := []byte(strconv.Itoa(child.Process.Pid))
		if err := os.WriteFile(os.Getenv("AGENT_RUNNER_TESTSCRIPT_CHILD_PID"), pid, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(os.Getenv("AGENT_RUNNER_TESTSCRIPT_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "normal":
	case "command-exit":
		// main calls os.Exit in the command subprocess, not in the harness worker.
		cmd := exec.Command("agent-runner", "--invalid-cleanup-fixture-flag")
		cmd.Env = append(os.Environ(), scriptWorkerEnv+"=")
		if err := cmd.Run(); err == nil {
			t.Fatal("expected command to exit unsuccessfully")
		}
	case "failure", "failfast":
		t.Fatal("intentional fixture failure")
	case "panic":
		panic("intentional fixture panic")
	case "exit":
		os.Exit(17)
	case "ignore-terminate":
		select {}
	case "timeout", "interrupt", "terminate", "hangup", "quit", "descendant":
		select {}
	default:
		t.Fatalf("unknown fixture: %s", mode)
	}
}

func TestScriptCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signal termination")
	}
	for _, mode := range []string{"normal", "command-exit", "failure", "failfast", "panic", "exit", "timeout", "interrupt", "terminate", "hangup", "quit"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			child := startCleanupFixture(t, root, mode)
			err := child.wait(t)
			success := mode == "normal" || mode == "command-exit"
			if (err == nil) != success {
				t.Fatalf("exit error = %v, output:\n%s", err, child.output.String())
			}
			if mode == "exit" && child.cmd.ProcessState.ExitCode() != 17 {
				t.Fatalf("exit code = %d, want 17", child.cmd.ProcessState.ExitCode())
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary files remain: %v, %v", entries, err)
			}
		})
	}
}

func TestScriptCleanupConcurrentRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signal termination")
	}
	root := t.TempDir()
	live := startCleanupFixture(t, root, "terminate")
	dirs, err := filepath.Glob(filepath.Join(root, "agent-runner-testscript-*", "testscript-main*", "bin", "agent-runner"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("live worker binary: %v, %v", dirs, err)
	}
	other := startCleanupFixture(t, root, "panic")
	if err := other.wait(t); err == nil {
		t.Fatal("expected panic")
	}
	if _, err := os.Stat(dirs[0]); err != nil {
		t.Fatalf("another run removed the live worker's binary: %v", err)
	}
	if err := live.wait(t); err == nil {
		t.Fatal("expected signal termination")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

type cleanupFixture struct {
	cmd    *exec.Cmd
	output bytes.Buffer
	done   chan error
	mode   string
	waited bool
	// childPID names the file holding the pid of the "descendant" fixture's child.
	childPID string
}

func startCleanupFixture(t *testing.T, root, mode string) *cleanupFixture {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return startCleanupFixtureBinary(t, root, mode, executable)
}

func startCleanupFixtureBinary(t *testing.T, root, mode, executable string) *cleanupFixture {
	t.Helper()
	args := []string{"-test.run=^TestScriptCleanupFixture$", "-test.timeout=15s"}
	if mode == "timeout" {
		args[1] = "-test.timeout=3s"
	}
	if mode == "failfast" {
		args = append(args, "-test.failfast")
	}
	ready := filepath.Join(t.TempDir(), "ready")
	child := &cleanupFixture{cmd: exec.Command(executable, args...), done: make(chan error, 1), mode: mode}
	childPID := filepath.Join(t.TempDir(), "child-pid")
	child.childPID = childPID
	child.cmd.Env = append(os.Environ(), "AGENT_RUNNER_TESTSCRIPT_CHILD_PID="+childPID, scriptWorkerEnv+"=", scriptTempDirEnv+"=", cleanupFixtureEnv+"="+mode, "AGENT_RUNNER_TESTSCRIPT_READY="+ready, "AGENT_RUNNER_TESTSCRIPT_EXPECT_TEMP="+root, "TMPDIR="+root, "TMP="+root, "TEMP="+root)
	child.cmd.Stdout, child.cmd.Stderr = &child.output, &child.output
	if err := child.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { child.done <- child.cmd.Wait() }()
	t.Cleanup(func() {
		// This is only a safety net for a failed assertion, never the cleanup under test.
		if child.waited {
			return
		}
		_ = child.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-child.done:
		case <-time.After(10 * time.Second):
			_ = child.cmd.Process.Kill()
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-child.done:
			child.waited = true
			t.Fatalf("fixture exited before ready: %v, %s", err, child.output.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return child
}

func (child *cleanupFixture) wait(t *testing.T) error {
	t.Helper()
	switch child.mode {
	case "interrupt":
		if err := child.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
	case "hangup":
		if err := child.cmd.Process.Signal(syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
	case "quit":
		if err := child.cmd.Process.Signal(syscall.SIGQUIT); err != nil {
			t.Fatal(err)
		}
	case "terminate", "ignore-terminate", "descendant":
		if err := child.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-child.done:
		child.waited = true
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("fixture did not exit")
		return nil
	}
}

func TestScriptCleanupSignalEscalation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signal termination")
	}
	root := t.TempDir()
	child := startCleanupFixture(t, root, "ignore-terminate")
	if err := child.wait(t); err == nil {
		t.Fatal("expected forced termination")
	}
	if got, want := child.cmd.ProcessState.ExitCode(), 128+int(syscall.SIGKILL); got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestScriptCleanupNamedTestBinary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	name := "agent-runner"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	renamed := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(renamed, data, 0o700); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	child := startCleanupFixtureBinary(t, root, "command-exit", renamed)
	if err := child.wait(t); err != nil {
		t.Fatalf("named test binary failed: %v, %s", err, child.output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestScriptCleanupDescendants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process groups")
	}
	root := t.TempDir()
	child := startCleanupFixture(t, root, "descendant")
	data, err := os.ReadFile(child.childPID)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := child.wait(t); err == nil {
		t.Fatal("expected signal termination")
	}
	deadline := time.Now().Add(5 * time.Second)
	for processRunning(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("worker's child %d outlived the supervisor", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

// processRunning reports whether pid is alive. A zombie counts as dead: where
// PID 1 does not reap orphans it stays signalable but is no longer running.
func processRunning(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	// The state follows the parenthesised command name, which may contain spaces.
	if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) {
		return stat[i+2] != 'Z'
	}
	return true
}
