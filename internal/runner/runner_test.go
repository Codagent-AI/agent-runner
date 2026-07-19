package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

type delayedRunner struct{ mockRunner }

func (r *delayedRunner) RunShell(cmd string, capture bool, workdir string) (exec.ProcessResult, error) {
	time.Sleep(10 * time.Millisecond)
	return r.mockRunner.RunShell(cmd, capture, workdir)
}

func TestRunWorkflowProducesMetricsWhenAuditLogCannotOpen(t *testing.T) {
	dir := t.TempDir()
	log := &mockLog{}
	if err := os.Mkdir(filepath.Join(dir, "audit.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s1", "echo hi")}}
	w.ApplyDefaults()
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: log})
	if err != nil || result != ResultSuccess {
		t.Fatalf("RunWorkflow = %q, %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatalf("metrics artifact missing: %v", err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("metrics artifact invalid: %v", err)
	}
	if len(artifact.Steps) != 1 || artifact.Sessions[0].Status != metrics.SessionClosed {
		t.Fatalf("artifact = %+v", artifact)
	}
	if !strings.Contains(strings.Join(log.lines, "\n"), "warning: audit trail unavailable") {
		t.Fatalf("audit logger warning missing from log: %v", log.lines)
	}
}

func TestRunWorkflowContinuesWhenAuditLogCannotOpenWithoutCustomLogger(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "audit.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s1", "echo hi")}}
	w.ApplyDefaults()

	result, err := RunWorkflow(&w, nil, &Options{
		SessionDir: dir, ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{},
	})
	if err != nil || result != ResultSuccess {
		t.Fatalf("RunWorkflow = %q, %v", result, err)
	}
}

type recordingAuditSink struct{ events []audit.Event }

func (s *recordingAuditSink) Emit(event audit.Event) { s.events = append(s.events, event) }

func TestEmitSkippedStepIncludesNestingPrefixInMetricsIdentity(t *testing.T) {
	iteration := 2
	sink := &recordingAuditSink{}
	rs := &runState{ctx: &model.ExecutionContext{
		AuditLogger: sink,
		NestingPath: []model.NestingSegment{
			{StepID: "outer", Iteration: &iteration},
			{StepID: "workflow", SubWorkflowName: "child"},
		},
	}}
	step := model.Step{ID: "duplicate", Command: "true", SkipIf: "previous_success"}

	emitSkippedStep(rs, &step, 0)

	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	identity := sink.events[1].Data[metrics.DataIdentity].(model.ExecutionIdentity)
	if identity.Prefix != "outer:2/workflow/sub:child" {
		t.Fatalf("skipped identity prefix = %q, want %q", identity.Prefix, "outer:2/workflow/sub:child")
	}
}

func TestRunWorkflowRoutesRunLifecycleAndTotalsThroughPipeline(t *testing.T) {
	dir := t.TempDir()
	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s1", "echo hi")}}
	w.ApplyDefaults()
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("RunWorkflow = %q, %v", result, err)
	}
	auditData, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	logText := string(auditData)
	if !strings.Contains(logText, " run_start ") || !strings.Contains(logText, " run_end ") || !strings.Contains(logText, `"totals":{`) || !strings.Contains(logText, `"cost_coverage":"none"`) {
		t.Fatalf("audit log missing lifecycle totals:\n%s", logText)
	}
}

func TestRunWorkflowMetricsPreserveMillisecondActiveDuration(t *testing.T) {
	dir := t.TempDir()
	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s1", "echo hi")}}
	w.ApplyDefaults()
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: &delayedRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("RunWorkflow = %q, %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Totals.ActiveDurationMS < 5 {
		t.Fatalf("active duration = %dms, want millisecond precision", artifact.Totals.ActiveDurationMS)
	}
}

func TestMetricsWriteFailureWarnsWithoutFailingRun(t *testing.T) {
	dir := t.TempDir()
	log := &mockLog{}
	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s1", "echo hi")}}
	w.ApplyDefaults()
	handle, err := PrepareRun(&w, nil, &Options{SessionDir: dir, ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, metrics.FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	result := ExecuteFromHandle(handle, &Options{})
	if result != ResultSuccess {
		t.Fatalf("result = %q, want success", result)
	}
	if !strings.Contains(strings.Join(log.lines, "\n"), "warning: metrics: write run-metrics artifact") {
		t.Fatalf("metrics warning missing from log: %v", log.lines)
	}
}

type mockRunner struct {
	calls   [][]string
	results []exec.ProcessResult
	idx     int
}

func (m *mockRunner) RunShell(cmd string, capture bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, []string{"sh", "-c", cmd})
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunAgent(args []string, _ bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, args)
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunScript(path string, stdin []byte, _ bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, []string{path, string(stdin)})
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

// lockProbeRunner is a mockRunner variant that records whether the lock file
// exists at the moment RunShell/RunAgent is invoked (i.e. while the step is
// "executing"). This proves the lock was written before the step runs,
// rather than only checking after finalizeRun has already deleted it.
type lockProbeRunner struct {
	calls    [][]string
	results  []exec.ProcessResult
	idx      int
	lockPath string
	observed *bool
}

func (m *lockProbeRunner) RunShell(cmd string, _ bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, []string{"sh", "-c", cmd})
	if _, err := os.Stat(m.lockPath); err == nil && m.observed != nil {
		*m.observed = true
	}
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *lockProbeRunner) RunAgent(args []string, _ bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, args)
	if _, err := os.Stat(m.lockPath); err == nil && m.observed != nil {
		*m.observed = true
	}
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *lockProbeRunner) RunScript(path string, stdin []byte, _ bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, []string{path, string(stdin)})
	if _, err := os.Stat(m.lockPath); err == nil && m.observed != nil {
		*m.observed = true
	}
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

type mockGlob struct{ matches []string }

func (g *mockGlob) Expand(_ string) ([]string, error) { return g.matches, nil }

type mockLog struct{ lines []string }

func (l *mockLog) Println(args ...any) { l.lines = append(l.lines, fmt.Sprint(args...)) }
func (l *mockLog) Printf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *mockLog) Errorf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func shellStep(id, cmd string) model.Step {
	return model.Step{ID: id, Command: cmd, Session: model.SessionNew}
}

func TestRunWorkflow_OptionalParamAvailableAsEmptyString(t *testing.T) {
	optional := false
	workflow := &model.Workflow{
		Name: "optional-param",
		Params: []model.Param{
			{Name: "task_file", Required: &optional, Default: ""},
		},
		Steps: []model.Step{
			shellStep("validate", `if [ -n "{{task_file}}" ]; then echo with-task; else echo no-task; fi`),
		},
	}
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}

	result, err := RunWorkflow(workflow, nil, &Options{
		WorkflowFile:  "optional.yaml",
		SessionDir:    t.TempDir(),
		ProcessRunner: runner,
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("RunWorkflow returned error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("result = %q, want success", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 shell call, got %d", len(runner.calls))
	}
	if cmd := runner.calls[0][2]; !strings.Contains(cmd, `[ -n "" ]`) {
		t.Fatalf("command did not interpolate optional empty param: %q", cmd)
	}
}

func TestRunWorkflowUntilStopsAfterNamedTopLevelStep(t *testing.T) {
	workflow := &model.Workflow{
		Name: "until",
		Steps: []model.Step{
			shellStep("A", "echo A"),
			shellStep("B", "echo B"),
			shellStep("C", "echo C"),
		},
	}
	processRunner := &mockRunner{}
	log := &mockLog{}
	sessionDir := t.TempDir()

	result, err := RunWorkflow(workflow, nil, &Options{
		Until:         "B",
		SessionDir:    sessionDir,
		ProcessRunner: processRunner,
		Log:           log,
	})
	if err != nil {
		t.Fatalf("RunWorkflow returned error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("result = %q, want success", result)
	}
	if got, want := len(processRunner.calls), 2; got != want {
		t.Fatalf("shell calls = %d, want %d: %v", got, want, processRunner.calls)
	}
	if got, want := processRunner.calls[0][2], "echo A"; got != want {
		t.Fatalf("first command = %q, want %q", got, want)
	}
	if got, want := processRunner.calls[1][2], "echo B"; got != want {
		t.Fatalf("second command = %q, want %q", got, want)
	}
	if got := strings.Join(log.lines, "\n"); !strings.Contains(got, `stopped after step "B" (--until).`) {
		t.Fatalf("log missing --until stop message: %q", got)
	}

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.Completed {
		t.Fatal("capped run state should remain resumable")
	}
	if state.CurrentStep.Nested == nil || state.CurrentStep.Nested.StepID != "B" || !state.CurrentStep.Nested.Completed {
		t.Fatalf("current step = %#v, want completed step B", state.CurrentStep.Nested)
	}
}

func TestRunWorkflowUntilFinalStepMarksRunCompleted(t *testing.T) {
	workflow := &model.Workflow{
		Name: "until-final",
		Steps: []model.Step{
			shellStep("A", "echo A"),
			shellStep("B", "echo B"),
		},
	}
	sessionDir := t.TempDir()

	result, err := RunWorkflow(workflow, nil, &Options{
		Until:         "B",
		SessionDir:    sessionDir,
		ProcessRunner: &mockRunner{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("RunWorkflow returned error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("result = %q, want success", result)
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !state.Completed {
		t.Fatal("run capped at its final step should be completed")
	}
}

func TestRunWorkflowUntilRejectsUnknownTopLevelStepBeforeRunning(t *testing.T) {
	workflow := &model.Workflow{
		Name: "until",
		Steps: []model.Step{
			shellStep("A", "echo A"),
			shellStep("B", "echo B"),
		},
	}
	processRunner := &mockRunner{}
	sessionDir := t.TempDir()

	result, err := RunWorkflow(workflow, nil, &Options{
		Until:         "missing",
		SessionDir:    sessionDir,
		ProcessRunner: processRunner,
		Log:           &mockLog{},
	})
	if err == nil {
		t.Fatal("RunWorkflow returned nil error, want unknown --until step error")
	}
	if result != ResultFailed {
		t.Fatalf("result = %q, want failed", result)
	}
	if got := err.Error(); !strings.Contains(got, `--until step "missing" not found in top-level workflow steps`) {
		t.Fatalf("error = %q, want clear unknown --until step error", got)
	}
	if len(processRunner.calls) != 0 {
		t.Fatalf("steps ran before --until validation: %v", processRunner.calls)
	}
	for _, name := range []string{"state.json", "audit.log", "lock"} {
		if _, statErr := os.Stat(filepath.Join(sessionDir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("%s created before --until validation; stat error = %v", name, statErr)
		}
	}
}

func TestRunWorkflowUntilStopsWhenNamedTopLevelStepIsSkipped(t *testing.T) {
	workflow := &model.Workflow{
		Name: "until-skipped",
		Steps: []model.Step{
			shellStep("A", "echo A"),
			{ID: "B", Command: "echo B", Session: model.SessionNew, SkipIf: "previous_success"},
			shellStep("C", "echo C"),
		},
	}
	processRunner := &mockRunner{}
	sessionDir := t.TempDir()

	result, err := RunWorkflow(workflow, nil, &Options{
		Until:         "B",
		SessionDir:    sessionDir,
		ProcessRunner: processRunner,
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("RunWorkflow returned error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("result = %q, want success", result)
	}
	if got, want := len(processRunner.calls), 1; got != want {
		t.Fatalf("shell calls = %d, want %d (A ran; B skipped; C capped): %v", got, want, processRunner.calls)
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.CurrentStep.Nested == nil || state.CurrentStep.Nested.StepID != "B" || !state.CurrentStep.Nested.Completed {
		t.Fatalf("current step = %#v, want reached skipped step B", state.CurrentStep.Nested)
	}
}

func TestMaterializeBundledAssetsCreatesMarkerForNamespaceWithoutAssets(t *testing.T) {
	sessionDir := t.TempDir()

	if err := materializeBundledAssets(sessionDir, "builtin:openspec/change.yaml"); err != nil {
		t.Fatalf("materialize bundled assets: %v", err)
	}

	marker := filepath.Join(sessionDir, "bundled", "openspec", ".complete")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("stat marker: %v", err)
	}
}

func TestMaterializeBundledAssetsPutsDebugPromptAtPromptPath(t *testing.T) {
	sessionDir := t.TempDir()

	if err := materializeBundledAssets(sessionDir, "builtin:core/debug.yaml"); err != nil {
		t.Fatalf("materialize bundled assets: %v", err)
	}

	prompt := filepath.Join(sessionDir, "bundled", "core", "debug", "prompt.md")
	data, err := os.ReadFile(prompt)
	if err != nil {
		t.Fatalf("read debug prompt at prompt path: %v", err)
	}
	if !strings.Contains(string(data), "# Debug Workflow Prompt") {
		t.Fatalf("debug prompt missing title, got %q", string(data))
	}
}

func TestPrepareRunPopulatesAutonomousBackendFromUserSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsPath := filepath.Join(home, ".agent-runner", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("autonomous_backend: interactive-claude\n"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s", "echo hi")}}
	h, err := PrepareRun(&w, nil, &Options{
		ProcessRunner: &mockRunner{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() returned error: %v", err)
	}
	defer finalizeRun(h.rs, ResultSuccess)

	if got := h.rs.ctx.AutonomousBackend; got != "interactive-claude" {
		t.Fatalf("ctx.AutonomousBackend = %q, want interactive-claude", got)
	}
}

func TestPrepareRunPopulatesAutonomousPermissionModeFromUserSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsPath := filepath.Join(home, ".agent-runner", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("autonomous_permission_mode: yolo\n"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	w := model.Workflow{Name: "test", Steps: []model.Step{shellStep("s", "echo hi")}}
	h, err := PrepareRun(&w, nil, &Options{
		ProcessRunner: &mockRunner{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() returned error: %v", err)
	}
	defer finalizeRun(h.rs, ResultSuccess)

	if got := h.rs.ctx.AutonomousPermissionMode; got != "yolo" {
		t.Fatalf("ctx.AutonomousPermissionMode = %q, want yolo", got)
	}
}

func TestRunWorkflow(t *testing.T) {
	t.Run("runs single step workflow", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo hi")},
		}
		w.ApplyDefaults()
		result, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
	})

	t.Run("runs multi-step workflow in order", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo first"),
				shellStep("s2", "echo second"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(runner.calls))
		}
	})

	t.Run("stops on failure", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "false"),
				shellStep("s2", "echo never"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultFailed {
			t.Fatalf("expected failed, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (stopped), got %d", len(runner.calls))
		}
	})

	t.Run("continues on failure with continue_on_failure", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}, {ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "s1", Command: "false", Session: model.SessionNew, ContinueOnFailure: true},
				shellStep("s2", "echo yes"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(runner.calls))
		}
	})

	t.Run("counted loop exhaustion succeeds workflow", func(t *testing.T) {
		maxIterations := 1
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{
					ID:      "retry",
					Session: model.SessionNew,
					Loop:    &model.Loop{Max: &maxIterations},
					Steps: []model.Step{
						{ID: "always-pass", Command: "true", Session: model.SessionNew},
					},
				},
				shellStep("reached", "echo reached"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success after loop exhaustion, got %q", result)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected loop body and following step to run, got %d calls", len(runner.calls))
		}
	})

	t.Run("counted retry loop exhaustion fails workflow", func(t *testing.T) {
		maxIterations := 1
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{
					ID:      "retry",
					Session: model.SessionNew,
					Loop:    &model.Loop{Max: &maxIterations},
					Steps: []model.Step{
						{
							ID: "always-fail", Command: "false", Session: model.SessionNew,
							ContinueOnFailure: true,
							BreakIf:           "success",
						},
					},
				},
				shellStep("never-reached", "echo never"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultFailed {
			t.Fatalf("expected failed after retry loop exhaustion, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected only the loop body to run, got %d calls", len(runner.calls))
		}
	})

	t.Run("skip_if previous_success skips step", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo ok"),
				{ID: "s2", Command: "echo skip me", Session: model.SessionNew, SkipIf: "previous_success"},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (second skipped), got %d", len(runner.calls))
		}
	})

	t.Run("skip_if sh: skips step when command exits 0", func(t *testing.T) {
		runner := &mockRunner{}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo ok"),
				{ID: "s2", Command: "echo skipped-body", Session: model.SessionNew, SkipIf: "sh: true"},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		// The skip_if shell eval bypasses the mock runner, so only s1 is seen.
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (s1 only; s2 skipped), got %d: %v", len(runner.calls), runner.calls)
		}
	})

	t.Run("skip_if sh: runs step when command exits non-zero", func(t *testing.T) {
		runner := &mockRunner{}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo ok"),
				{ID: "s2", Command: "echo s2-ran", Session: model.SessionNew, SkipIf: "sh: false"},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls (s1 + s2), got %d: %v", len(runner.calls), runner.calls)
		}
	})

	t.Run("skip_if sh: interpolates params", func(t *testing.T) {
		// Write a side-effect file iff the interpolated command actually ran,
		// proving that {{flag}} was expanded before shell execution.
		runner := &mockRunner{}
		sentinel := filepath.Join(t.TempDir(), "sentinel")
		w := model.Workflow{
			Name:   "test",
			Params: []model.Param{{Name: "flag"}},
			Steps: []model.Step{
				shellStep("s1", "echo ok"),
				{
					ID: "s2", Command: "echo body", Session: model.SessionNew,
					SkipIf: fmt.Sprintf(`sh: touch %q && test {{flag}} = true`, sentinel),
				},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{"flag": "true"}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("expected skip_if shell to have run (sentinel missing): %v", err)
		}
		// flag="true" → test exits 0 → s2 skipped.
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (s2 skipped after param expansion), got %d: %v", len(runner.calls), runner.calls)
		}
	})

	t.Run("skip_if sh: does not double-quote when template omits quotes", func(t *testing.T) {
		runner := &mockRunner{}
		w := model.Workflow{
			Name:   "test",
			Params: []model.Param{{Name: "flag"}},
			Steps: []model.Step{
				shellStep("s1", "echo ok"),
				{
					ID: "s2", Command: "echo should-run", Session: model.SessionNew,
					SkipIf: "sh: test {{flag}} != true",
				},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{"flag": "true"}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		// flag="true" and compare != "true" → test exits 1 → s2 NOT skipped.
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls (s2 not skipped), got %d: %v", len(runner.calls), runner.calls)
		}
	})

	t.Run("validates required params", func(t *testing.T) {
		w := model.Workflow{
			Name:   "test",
			Params: []model.Param{{Name: "required_param"}},
			Steps:  []model.Step{shellStep("s1", "echo")},
		}
		w.ApplyDefaults()
		_, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: &mockRunner{},
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for missing required param")
		}
	})

	t.Run("applies default param values", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:   "test",
			Params: []model.Param{{Name: "env", Default: "dev"}},
			Steps:  []model.Step{shellStep("s1", "echo {{env}}")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if runner.calls[0][2] != "echo 'dev'" {
			t.Fatalf("expected %q, got %q", "echo 'dev'", runner.calls[0][2])
		}
	})

	t.Run("resumes from specified step", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo skip"),
				shellStep("s2", "echo run"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			From:          "s2",
			SessionDir:    t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (s1 skipped), got %d", len(runner.calls))
		}
		if runner.calls[0][2] != "echo run" {
			t.Fatalf("expected 'echo run', got %q", runner.calls[0][2])
		}
	})

	t.Run("errors for unknown from step", func(t *testing.T) {
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo")},
		}
		w.ApplyDefaults()
		_, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: &mockRunner{},
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			From:          "nonexistent",
			SessionDir:    t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for unknown step")
		}
	})

	t.Run("writes state.json and audit.log into session directory", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "false")},
		}
		w.ApplyDefaults()
		RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})

		// State file should be state.json (not agent-runner-state.json).
		stateFile := filepath.Join(sessionDir, "state.json")
		if _, err := os.Stat(stateFile); os.IsNotExist(err) {
			t.Fatal("expected state.json in session directory")
		}

		// Audit log should exist in the same directory.
		auditFile := filepath.Join(sessionDir, "audit.log")
		if _, err := os.Stat(auditFile); os.IsNotExist(err) {
			t.Fatal("expected audit.log in session directory")
		}
	})

	t.Run("marks state.json completed on success", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo ok")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}

		stateFile := filepath.Join(sessionDir, "state.json")
		state, err := stateio.ReadState(stateFile)
		if err != nil {
			t.Fatalf("expected state.json to be preserved on success: %v", err)
		}
		if !state.Completed {
			t.Fatal("expected state.Completed to be true after successful run")
		}
	})

	t.Run("prints resume hint with session ID on failure", func(t *testing.T) {
		sessionDir := filepath.Join(t.TempDir(), "deploy-service-test")
		log := &mockLog{}
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name:  "deploy-service",
			Steps: []model.Step{shellStep("s1", "false")},
		}
		w.ApplyDefaults()
		RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           log,
			SessionDir:    sessionDir,
		})

		found := false
		for _, line := range log.lines {
			if contains(line, "--resume deploy-service-") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected resume hint with session ID, got log lines: %v", log.lines)
		}
	})

	t.Run("creates lock file during run and deletes it on success", func(t *testing.T) {
		sessionDir := t.TempDir()
		lockFile := filepath.Join(sessionDir, "lock")
		lockSeenDuringStep := false
		runner := &lockProbeRunner{
			results:  []exec.ProcessResult{{ExitCode: 0}},
			lockPath: lockFile,
			observed: &lockSeenDuringStep,
		}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo ok")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if !lockSeenDuringStep {
			t.Fatal("expected lock file to exist while step was executing")
		}

		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Fatal("expected lock file to be deleted after successful run")
		}
	})

	t.Run("deletes lock file on failure", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "false")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if result != ResultFailed {
			t.Fatalf("expected failed, got %q", result)
		}

		lockFile := filepath.Join(sessionDir, "lock")
		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Fatal("expected lock file to be deleted after failed run")
		}
	})

	t.Run("refuses to run when session dir has an active lock", func(t *testing.T) {
		sessionDir := t.TempDir()
		// Simulate an already-running runner by writing a lock file whose PID
		// is this test process (guaranteed alive for the test duration).
		lockFile := filepath.Join(sessionDir, "lock")
		if err := os.WriteFile(lockFile, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o600); err != nil {
			t.Fatalf("failed to seed lock: %v", err)
		}

		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo hi")},
		}
		w.ApplyDefaults()
		result, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if err == nil {
			t.Fatal("expected error for active lock, got nil")
		}
		if !strings.Contains(err.Error(), "already in progress") {
			t.Fatalf("expected 'already in progress' in error, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%d", os.Getpid())) {
			t.Fatalf("expected PID %d in error, got %q", os.Getpid(), err.Error())
		}
		if result != ResultFailed {
			t.Fatalf("expected ResultFailed, got %q", result)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("expected no steps to run, got calls: %v", runner.calls)
		}
		// Pre-existing lock must be untouched (still contains this PID).
		data, _ := os.ReadFile(lockFile)
		if !strings.Contains(string(data), fmt.Sprintf("%d", os.Getpid())) {
			t.Fatalf("expected lock file preserved, got %q", string(data))
		}
	})

	t.Run("proceeds when existing lock is stale", func(t *testing.T) {
		sessionDir := t.TempDir()
		lockFile := filepath.Join(sessionDir, "lock")
		// PID 999999999 is essentially guaranteed to be dead.
		if err := os.WriteFile(lockFile, []byte("999999999\n"), 0o600); err != nil {
			t.Fatalf("failed to seed stale lock: %v", err)
		}

		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo hi")},
		}
		w.ApplyDefaults()
		result, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
	})
}

func TestWriteMetaJSON(t *testing.T) {
	t.Run("creates meta.json with project path", func(t *testing.T) {
		dir := t.TempDir()
		writeMetaJSON(dir, "/home/user/myproject")

		data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			t.Fatalf("failed to read meta.json: %v", err)
		}
		if !contains(string(data), "/home/user/myproject") {
			t.Fatalf("expected path in meta.json, got %s", data)
		}
	})

	t.Run("does not overwrite existing meta.json", func(t *testing.T) {
		dir := t.TempDir()
		existing := `{"path":"/original/path"}`
		os.WriteFile(filepath.Join(dir, "meta.json"), []byte(existing), 0o600)

		writeMetaJSON(dir, "/new/path")

		data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			t.Fatalf("failed to read meta.json: %v", err)
		}
		if string(data) != existing {
			t.Fatalf("meta.json was overwritten: got %s", data)
		}
	})
}

func TestPrepareRun_SeedsInitialState(t *testing.T) {
	w := model.Workflow{
		Name:  "my-workflow",
		Steps: []model.Step{shellStep("s1", "echo hi")},
	}
	w.ApplyDefaults()

	sessionDir := t.TempDir()
	h, err := PrepareRun(&w, map[string]string{"k": "v"}, &Options{
		WorkflowFile:  ".agent-runner/workflows/my-workflow.yaml",
		ProcessRunner: &mockRunner{},
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
		SessionDir:    sessionDir,
	})
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}
	defer finalizeRun(h.rs, ResultSuccess)

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("state.json missing after PrepareRun: %v", err)
	}
	if state.WorkflowFile != ".agent-runner/workflows/my-workflow.yaml" {
		t.Errorf("WorkflowFile = %q, want %q", state.WorkflowFile, ".agent-runner/workflows/my-workflow.yaml")
	}
	if state.WorkflowName != "my-workflow" {
		t.Errorf("WorkflowName = %q, want %q", state.WorkflowName, "my-workflow")
	}
	if state.Params["k"] != "v" {
		t.Errorf("Params = %v, want k=v", state.Params)
	}
	if state.CurrentStep.Nested == nil {
		t.Fatal("CurrentStep should be seeded with the first step")
	}
	if state.CurrentStep.Nested.StepID != "s1" {
		t.Fatalf("CurrentStep.StepID = %q, want s1", state.CurrentStep.Nested.StepID)
	}
}

func TestPrepareRun_SeedsResumeState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w := model.Workflow{
		Name: "my-workflow",
		Steps: []model.Step{
			shellStep("before", "echo before"),
			{ID: "validator", Workflow: "validator.yaml"},
		},
	}
	w.ApplyDefaults()

	sessionDir := t.TempDir()
	child := &model.NestedStepState{
		StepID:            "summary-ui",
		SessionIDs:        map[string]string{"agent": "session-1"},
		CapturedVariables: map[string]model.CapturedValue{"answer": model.NewCapturedString("continue")},
		Completed:         false,
	}
	h, err := PrepareRun(&w, map[string]string{"k": "v"}, &Options{
		WorkflowFile:      ".agent-runner/workflows/my-workflow.yaml",
		From:              "validator",
		SessionIDs:        map[string]string{"root-agent": "root-session"},
		CapturedVariables: map[string]model.CapturedValue{"root": model.NewCapturedString("value")},
		LastSessionStepID: "root-step",
		NamedSessions:     map[string]string{"validator-setup": "session-2"},
		NamedSessionDecls: map[string]string{"validator-setup": "planner"},
		ChildState:        child,
		ProcessRunner:     &mockRunner{},
		GlobExpander:      &mockGlob{},
		Log:               &mockLog{},
		SessionDir:        sessionDir,
	})
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}
	defer finalizeRun(h.rs, ResultSuccess)

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("state.json missing after PrepareRun: %v", err)
	}
	if state.CurrentStep.Nested == nil {
		t.Fatal("CurrentStep should be nested")
	}
	if got := state.CurrentStep.Nested.StepID; got != "validator" {
		t.Fatalf("CurrentStep.StepID = %q, want validator", got)
	}
	if got := state.CurrentStep.Nested.Child; got == nil || got.StepID != "summary-ui" {
		t.Fatalf("CurrentStep.Child = %#v, want summary-ui", got)
	}
	if got := state.CurrentStep.Nested.SessionIDs["root-agent"]; got != "root-session" {
		t.Fatalf("CurrentStep.SessionIDs[root-agent] = %q, want root-session", got)
	}
	if got := state.CurrentStep.Nested.NamedSessions["validator-setup"]; got != "session-2" {
		t.Fatalf("CurrentStep.NamedSessions[validator-setup] = %q, want session-2", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
