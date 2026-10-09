package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
)

type finalizeCrashRunner struct{ fixes, statusGates, fixNeededGates int }

func (*finalizeCrashRunner) RunShell(string, bool, string) (ProcessResult, error) {
	return ProcessResult{Started: true}, nil
}
func (r *finalizeCrashRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	if strings.Contains(options.Prefix, "fix-pr") {
		r.fixes++
		if r.fixes == 1 {
			return ProcessResult{Started: true, ExitCode: 1, Stderr: "capacity"}, nil
		}
	}
	return ProcessResult{Started: true, Stdout: claudeUsageOutput("CI_PASSED", 0)}, nil
}
func (r *finalizeCrashRunner) RunScript(path string, _ []byte, _ bool, _ string) (ProcessResult, error) {
	switch filepath.Base(path) {
	case "ci-status-gate.sh":
		r.statusGates++
		if r.statusGates == 1 {
			return ProcessResult{Started: true, ExitCode: 1}, nil
		}
	case "ci-fix-needed-gate.sh":
		r.fixNeededGates++
		if r.fixNeededGates == 1 {
			return ProcessResult{Started: true, ExitCode: 1}, nil
		}
	}
	return ProcessResult{Started: true}, nil
}

func TestBuiltinFinalizePRAbsorbedCrash(t *testing.T) {
	dir := t.TempDir()
	recorder, err := audit.NewLogger(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { recorder.Close() })
	ctx := model.NewRootContext(&model.RootContextOptions{ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "claude", DefaultMode: "autonomous"}}}, SessionDir: dir, ProjectRoot: dir, WorkingDir: dir, AuditLogger: recorder})
	runner := &finalizeCrashRunner{}
	step := &model.Step{ID: "finalize", Workflow: "builtin:core/finalize-pr-v1.0.yaml", Params: map[string]string{"ci_fix_cycles": "2"}}
	outcome, err := DispatchStep(step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess {
		t.Fatalf("outcome=%q err=%v fixes=%d statusGates=%d fixNeededGates=%d", outcome, err, runner.fixes, runner.statusGates, runner.fixNeededGates)
	}
	if runner.fixes != 1 || runner.statusGates < 2 || runner.fixNeededGates != 1 {
		t.Fatalf("fixes=%d statusGates=%d fixNeededGates=%d", runner.fixes, runner.statusGates, runner.fixNeededGates)
	}
	if !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "finalize"}}) {
		t.Fatal("crash lost")
	}
	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	auditText := string(data)
	if !strings.Contains(auditText, `sub_workflow_end {"crash_observed":true`) || !strings.Contains(auditText, `iteration_end {"crash_observed":true`) || !strings.Contains(auditText, `"failure_kind":"infrastructure"`) {
		t.Fatalf("missing classified events: %s", auditText)
	}
}
