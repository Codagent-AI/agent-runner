package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

const scriptWorkerEnv = "AGENT_RUNNER_TESTSCRIPT_WORKER"
const scriptTempDirEnv = "AGENT_RUNNER_TESTSCRIPT_TEMPDIR"
const scriptSignalGrace = 5 * time.Second

type scriptTestRun func() int

func (run scriptTestRun) Run() int { return run() }

func TestMain(m *testing.M) {
	// Script commands use a deliberately reduced environment, so recognize the
	// registered command by name rather than relying on the worker marker.
	name := filepath.Base(os.Args[0])
	if name != "agent-runner" && name != "agent-runner.exe" && os.Getenv(scriptWorkerEnv) != "1" {
		os.Exit(runScriptWorker())
	}
	run := testscript.TestingM(m)
	if dir := os.Getenv(scriptTempDirEnv); name != "agent-runner" && name != "agent-runner.exe" && dir != "" {
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
// directory even when the worker exits abruptly. No other run's files are swept.
// SIGKILL of the supervisor (or SIGKILL of the entire process group) cannot be
// handled; such a cancellation can still leave the private directory behind.
func runScriptWorker() int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

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
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Env = append(os.Environ(), scriptWorkerEnv+"=1", scriptTempDirEnv+"="+dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
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
			_ = cmd.Process.Signal(sig)
			if escalation == nil {
				escalation = time.NewTimer(scriptSignalGrace)
				kill = escalation.C
			}
		case <-kill:
			_ = cmd.Process.Kill()
			kill = nil
		case err := <-finished:
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
