package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
)

func handleStatuslineRecord(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("statusline-record", flag.ContinueOnError)
	flags.SetOutput(stderr)
	report := flags.String("report", "", "report path")
	delegate := flags.String("delegate", "", "user status-line command")
	if flags.Parse(args) != nil || *report == "" || flags.NArg() != 0 {
		return 1
	}
	payload, err := io.ReadAll(stdin)
	if err != nil {
		return 1
	}
	recordClaudeStatusline(*report, payload)
	if *delegate == "" {
		return 0
	}
	cmd := exec.Command("sh", "-c", *delegate) // #nosec G204 -- user's configured status-line command
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	if err = cmd.Start(); err != nil {
		return 1
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case sig := <-signals:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		if sig == syscall.SIGINT {
			return 130
		}
		return 143
	case err = <-done:
		if err == nil {
			return 0
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return 1
	}
}

func recordClaudeStatusline(path string, payload []byte) {
	var input struct {
		SessionID string `json:"session_id"`
		PromptID  string `json:"prompt_id"`
		Cost      struct {
			Total *float64 `json:"total_cost_usd"`
		} `json:"cost"`
		Context struct {
			Usage json.RawMessage `json:"current_usage"`
		} `json:"context_window"`
	}
	if json.Unmarshal(payload, &input) != nil {
		return
	}
	record := cli.ClaudeStatusReport{RecordedAt: time.Now().UTC(), SessionID: input.SessionID, PromptID: input.PromptID, TotalCostUSD: input.Cost.Total, CurrentUsage: input.Context.Usage}
	raw, err := json.Marshal(record)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(raw, '\n'))
}
