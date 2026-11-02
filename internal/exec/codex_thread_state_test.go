package exec

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/google/go-cmp/cmp"
)

func TestCodexThreadStateSharedAcrossInlineRepair(t *testing.T) {
	sourceHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", sourceHome)
	t.Setenv("CODEX_SQLITE_HOME", "")
	t.Setenv("AGENT_RUNNER_EXECUTABLE", writeExecutableFixture(t))
	ctx := makeCtx()
	ctx.SessionDir = t.TempDir()
	ctx.ProjectRoot = t.TempDir()
	ctx.WorkingDir = ctx.ProjectRoot
	if pid, err := runlock.Acquire(ctx.SessionDir); err != nil || pid != 0 {
		t.Fatalf("acquire run lock: pid=%d err=%v", pid, err)
	}
	t.Cleanup(func() {
		if server, ok := ctx.Control.(*control.ControlServer); ok {
			_ = server.Close()
		}
		runlock.Delete(ctx.SessionDir)
	})
	ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{
		"implementor": {CLI: "codex", DefaultMode: "autonomous"},
	}}
	output := `{"type":"thread.started","thread_id":"test-thread"}` + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":1}}` + "\n"
	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 0, Stdout: output}, // step A
		{ExitCode: 1, Stderr: "needs repair"},
		{ExitCode: 0, Stdout: output}, // repair resumes A without call_agent
		{ExitCode: 0},                 // passing check
		{ExitCode: 0, Stdout: output}, // step B resumes after repair
	}}
	maxAttempts := 1
	steps := []model.Step{
		{ID: "a", Agent: "implementor", Session: model.SessionNew, Mode: model.ModeAutonomous, Prompt: "start", Tools: model.RunnerTools{model.RunnerToolCallAgent}},
		{ID: "verify", Command: "verify", Repair: &model.Repair{Prompt: "fix", Session: model.SessionResume, Max: &maxAttempts}},
		{ID: "b", Session: model.SessionResume, Mode: model.ModeAutonomous, Prompt: "continue", Tools: model.RunnerTools{model.RunnerToolCallAgent}},
	}
	for i := range steps {
		var outcome StepOutcome
		var err error
		if steps[i].Command != "" {
			outcome, err = ExecuteCheckStep(&steps[i], ctx, runner, &mockLogger{})
		} else {
			outcome, err = ExecuteAgentStep(&steps[i], ctx, runner, &mockLogger{})
		}
		if err != nil || outcome != OutcomeSuccess {
			t.Fatalf("step %s = %s, %v", steps[i].ID, outcome, err)
		}
	}
	if len(runner.agentOptions) != 3 {
		t.Fatalf("Codex invocations = %d, want 3", len(runner.agentOptions))
	}
	for i, options := range runner.agentOptions {
		if options.Args[0] != "codex" {
			t.Fatalf("invocation %d: %v", i, options.Args)
		}
		if i > 0 && (!slices.Contains(options.Args, "resume") || !slices.Contains(options.Args, "test-thread")) {
			t.Errorf("invocation %d did not resume A: %v", i, options.Args)
		}
		home, sqliteHome := sourceHome, ""
		for _, entry := range options.Env {
			if v, ok := strings.CutPrefix(entry, "CODEX_HOME="); ok {
				home = v
			}
			if v, ok := strings.CutPrefix(entry, "CODEX_SQLITE_HOME="); ok {
				sqliteHome = v
			}
		}
		if sqliteHome == "" {
			sqliteHome = home
		}
		if diff := cmp.Diff(sourceHome, sqliteHome); diff != "" {
			t.Errorf("invocation %d SQLite home (-want +got):\n%s", i, diff)
		}
		index, err := filepath.EvalSymlinks(filepath.Join(home, "session_index.jsonl"))
		if err != nil {
			t.Errorf("invocation %d session index: %v", i, err)
			continue
		}
		wantIndex, err := filepath.EvalSymlinks(filepath.Join(sourceHome, "session_index.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(wantIndex, index); diff != "" {
			t.Errorf("invocation %d session index (-want +got):\n%s", i, diff)
		}
	}
}
