package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

const scriptWorkerEnv = "AGENT_RUNNER_TESTSCRIPT_WORKER"
const scriptTempDirEnv = "AGENT_RUNNER_TESTSCRIPT_TEMPDIR"
const scriptPIDMarker = "supervisor.pid"
const scriptSignalGrace = 5 * time.Second

type scriptTestRun func() int

func (run scriptTestRun) Run() int { return run() }

func TestMain(m *testing.M) {
	// Script commands use a reduced environment. Identify the installed binary
	// by its location so a top-level test binary named agent-runner still runs tests.
	command := isScriptCommand()
	if !command && os.Getenv(scriptWorkerEnv) != "1" {
		os.Exit(runScriptWorker())
	}
	run := testscript.TestingM(m)
	if !command {
		// Upstream dispatches on argv[0]; normalize only the test-harness invocation.
		os.Args[0] = "agent-runner.test"
	}
	if dir := os.Getenv(scriptTempDirEnv); !command && dir != "" {
		run = scriptRunWithTempDir(m, dir)
	}
	testscript.Main(run, map[string]func(){
		"agent-runner": func() { main() },
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/scripts",
		Setup: func(env *testscript.Env) error {
			env.Setenv("HOME", env.WorkDir)
			env.Setenv("AGENT_RUNNER_NO_TUI", "1")
			return nil
		},
	})
}

// testscript.Main owns its binary copy through a defer around m.Run. Test
// timeouts, panics in test goroutines, and signals skip that defer. Keep a
// supervisor outside the tests so it can reclaim this run's private temporary
// directory even when the worker exits abruptly. Stale directories are swept
// at startup only when their supervisor is confirmed dead.
// The worker runs in its own process group so signals, escalation, and the final
// cleanup also reach commands it started. SIGKILL of the supervisor (or SIGKILL of the entire process group) cannot be
// handled; such a cancellation can still leave the private directory behind.
func runScriptWorker() int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(signals)

	sweepScriptTempDirs(os.TempDir(), func(pid int) error { return syscall.Kill(pid, 0) })
	dir, err := os.MkdirTemp("", "agent-runner-testscript-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, "clean up testscript temporary directory:", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, scriptPIDMarker), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write testscript supervisor pid:", err)
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Env = append(os.Environ(), scriptWorkerEnv+"=1", scriptTempDirEnv+"="+dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runInOwnProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	var escalation *time.Timer
	var kill <-chan time.Time
	defer func() {
		if escalation != nil {
			escalation.Stop()
		}
	}()
	for {
		select {
		case sig := <-signals:
			signalProcessGroup(cmd, sig)
			if escalation == nil {
				escalation = time.NewTimer(scriptSignalGrace)
				kill = escalation.C
			}
		case <-kill:
			killProcessGroup(cmd)
			kill = nil
		case err := <-finished:
			// Reap anything the worker left running before its directory is removed.
			killProcessGroup(cmd)
			if err == nil {
				return 0
			}
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal())
			}
			if code := cmd.ProcessState.ExitCode(); code >= 0 {
				return code
			}
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
}

// Keep unmarked directories and ambiguous process-check results: only ESRCH
// proves that the supervisor no longer owns the directory.
func sweepScriptTempDirs(root string, checkProcess func(int) error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan testscript temporary directories:", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "agent-runner-testscript-") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, scriptPIDMarker))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				fmt.Fprintln(os.Stderr, "read testscript supervisor pid:", err)
			}
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		if err := checkProcess(pid); !errors.Is(err, syscall.ESRCH) {
			if err != nil {
				fmt.Fprintln(os.Stderr, "check testscript supervisor pid:", err)
			}
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, "sweep testscript temporary directory:", err)
		}
	}
}

func scriptRunWithTempDir(m *testing.M, dir string) testscript.TestingM {
	// Restrict the private temp root to testscript's binary setup. In particular,
	// ordinary tests must retain their original short Unix socket paths.
	restore := make(map[string]*string)
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if value, ok := os.LookupEnv(key); ok {
			restore[key] = &value
		} else {
			restore[key] = nil
		}
		if err := os.Setenv(key, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	return scriptTestRun(func() int {
		for key, value := range restore {
			var err error
			if value == nil {
				err = os.Unsetenv(key)
			} else {
				err = os.Setenv(key, *value)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		return m.Run()
	})
}

func isScriptCommand() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	name := filepath.Base(executable)
	bin := filepath.Dir(executable)
	return (name == "agent-runner" || name == "agent-runner.exe") && filepath.Base(bin) == "bin" &&
		strings.HasPrefix(filepath.Base(filepath.Dir(bin)), "testscript-main")
}
