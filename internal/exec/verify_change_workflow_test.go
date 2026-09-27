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
	readyInRound func(round int) bool
	round        int
	events       []string
	// breakStatusOnce deletes the acceptance status before the first
	// verify-acceptance-handoff check so its rerun repair must restore it.
	breakStatusOnce bool
	// validatorFails makes every validator run fail, so its repairs are
	// exhausted and the validator workflow ends with a warning.
	validatorFails bool
}

func (r *acceptanceRoundRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	switch {
	case strings.HasPrefix(cmd, "rm -f"):
		r.events = append(r.events, "reset-round-evidence")
	case strings.Contains(cmd, "acceptance round limit reached"):
		r.events = append(r.events, "end-final-round")
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
	case "acceptance-gate.sh":
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
	cmd.Dir = r.repo
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
	r.write("acceptance-flow-evidence.md", "flows at "+head+"\n")
	if r.readyInRound(r.round) {
		r.write("acceptance-test.md", "tested "+head+"\n")
		r.write("acceptance-handoff.md", "ready "+head+"\n")
		r.write("acceptance-round-status.txt", "READY "+head+"\n")
		return
	}
	r.write("acceptance-findings.md", "AT-1 defect found at "+head+"\n")
	r.write("acceptance-round-status.txt", "NOT_READY\n")
}

func (r *acceptanceRoundRunner) fix() {
	if err := os.WriteFile(filepath.Join(r.repo, "file.txt"), []byte(r.head()+"\n"), 0o600); err != nil {
		r.t.Fatalf("write fix: %v", err)
	}
	gitIn(r.t, r.repo, "commit", "-q", "-am", "fix")
	r.write("acceptance-impact-scope.md", "targeted: AT-1\n")
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
func runVerifyChangeAcceptance(t *testing.T, rounds, skipValidator string, readyInRound func(int) bool, breakStatusOnce, validatorFails bool) (events []string, status, evidenceDir string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

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
		t: t, repo: repo, evidenceDir: evidenceDir, readyInRound: readyInRound,
		breakStatusOnce: breakStatusOnce, validatorFails: validatorFails,
	}
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params: map[string]string{
			"change_name":                     "add-export",
			"change_dir":                      "changes/add-export",
			"change_label":                    "change",
			"artifact_validation_instruction": "Validate artifacts.",
			"skip_validator":                  skipValidator,
			"acceptance_rounds":               rounds,
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
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "run-validator.sh", "acceptance-push.sh", "check-draft-pr.sh",
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
				"run-validator.sh", "fix-violations", "run-validator.sh", "fix-violations", "run-validator.sh", "fix-violations", "run-validator.sh",
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
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "run-validator.sh", "acceptance-push.sh", "check-draft-pr.sh",
				"reset-round-evidence", "acceptance-test", "acceptance-gate.sh", "acceptance-fix", "run-validator.sh", "acceptance-push.sh", "check-draft-pr.sh",
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
			events, status, evidenceDir := runVerifyChangeAcceptance(t, tt.rounds, tt.skipValidator, tt.readyInRound, tt.breakStatus, tt.failValidator)
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
