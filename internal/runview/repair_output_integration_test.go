package runview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	stepExec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

type repairEvidenceRunner struct {
	t       *testing.T
	results []stepExec.ProcessResult
}

func (r *repairEvidenceRunner) next() (stepExec.ProcessResult, error) {
	r.t.Helper()
	if len(r.results) == 0 {
		r.t.Fatal("unexpected process")
	}
	v := r.results[0]
	r.results = r.results[1:]
	return v, nil
}
func (r *repairEvidenceRunner) RunAgent(_ *stepExec.AgentProcessOptions) (stepExec.ProcessResult, error) {
	return r.next()
}
func (r *repairEvidenceRunner) RunShell(_ string, _ bool, _ string) (stepExec.ProcessResult, error) {
	return r.next()
}
func (r *repairEvidenceRunner) RunScript(_ string, _ []byte, _ bool, _ string) (stepExec.ProcessResult, error) {
	return r.next()
}

type repairEvidenceLog struct{}

func (*repairEvidenceLog) Println(...any)        {}
func (*repairEvidenceLog) Printf(string, ...any) {}
func (*repairEvidenceLog) Errorf(string, ...any) {}

type repairEvidenceGlob struct{}

func (*repairEvidenceGlob) Expand(string) ([]string, error) { return nil, nil }

// INT-004 runs repairs through real persisted state and audit reconstruction.
func TestRepairOutputBeforeRepairHistoricalIntegration(t *testing.T) {
	agent := func(text string) stepExec.ProcessResult {
		b, _ := json.Marshal(map[string]string{"type": "result", "result": text})
		return stepExec.ProcessResult{Started: true, Stdout: string(b)}
	}
	check := stepExec.ProcessResult{ExitCode: 2, Stderr: "original check"}
	blocked := agent("needs decision\nREPAIR_BLOCKED")
	for _, tc := range []struct {
		name           string
		rerun, guarded bool
		results        []stepExec.ProcessResult
		want           string
		flag           bool
	}{
		{name: "inline blocked", results: []stepExec.ProcessResult{check, blocked}, want: "check failed: before repair: original check; blocked: needs decision", flag: true},
		{name: "rerun blocks second attempt", rerun: true, results: []stepExec.ProcessResult{agent("worked"), check, agent("fixed"), {ExitCode: 2, Stderr: "3 rows unverified"}, blocked}, want: "check failed: before repair: 3 rows unverified; blocked: needs decision after 2 repair attempts", flag: true},
		{name: "rerun blocked", rerun: true, results: []stepExec.ProcessResult{agent("worked"), check, blocked}, want: "check failed: before repair: original check; blocked: needs decision", flag: true},
		{name: "step failure", results: []stepExec.ProcessResult{check, {Started: true, Stderr: "Tool error: AskUserQuestion error: not supported in autonomous mode"}}, want: "check failed: before repair: original check after 1 repair attempts", flag: true},
		{name: "guarded blocked", guarded: true, results: []stepExec.ProcessResult{blocked, check}, want: "check failed: original check; blocked: needs decision"},
		{name: "rechecked exhaustion", results: []stepExec.ProcessResult{check, agent("fixed"), {ExitCode: 2, Stderr: "still red"}}, want: "check failed: still red after 1 repair attempts"},
		{name: "empty stderr", results: []stepExec.ProcessResult{{ExitCode: 2}, blocked}, want: "check failed with exit code 2 before repair; blocked: needs decision", flag: true},
		{name: "crash", results: []stepExec.ProcessResult{check, {Started: true, ExitCode: 1, Stderr: "Selected model is at capacity"}}, want: "check repair failed (infrastructure): Selected model is at capacity after 1 repair attempts", flag: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			workflowPath := filepath.Join(dir, "repair-v1.0.yaml")
			text := "name: repair\nsteps:\n"
			if tc.rerun || tc.guarded {
				text += "  - id: act\n    agent: implementor\n    session: new\n    mode: autonomous\n    prompt: act\n"
			}
			text += "  - id: check\n    command: check-it\n    repair:\n"
			if tc.name == "rerun blocks second attempt" {
				text += "      max: 3\n"
			} else {
				text += "      max: 1\n"
			}
			if tc.rerun {
				text += "      rerun: act\n"
			} else {
				text += "      agent: implementor\n      prompt: fix it\n"
			}
			if e := os.WriteFile(workflowPath, []byte(text), 0o600); e != nil {
				t.Fatal(e)
			}
			w, e := loader.LoadWorkflow(workflowPath, loader.Options{})
			if e != nil {
				t.Fatal(e)
			}
			session := filepath.Join(dir, "project", "runs", "repair-test")
			result, e := runner.RunWorkflow(&w, nil, &runner.Options{SessionDir: session, WorkflowFile: workflowPath, ProjectRoot: dir, WorkingDir: dir, ProcessRunner: &repairEvidenceRunner{t: t, results: tc.results}, GlobExpander: &repairEvidenceGlob{}, Log: &repairEvidenceLog{}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"implementor": {CLI: "claude", DefaultMode: "autonomous"}}}})
			if e != nil || result != runner.ResultFailed {
				t.Fatalf("result %s: %v", result, e)
			}
			state, e := stateio.ReadState(filepath.Join(session, "state.json"))
			if e != nil {
				t.Fatal(e)
			}
			if state.FailureReason != tc.want {
				t.Fatalf("state %q want %q", state.FailureReason, tc.want)
			}
			if got := FailureReasonForSession(session); got != tc.want {
				t.Fatalf("debug reason %q", got)
			}
			m, e := New(session, filepath.Dir(filepath.Dir(session)), FromInspect)
			if e != nil {
				t.Fatal(e)
			}
			// Replay the full durable log for per-check evidence, including a
			// rerun whose saved position is still its earlier target.
			direct := BuildTree(&w, workflowPath)
			events, e := (&FileTailer{}).ReadSince(session)
			if e != nil {
				t.Fatal(e)
			}
			for _, event := range events {
				direct.ApplyEvent(event)
			}
			if m.failureReason() != tc.want {
				t.Fatalf("run view %q", m.failureReason())
			}
			node := childByID(direct.Root, "check")
			if node == nil || node.Failure == nil {
				b, _ := os.ReadFile(filepath.Join(session, "audit.log"))
				t.Fatalf("no failure evidence: node %+v audit %s", node, b)
			}
			if node.Failure.OutputBeforeRepair != tc.flag {
				t.Fatalf("flag %+v", node.Failure)
			}
			if tc.name != "crash" && classifiedFailureReason(node.Failure) != tc.want {
				t.Fatalf("reconstructed reason %q", classifiedFailureReason(node.Failure))
			}
			audit, e := os.ReadFile(filepath.Join(session, "audit.log"))
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, line := range strings.Split(string(audit), "\n") {
				if strings.Contains(line, " run_end ") {
					var data map[string]any
					if e = json.Unmarshal([]byte(strings.SplitN(line, " run_end ", 2)[1]), &data); e != nil {
						t.Fatal(e)
					}
					if data["failure_reason"] != tc.want {
						t.Fatalf("run_end %v", data)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("no run_end")
			}
		})
	}
}
