package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
)

func handleMetricsCommand(args []string) int {
	if len(args) != 2 || args[0] != "recover" {
		fmt.Fprintln(os.Stderr, "usage: agent-runner metrics recover <run-id>")
		return 1
	}
	path, err := resolveResumeStatePath(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir := filepath.Dir(path)
	active, err := runlock.Acquire(dir)
	if err != nil || active != 0 {
		fmt.Fprintln(os.Stderr, "agent-runner: cannot recover metrics while the run is locked")
		return 1
	}
	defer runlock.Delete(dir)
	state, err := stateio.ReadState(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	collector := metrics.NewCollector(dir, state.RunID, state.WorkflowName, time.Now())
	ctx := &model.ExecutionContext{SessionDir: dir, AuditLogger: metrics.NewPipeline(collector, nil)}
	if err = iexec.RecoverValidatorMetrics(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: metrics recovery incomplete: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintln(os.Stdout, "metrics recovery complete"); err != nil {
		return 1
	}
	return 0
}
