package exec

import (
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
)

const verifyChangeRef = "builtin:core/verify-change-v1.0.yaml"

// acceptanceRoundRunner stands in for the agents and external tools of the
// verify-change acceptance rounds. The tester reports readiness in the rounds
// that readyInRound selects; the lead's fix commits a change. The convergence
// gate and plain shell commands run for real against a temporary repository.
type acceptanceRoundRunner struct {
	t            *testing.T
	repo         string
	evidenceDir  string
	sessionDir   string
	readyInRound func(round int) bool
	round        int
	events       []string
	// breakStatusOnce deletes the acceptance status before the first
	// verify-acceptance-handoff check so its rerun repair must restore it.
	breakStatusOnce bool
	// validatorFails makes every validator run fail, so its repairs are
	// exhausted and the validator workflow ends with a warning.
	validatorFails bool
	// fixMode is how the lead's fix ends: "" commits a change, "none" changes
	// nothing, and "retest" changes nothing but asks for another round.
	fixMode string
}

func (r *acceptanceRoundRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	switch {
	case strings.HasPrefix(cmd, "rm -f"):
		r.events = append(r.events, "reset-round-evidence")
	case strings.Contains(cmd, "acceptance round limit reached"):
		r.events = append(r.events, "end-final-round")
	case strings.Contains(cmd, "ending the acceptance rounds"):
		r.events = append(r.events, "check-acceptance-progress")
	case strings.Contains(cmd, "acceptance-handoff.md"):
		r.events = append(r.events, "verify-acceptance-handoff")
		if r.breakStatusOnce {
			r.breakStatusOnce = false
			if err := os.Remove(filepath.Join(r.evidenceDir, "acceptance-preparation-status.txt")); err != nil {
				r.t.Fatalf("remove status: %v", err)
			}
		}
	default:
		r.events = append(r.events, "shell")
	}
	return r.run(osexec.Command("sh", "-c", cmd), nil)
}

func (r *acceptanceRoundRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	name := filepath.Base(path)
	r.events = append(r.events, name)
	switch name {
	case "acceptance-gate.sh", "check-task-compliance.sh":
		return r.run(osexec.Command("sh", path), stdin)
	case "run-validator.sh":
		return r.validate(stdin), nil
	case "acceptance-push.sh", "check-draft-pr.sh":
		return ProcessResult{Started: true, ExitCode: 0}, nil
	default:
		r.t.Fatalf("unexpected script %s", name)
		return ProcessResult{}, nil
	}
}

func (r *acceptanceRoundRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	switch {
	case strings.Contains(options.Prefix, "acceptance-test"):
		scratchDir := filepath.Join(r.sessionDir, "scratch", "acceptance-test")
		if info, err := os.Stat(scratchDir); err != nil || !info.IsDir() {
			r.t.Fatalf("acceptance scratch directory %q missing when tester runs: %v", scratchDir, err)
		}
		if !strings.Contains(strings.Join(options.Args, "\n"), scratchDir) {
			r.t.Errorf("acceptance prompt does not contain interpolated scratch directory %q", scratchDir)
		}
		r.round++
		r.events = append(r.events, "acceptance-test")
		r.test()
	case strings.Contains(options.Prefix, "acceptance-fix"):
		r.events = append(r.events, "acceptance-fix")
		r.fix()
	case r.validatorFails && strings.Contains(options.Prefix, "fix-violations"):
		r.events = append(r.events, "fix-violations")
	default:
		r.t.Fatalf("unexpected agent step %s", options.Prefix)
	}
	return ProcessResult{Started: true, ExitCode: 0, Stdout: claudeUsageOutput("done", 0)}, nil
}

// validate records the validator result in the requested result file, as
// run-validator.sh does, and fails when validatorFails is set.
func (r *acceptanceRoundRunner) validate(stdin []byte) ProcessResult {
	var inputs struct {
		ResultFile string `json:"result_file"`
	}
	if err := json.Unmarshal(stdin, &inputs); err != nil {
		r.t.Fatalf("run-validator inputs %q: %v", stdin, err)
	}
	result, exitCode := "PASS\n", 0
	if r.validatorFails {
		result, exitCode = "FAIL\ncheck: tests failed\n", 1
	}
	if inputs.ResultFile != "" {
		if err := os.WriteFile(inputs.ResultFile, []byte(result), 0o600); err != nil {
			r.t.Fatalf("write validator result: %v", err)
		}
	}
	return ProcessResult{Started: true, ExitCode: exitCode}
}

func (r *acceptanceRoundRunner) run(cmd *osexec.Cmd, stdin []byte) (ProcessResult, error) {
	return runProcess(cmd, r.repo, stdin)
}

// runProcess runs cmd in dir and reports its untrimmed output and exit code.
func runProcess(cmd *osexec.Cmd, dir string, stdin []byte) (ProcessResult, error) {
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*osexec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		return ProcessResult{}, err
	}
	return ProcessResult{Started: true, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (r *acceptanceRoundRunner) head() string {
	out, err := osexec.Command("git", "-C", r.repo, "rev-parse", "HEAD").Output()
	if err != nil {
		r.t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func (r *acceptanceRoundRunner) write(name, content string) {
	if err := os.WriteFile(filepath.Join(r.evidenceDir, name), []byte(content), 0o600); err != nil {
		r.t.Fatalf("write %s: %v", name, err)
	}
}

func (r *acceptanceRoundRunner) test() {
	head := r.head()
	r.write("exploration-log.md", "explored "+head+"\n")
	r.write("acceptance-tested-revision.txt", head+"\n")
	if r.readyInRound(r.round) {
		r.write("acceptance-handoff.md", "ready "+head+"\n")
		r.write("acceptance-round-status.txt", "READY "+head+"\n")
		return
	}
	r.write("acceptance-findings.md", "defect found at "+head+"\n")
	r.write("acceptance-round-status.txt", "NOT_READY\n")
}

func (r *acceptanceRoundRunner) fix() {
	switch r.fixMode {
	case "none":
		return
	case "retest":
		r.write("acceptance-retest-reason.txt", "repaired the test server\n")
		return
	}
	if err := os.WriteFile(filepath.Join(r.repo, "file.txt"), []byte(r.head()+"\n"), 0o600); err != nil {
		r.t.Fatalf("write fix: %v", err)
	}
	gitIn(r.t, r.repo, "commit", "-q", "-am", "fix")
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := osexec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestBuiltinVerifyChangeLoadsStandalone(t *testing.T) {
	workflow, err := loader.LoadWorkflow(verifyChangeRef, loader.Options{})
	if err != nil {
		t.Fatalf("LoadWorkflow(%s): %v", verifyChangeRef, err)
	}
	if len(workflow.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want lead-agent and acceptance-tester", workflow.Sessions)
	}
}

// runVerifyChangeAcceptance executes verify-change from prepare-acceptance to
// the end with the given acceptance rounds and returns the observed events and
// the final acceptance status.
func runVerifyChangeAcceptance(t *testing.T, rounds, skipValidator, stopUnchanged, fixMode string, readyInRound func(int) bool, breakStatusOnce, validatorFails bool) (events []string, status, evidenceDir string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	// Keep the process fake independent of any installed Validator CLI.
	t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", filepath.Join(t.TempDir(), "missing-validator"))

	workflow, err := loader.LoadWorkflow(verifyChangeRef, loader.Options{})
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	start := -1
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == "prepare-acceptance" {
			start = i
		}
	}
	if start < 0 {
		t.Fatal("verify-change has no prepare-acceptance step")
	}

	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "feature")
	gitIn(t, repo, "config", "user.email", "test@example.com")
	gitIn(t, repo, "config", "user.name", "Test")
	gitIn(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "file.txt")
	gitIn(t, repo, "commit", "-q", "-m", "initial")

	sessionDir := t.TempDir()
	evidenceDir = filepath.Join(sessionDir, "output")
	if err := os.MkdirAll(evidenceDir, 0o750); err != nil {
		t.Fatal(err)
	}

	runner := &acceptanceRoundRunner{
		t: t, repo: repo, evidenceDir: evidenceDir, sessionDir: sessionDir, readyInRound: readyInRound,
		breakStatusOnce: breakStatusOnce, validatorFails: validatorFails, fixMode: fixMode,
	}
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params: map[string]string{
			"change_name":                     "add-export",
			"change_dir":                      "changes/add-export",
			"change_label":                    "change",
			"artifact_validation_instruction": "Validate artifacts.",
			"skip_validator":                  skipValidator,
			"acceptance_rounds":               rounds,
			"stop_unchanged_acceptance":       stopUnchanged,
		},
		WorkflowFile:      verifyChangeRef,
		SessionDir:        sessionDir,
		NamedSessionDecls: map[string]string{"lead-agent": "lead", "acceptance-tester": "tester"},
	})

	// A group runs the tail with the same rewind handling as a workflow scope,
	// so rerun repairs replay their target.
	tail := model.Step{ID: "verify-tail", Steps: workflow.Steps[start:]}
	outcome, err := DispatchStep(&tail, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("verify-change tail: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("verify-change tail outcome = %q, want success; events %v", outcome, runner.events)
	}

	body, err := os.ReadFile(filepath.Join(evidenceDir, "acceptance-preparation-status.txt"))
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	return runner.events, strings.TrimSpace(string(body)), evidenceDir
}

func TestBuiltinVerifyChangeAcceptanceRounds(t *testing.T) {
	never := func(int) bool { return false }

	tests := []struct {
		name          string
		rounds        string
		skipValidator string
		stopUnchanged string
		fixMode       string
		readyInRound  func(int) bool
		breakStatus   bool
		failValidator bool
		wantEvents    []string
		wantStatus    string
		// wantValidatorResult is the first line of the acceptance-round
		// validator result, or empty when no acceptance validator runs.
		wantValidatorResult string
	}{
		{
			name:          "first round converges without a fix",
			rounds:        "3",
			skipValidator: "false",
			readyInRound:  func(round int) bool { return round == 1 },
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_COMPLETE",
		},
		{
			name:          "second round converges after a validated and pushed fix",
			rounds:        "3",
			skipValidator: "false",
			readyInRound:  func(round int) bool { return round == 2 },
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "check-task-compliance.sh", "run-validator.sh", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus:          "ACCEPTANCE_COMPLETE",
			wantValidatorResult: "PASS",
		},
		{
			name:          "a red validator after a fix is recorded without blocking the rounds",
			rounds:        "3",
			skipValidator: "false",
			readyInRound:  func(round int) bool { return round == 2 },
			failValidator: true,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix",
				"check-task-compliance.sh", "run-validator.sh", "fix-violations", "run-validator.sh", "fix-violations", "run-validator.sh", "fix-violations", "run-validator.sh",
				"acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus:          "ACCEPTANCE_COMPLETE",
			wantValidatorResult: "FAIL",
		},
		{
			name:          "exhausted rounds leave no untested fix and fail",
			rounds:        "3",
			skipValidator: "false",
			readyInRound:  never,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "check-task-compliance.sh", "run-validator.sh", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "check-task-compliance.sh", "run-validator.sh", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "end-final-round",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus:          "ACCEPTANCE_FAILED",
			wantValidatorResult: "PASS",
		},
		{
			name:          "skipped validator is not run between rounds",
			rounds:        "2",
			skipValidator: "true",
			readyInRound:  never,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "end-final-round",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_FAILED",
		},
		{
			name:          "a failed handoff check reruns the status step",
			rounds:        "1",
			skipValidator: "false",
			readyInRound:  never,
			breakStatus:   true,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "end-final-round",
				"acceptance-gate.sh", "verify-acceptance-handoff",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_FAILED",
		},
		{
			name:          "an unchanged fix ends the rounds when stop_unchanged_acceptance is set",
			rounds:        "3",
			skipValidator: "false",
			stopUnchanged: "true",
			fixMode:       "none",
			readyInRound:  never,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "check-acceptance-progress",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_FAILED",
		},
		{
			name:          "a committed fix continues the rounds when stop_unchanged_acceptance is set",
			rounds:        "2",
			skipValidator: "true",
			stopUnchanged: "true",
			readyInRound:  never,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "check-acceptance-progress", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "end-final-round",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_FAILED",
		},
		{
			name:          "a retest reason continues the rounds without a commit",
			rounds:        "2",
			skipValidator: "true",
			stopUnchanged: "true",
			fixMode:       "retest",
			readyInRound:  func(round int) bool { return round == 2 },
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "check-acceptance-progress", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_COMPLETE",
		},
		{
			name:          "an unchanged fix continues the rounds by default",
			rounds:        "2",
			skipValidator: "true",
			fixMode:       "none",
			readyInRound:  never,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "end-final-round",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_FAILED",
		},
		{
			name:          "a single round never fixes",
			rounds:        "1",
			skipValidator: "false",
			readyInRound:  never,
			wantEvents: []string{
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "end-final-round",
				"acceptance-gate.sh", "verify-acceptance-handoff",
			},
			wantStatus: "ACCEPTANCE_FAILED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopUnchanged := tt.stopUnchanged
			if stopUnchanged == "" {
				stopUnchanged = "false"
			}
			events, status, evidenceDir := runVerifyChangeAcceptance(t, tt.rounds, tt.skipValidator, stopUnchanged, tt.fixMode, tt.readyInRound, tt.breakStatus, tt.failValidator)
			if diff := cmp.Diff(tt.wantEvents, events); diff != "" {
				t.Errorf("events mismatch (-want +got):\n%s", diff)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			handoff, err := os.ReadFile(filepath.Join(evidenceDir, "acceptance-handoff.md"))
			if err != nil {
				t.Fatalf("read handoff: %v", err)
			}
			if tt.wantStatus == "ACCEPTANCE_FAILED" && !strings.Contains(string(handoff), filepath.Join(evidenceDir, "acceptance-findings.md")) {
				t.Errorf("failure handoff does not point at the open findings:\n%s", handoff)
			}
			result, err := os.ReadFile(filepath.Join(evidenceDir, "acceptance-validator-result.txt"))
			gotResult := ""
			if err == nil {
				gotResult, _, _ = strings.Cut(string(result), "\n")
			} else if !os.IsNotExist(err) {
				t.Fatalf("read acceptance validator result: %v", err)
			}
			if gotResult != tt.wantValidatorResult {
				t.Errorf("acceptance validator result = %q, want %q", gotResult, tt.wantValidatorResult)
			}
		})
	}
}

// implementationReviewRunner runs the head of verify-change, from the
// skip_implementation_review validation through simplify. Scripts and shell
// checks run for real; the lead's assumption review writes the handoff.
type implementationReviewRunner struct {
	t          *testing.T
	repo       string
	sessionDir string
	events     []string
}

func (r *implementationReviewRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	r.events = append(r.events, "verify-assumptions-handoff")
	return runProcess(osexec.Command("sh", "-c", cmd), r.repo, nil)
}

func (r *implementationReviewRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	r.events = append(r.events, filepath.Base(path))
	return runProcess(osexec.Command("sh", path), r.repo, stdin)
}

func (r *implementationReviewRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	switch {
	case strings.Contains(options.Prefix, "review-assumptions"):
		r.events = append(r.events, "review-assumptions")
		handoff := filepath.Join(r.sessionDir, "output", "acceptance-assumptions.md")
		if err := os.WriteFile(handoff, []byte("No unresolved assumptions or context gaps.\n"), 0o600); err != nil {
			r.t.Fatalf("write handoff: %v", err)
		}
	case strings.Contains(options.Prefix, "simplify"):
		r.events = append(r.events, "simplify")
	default:
		r.t.Fatalf("unexpected agent step %s", options.Prefix)
	}
	return ProcessResult{Started: true, ExitCode: 0, Stdout: claudeUsageOutput("done", 0)}, nil
}

func TestBuiltinVerifyChangeSkipImplementationReview(t *testing.T) {
	tests := []struct {
		name        string
		skip        string
		handoff     bool
		wantOutcome StepOutcome
		wantEvents  []string
	}{
		{
			name:        "default runs the assumption review and simplification",
			skip:        "false",
			wantOutcome: OutcomeSuccess,
			wantEvents:  []string{"validate-boolean-param.sh", "review-assumptions", "verify-assumptions-handoff", "simplify"},
		},
		{
			name:        "skipping keeps the handoff check",
			skip:        "true",
			handoff:     true,
			wantOutcome: OutcomeSuccess,
			wantEvents:  []string{"validate-boolean-param.sh", "verify-assumptions-handoff"},
		},
		{
			name:        "a skipped review still requires the caller's handoff",
			skip:        "true",
			wantOutcome: OutcomeFailed,
			wantEvents:  []string{"validate-boolean-param.sh", "verify-assumptions-handoff"},
		},
		{
			name:        "a non-boolean value fails before any review",
			skip:        "yes",
			wantOutcome: OutcomeFailed,
			wantEvents:  []string{"validate-boolean-param.sh"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			workflow, err := loader.LoadWorkflow(verifyChangeRef, loader.Options{})
			if err != nil {
				t.Fatalf("LoadWorkflow: %v", err)
			}
			start, end := -1, -1
			for i := range workflow.Steps {
				switch workflow.Steps[i].ID {
				case "validate-skip-implementation-review":
					start = i
				case "simplify":
					end = i
				}
			}
			if start < 0 || end < start {
				t.Fatalf("verify-change has no validate-skip-implementation-review..simplify range (%d..%d)", start, end)
			}
			head := make([]model.Step, end-start+1)
			copy(head, workflow.Steps[start:end+1])
			// The handoff check's repair would start a lead session; this test
			// observes only whether the check passes.
			for i := range head {
				head[i].Repair = nil
			}

			sessionDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(sessionDir, "output"), 0o750); err != nil {
				t.Fatal(err)
			}
			if tt.handoff {
				handoff := filepath.Join(sessionDir, "output", "acceptance-assumptions.md")
				if err := os.WriteFile(handoff, []byte("No questions were escalated to the lead; no lead decisions are recorded.\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runner := &implementationReviewRunner{t: t, repo: t.TempDir(), sessionDir: sessionDir}
			ctx := model.NewRootContext(&model.RootContextOptions{
				Params: map[string]string{
					"change_name":                     "add-export",
					"change_dir":                      "changes/add-export",
					"change_label":                    "change",
					"artifact_validation_instruction": "Validate artifacts.",
					"skip_validator":                  "false",
					"acceptance_rounds":               "3",
					"skip_implementation_review":      tt.skip,
				},
				WorkflowFile:      verifyChangeRef,
				SessionDir:        sessionDir,
				NamedSessionDecls: map[string]string{"lead-agent": "lead", "acceptance-tester": "tester"},
			})
			group := model.Step{ID: "verify-head", Steps: head}
			outcome, err := DispatchStep(&group, ctx, runner, &mockGlob{}, &mockLogger{})
			if err != nil && tt.wantOutcome == OutcomeSuccess {
				t.Fatalf("verify-change head: %v", err)
			}
			if outcome != tt.wantOutcome {
				t.Errorf("outcome = %q, want %q; events %v", outcome, tt.wantOutcome, runner.events)
			}
			if diff := cmp.Diff(tt.wantEvents, runner.events); diff != "" {
				t.Errorf("events mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
