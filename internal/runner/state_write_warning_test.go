package runner

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestPersistPreviousStepReportsStateWriteFailure(t *testing.T) {
	dir := t.TempDir() // No state.json, so updateState must fail.
	log := &mockLog{}
	rs := &runState{sessionDir: dir, ctx: model.NewRootContext(&model.RootContextOptions{SessionDir: dir}), log: log}

	persistPreviousStep(rs)

	if !strings.Contains(strings.Join(log.lines, ""), "warning: could not persist previous-step state:") {
		t.Fatalf("missing state-write warning in log: %q", log.lines)
	}
}

func TestFailedRunReportsClassificationStateWriteFailure(t *testing.T) {
	dir := t.TempDir() // No state.json, so updateState must fail.
	log := &mockLog{}
	rs := &runState{sessionDir: dir, ctx: model.NewRootContext(&model.RootContextOptions{SessionDir: dir}), log: log}

	finalizeRun(rs, ResultFailed)

	if !strings.Contains(strings.Join(log.lines, ""), "warning: could not persist failed-run state:") {
		t.Fatalf("missing failed-run state-write warning in log: %q", log.lines)
	}
}
