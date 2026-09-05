package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestExecuteFromHandleCallsPostFinalizationHookAfterDurableTerminalEvidence(t *testing.T) {
	workflow := &model.Workflow{
		Name:  "terminal-hook",
		Steps: []model.Step{{ID: "done", Command: "true"}},
	}

	var got PostFinalizationSummary
	hook := func(summary PostFinalizationSummary) error {
		got = summary
		if _, err := runlock.ProveHeld(summary.SessionDir); err != nil {
			t.Fatalf("source lock not held while post-finalization hook runs: %v", err)
		}
		state, err := stateio.ReadState(filepath.Join(summary.SessionDir, "state.json"))
		if err != nil {
			t.Fatalf("read final state: %v", err)
		}
		if !state.Completed {
			t.Fatal("state was not completed before post-finalization hook")
		}
		auditLog, err := os.ReadFile(filepath.Join(summary.SessionDir, "audit.log"))
		if err != nil {
			t.Fatalf("read final audit log: %v", err)
		}
		if !strings.Contains(string(auditLog), "run_end") {
			t.Fatalf("terminal audit evidence was not flushed before hook: %s", auditLog)
		}
		return nil
	}

	handle, err := PrepareRun(workflow, nil, &Options{
		SessionDir:           t.TempDir(),
		ProcessRunner:        &mockRunner{},
		Log:                  &DiscardLogger{},
		PostFinalizationHook: hook,
	})
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}
	if result := ExecuteFromHandle(handle, nil); result != ResultSuccess {
		t.Fatalf("ExecuteFromHandle() = %q, want success", result)
	}
	if got.SessionDir != handle.SessionDir || got.Result != ResultSuccess {
		t.Fatalf("hook summary = %#v", got)
	}
}
