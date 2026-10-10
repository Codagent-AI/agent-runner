package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStatuslineRecorderDelegate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report")
	payload := `{"session_id":"session","prompt_id":"prompt","cost":{"total_cost_usd":0.42},"context_window":{"current_usage":{"input_tokens":1}},"transcript_path":"secret"}`
	var out, errout bytes.Buffer
	code := handleStatuslineRecord([]string{"--report", path, "--delegate", "cat; exit 3"}, strings.NewReader(payload), &out, &errout)
	if code != 3 || out.String() != payload {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errout.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if len(record) != 5 || record["session_id"] != "session" || record["prompt_id"] != "prompt" || record["total_cost_usd"] != 0.42 {
		t.Fatalf("record: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v", info.Mode())
	}
}

func TestStatuslineRecorderBinaryINT002(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), bin)
	payload := `{"session_id":"session","prompt_id":"prompt","cost":{"total_cost_usd":0.42},"context_window":{"current_usage":{"input_tokens":1}},"transcript_path":"secret","rate_limits":{}}`
	path := filepath.Join(root, "report")
	for _, delegate := range []string{"", `printf '%s:' "$COLUMNS"; cat; exit 3`} {
		args := []string{"internal", "statusline-record", "--report", path}
		if delegate != "" {
			args = append(args, "--delegate", delegate)
		}
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "COLUMNS=99", "LINES=40")
		cmd.Stdin = strings.NewReader(payload)
		out, err := cmd.Output()
		if delegate == "" {
			if err != nil || len(out) != 0 {
				t.Fatalf("%s %v", out, err)
			}
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 3 || string(out) != "99:"+payload {
				t.Fatalf("%s %v", out, err)
			}
		}
	}
	pidfile := filepath.Join(root, "delegate.pid")
	cmd := exec.Command(bin, "internal", "statusline-record", "--report", path, "--delegate", "echo $$ > '"+pidfile+"'; sleep 30")
	cmd.Stdin = strings.NewReader(payload)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		raw, _ = os.ReadFile(pidfile)
		if len(raw) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(raw) == 0 {
		t.Fatal("delegate never started")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("delegate still running")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 3 {
		t.Fatalf("report: %s", data)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "rate_limits") {
		t.Fatalf("unexpected persisted fields: %s", data)
	}
}

// The escaped child keeps both inherited pipes open after the delegate group is
// killed. The recorder must still drain with a deadline and return its signal code.
func TestStatuslineSignalWithEscapedPipes(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "escaped.pid")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestStatuslinePipeHelper$")
	cmd.Env = append(os.Environ(), "STATUSLINE_PIPE_HELPER=recorder", "STATUSLINE_PIPE_PID="+pidfile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	deadline := time.Now().Add(3 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		raw, _ = os.ReadFile(pidfile)
		if len(raw) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("escaped child never started: %q %v", raw, err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 143 {
			t.Fatalf("signal exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("recorder hung draining escaped child's pipes")
	}
}

func TestStatuslinePipeHelper(t *testing.T) {
	switch os.Getenv("STATUSLINE_PIPE_HELPER") {
	case "recorder":
		var out, errout bytes.Buffer
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		delegate := "exec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestStatuslinePipeHelper$"
		_ = os.Setenv("STATUSLINE_PIPE_HELPER", "delegate")
		os.Exit(handleStatuslineRecord([]string{"--report", os.Getenv("STATUSLINE_PIPE_PID") + ".report", "--delegate", delegate}, strings.NewReader("{}"), &out, &errout))
	case "delegate":
		child := exec.Command("sleep", "30")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("STATUSLINE_PIPE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = child.Wait()
	}
}
