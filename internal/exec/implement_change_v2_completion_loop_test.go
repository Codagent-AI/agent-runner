package exec

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
)

const implementChangeV2CompletionRef = "builtin:core/implement-change-v2.0.yaml"

type completionLoopRunner struct {
	t                 *testing.T
	repo, output      string
	events            []string
	agents            []AgentProcessOptions
	judge             func(int)
	pass              func(int)
	judgments, passes int
}

func (r *completionLoopRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	return runProcess(osexec.Command("sh", "-c", cmd), r.repo, nil)
}
func (r *completionLoopRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	r.events = append(r.events, filepath.Base(path))
	return runProcess(osexec.Command("sh", path), r.repo, stdin)
}
func (r *completionLoopRunner) RunAgent(opts *AgentProcessOptions) (ProcessResult, error) {
	r.agents = append(r.agents, *opts)
	switch {
	case strings.Contains(opts.Prefix, "judge-completion"):
		r.judgments++
		r.events = append(r.events, "judge")
		r.judge(r.judgments)
	case strings.Contains(opts.Prefix, "continue-implementation"):
		r.passes++
		r.events = append(r.events, "continue")
		r.pass(r.passes)
	case strings.Contains(opts.Prefix, "audit"):
		r.events = append(r.events, "audit")
	default:
		r.t.Fatalf("unexpected agent %s", opts.Prefix)
	}
	if len(opts.Args) > 0 && opts.Args[0] == "codex" {
		return ProcessResult{Started: true, Stdout: `{"type":"thread.started","thread_id":"codex-implementor"}` + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":"latest implementor response"}}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":1}}` + "\n"}, nil
	}
	return ProcessResult{Started: true, Stdout: claudeUsageOutput("latest implementor response", 0)}, nil
}
func (r *completionLoopRunner) write(name, body string) {
	r.t.Helper()
	if e := os.WriteFile(filepath.Join(r.output, name), []byte(body), 0o600); e != nil {
		r.t.Fatal(e)
	}
}
func (r *completionLoopRunner) commit() {
	gitIn(r.t, r.repo, "commit", "-q", "--allow-empty", "-m", "feat: progress")
}
func (r *completionLoopRunner) verdict(word string) {
	r.write("completion-verdict.json", `{"verdict":"`+word+`","remaining":["finish R-1"]}`)
}
func (r *completionLoopRunner) checklist(status, evidence string) {
	r.t.Helper()
	if e := os.WriteFile(filepath.Join(r.repo, "checklist.md"), []byte("| ID | Behavior | Evidence | Status |\n|---|---|---|---|\n| R-1 | works | "+evidence+" | "+status+" |\n"), 0o600); e != nil {
		r.t.Fatal(e)
	}
}
func completionLoopFixture(t *testing.T) (*completionLoopRunner, *model.ExecutionContext, model.Step) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	w := loadBuiltinWorkflow(t, implementChangeV2CompletionRef)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "config", "user.name", "Test")
	gitIn(t, repo, "config", "user.email", "test@example.com")
	gitIn(t, repo, "config", "commit.gpgsign", "false")
	session := t.TempDir()
	if pid, e := runlock.Acquire(session); e != nil || pid != 0 {
		t.Fatalf("lock: %d %v", pid, e)
	}
	t.Cleanup(func() { runlock.Delete(session) })
	out := filepath.Join(session, "output")
	if e := os.MkdirAll(filepath.Join(out, "evidence"), 0o700); e != nil {
		t.Fatal(e)
	}
	r := &completionLoopRunner{t: t, repo: repo, output: out}
	r.checklist("verified", "`ok.log`")
	gitIn(t, repo, "add", "checklist.md")
	r.commit()
	b, _ := os.ReadFile(filepath.Join(repo, "checklist.md"))
	r.write("planned-checklist.md", string(b))
	r.write("evidence/ok.log", "PASS")
	head, e := osexec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if e != nil {
		t.Fatal(e)
	}
	ctx := model.NewRootContext(&model.RootContextOptions{Params: map[string]string{"change_name": "test", "change_label": "change", "change_dir": repo, "artifact_validation_instruction": ""}, WorkflowFile: implementChangeV2CompletionRef, SessionDir: session, WorkingDir: repo, ProjectRoot: repo, NamedSessionDecls: map[string]string{"implementor-agent": "implementor", "crosscheck-agent": "crosscheck"}})
	ctx.CapturedVariables["implementation_start_head"] = model.NewCapturedString(strings.TrimSpace(string(head)))
	ctx.CapturedVariables["implementor_response"] = model.NewCapturedString("stopped early")
	_, start := requireStep(t, &w, "complete-implementation")
	_, end := requireStep(t, &w, "audit")
	return r, ctx, model.Step{ID: "completion-tail", Steps: w.Steps[start : end+1]}
}

// INT-001 and INT-002 execute the embedded composition and real scripts/git.
func TestImplementChangeCompletionPaths(t *testing.T) {
	cases := []struct {
		name, reason   string
		setup          func(*completionLoopRunner)
		judge          func(*completionLoopRunner, int)
		pass           func(*completionLoopRunner, int)
		judges, passes int
	}{
		{name: "already complete", setup: func(r *completionLoopRunner) { r.write("implementation-complete.md", "done") }},
		{name: "continue finishes", judge: func(r *completionLoopRunner, _ int) { r.verdict("continue") }, pass: func(r *completionLoopRunner, _ int) { r.write("implementation-complete.md", "done"); r.commit() }, judges: 1, passes: 1},
		{name: "record only continue", judge: func(r *completionLoopRunner, _ int) { r.verdict("continue") }, pass: func(r *completionLoopRunner, _ int) { r.write("implementation-complete.md", "done") }, judges: 1, passes: 1},
		{name: "fix missing record", judge: func(r *completionLoopRunner, _ int) {
			r.write("implementation-complete.md", "done")
			r.verdict("fix-record")
		}, judges: 1},
		{name: "fix citation", setup: func(r *completionLoopRunner) {
			r.write("implementation-complete.md", "done")
			r.checklist("verified", "ok.log")
			gitIn(r.t, r.repo, "add", "checklist.md")
			r.commit()
		}, judge: func(r *completionLoopRunner, _ int) {
			r.checklist("verified", "`ok.log`")
			gitIn(r.t, r.repo, "add", "checklist.md")
			gitIn(r.t, r.repo, "commit", "-q", "-m", "[judge-completion] docs: fix record")
			r.verdict("fix-record")
		}, judges: 1},
		{name: "complete exception", setup: func(r *completionLoopRunner) {
			r.write("implementation-complete.md", "done")
			r.checklist("not verified", "needs unavailable external service")
		}, judge: func(r *completionLoopRunner, _ int) { r.verdict("complete") }, judges: 1},
		{name: "judge blocked", reason: "the change cannot be implemented compliantly", judge: func(r *completionLoopRunner, _ int) {
			r.write("implementation-blocked.md", "need user decision")
			r.verdict("blocked")
		}, judges: 1},
		{name: "blocked during continue", reason: "the change cannot be implemented compliantly", judge: func(r *completionLoopRunner, _ int) { r.verdict("continue") }, pass: func(r *completionLoopRunner, _ int) { r.write("implementation-blocked.md", "human decision needed") }, judges: 1, passes: 1},
		{name: "implementor blocked", reason: "the change cannot be implemented compliantly", setup: func(r *completionLoopRunner) { r.write("implementation-blocked.md", "need user decision") }},
		{name: "three continues", reason: "implementation incomplete: 1 items remain after 3 continue passes", judge: func(r *completionLoopRunner, _ int) { r.verdict("continue") }, pass: func(r *completionLoopRunner, _ int) { r.commit() }, judges: 4, passes: 3},
		{name: "third continue completes", judge: func(r *completionLoopRunner, _ int) { r.verdict("continue") }, pass: func(r *completionLoopRunner, n int) {
			r.commit()
			if n == 3 {
				r.write("implementation-complete.md", "done")
			}
		}, judges: 3, passes: 3},
		{name: "stall", reason: "implementation stalled: continue pass 1", judge: func(r *completionLoopRunner, _ int) { r.verdict("continue") }, pass: func(_ *completionLoopRunner, _ int) {}, judges: 1, passes: 1},
		{name: "fix bound", reason: "implementation record could not be fixed", judge: func(r *completionLoopRunner, _ int) { r.verdict("fix-record") }, judges: 3},
		{name: "missing verdict", reason: "completion judge returned no valid verdict", judge: func(_ *completionLoopRunner, _ int) {}, judges: 1},
		{name: "unknown verdict", reason: "completion judge returned no valid verdict", judge: func(r *completionLoopRunner, _ int) { r.verdict("unknown") }, judges: 1},
		{name: "empty remaining", reason: "completion judge returned no valid verdict", judge: func(r *completionLoopRunner, _ int) {
			r.write("completion-verdict.json", `{"verdict":"continue","remaining":[]}`)
		}, judges: 1},
		{name: "unguarded complete", reason: "completion judge returned no valid verdict", judge: func(r *completionLoopRunner, _ int) { r.verdict("complete") }, judges: 1},
		{name: "missing blocked record", reason: "completion judge returned no valid verdict", judge: func(r *completionLoopRunner, _ int) { r.verdict("blocked") }, judges: 1},
		{name: "longest sequence", reason: "implementation incomplete", judge: func(r *completionLoopRunner, n int) {
			if n%3 == 0 || n == 12 {
				r.verdict("continue")
			} else {
				r.verdict("fix-record")
			}
		}, pass: func(r *completionLoopRunner, _ int) { r.commit() }, judges: 12, passes: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, ctx, tail := completionLoopFixture(t)
			r.commit()
			if tc.setup != nil {
				tc.setup(r)
			}
			r.judge = func(n int) { tc.judge(r, n) }
			r.pass = func(n int) { tc.pass(r, n) }
			outcome, e := DispatchStep(&tail, ctx, r, &mockGlob{}, &mockLogger{})
			if e != nil {
				t.Fatal(e)
			}
			if tc.reason == "" {
				if outcome != OutcomeSuccess || r.events[len(r.events)-1] != "audit" {
					t.Fatalf("outcome %s events %v", outcome, r.events)
				}
			} else {
				if outcome != OutcomeFailed || ctx.LastFailure == nil || !strings.Contains(ClassifyFailure(ctx.LastFailure), tc.reason) {
					t.Fatalf("outcome %s failure %+v events %v", outcome, ctx.LastFailure, r.events)
				}
				if r.events[len(r.events)-1] == "audit" {
					t.Fatal("reached audit on failure")
				}
			}
			if r.judgments != tc.judges || r.passes != tc.passes {
				t.Fatalf("judges=%d passes=%d events=%v", r.judgments, r.passes, r.events)
			}
			judgeSessions := map[string]bool{}
			for _, a := range r.agents {
				if strings.Contains(a.Prefix, "judge-completion") {
					id, ok := argValue(a.Args, "--session-id")
					if !ok || id == "" || judgeSessions[id] {
						t.Fatalf("judge session not fresh: %v", a.Args)
					}
					judgeSessions[id] = true
					if _, ok := argValue(a.Args, "--resume"); ok {
						t.Fatal("judge resumed a session")
					}
				}
				if strings.Contains(a.Prefix, "continue-implementation") && !strings.Contains(strings.Join(a.Args, " "), "finish R-1") {
					t.Fatal("remaining work not routed")
				}
			}
			if r.passes > 0 && ctx.CapturedVariables["implementor_response"].Str != "latest implementor response" {
				t.Fatalf("capture not merged: %v", ctx.CapturedVariables)
			}
		})
	}
}

func TestImplementChangeCompletionJudgeCommitExcluded(t *testing.T) {
	r, ctx, tail := completionLoopFixture(t)
	r.write("implementation-complete.md", "done")
	gitIn(t, r.repo, "commit", "-q", "--allow-empty", "-m", "[judge-completion] docs: record")
	r.judge = func(_ int) {
		b, _ := os.ReadFile(filepath.Join(r.output, "completion-problems.txt"))
		if !strings.Contains(string(b), "no implementation commit") {
			t.Fatalf("problems %s", b)
		}
		r.verdict("complete")
	}
	outcome, e := DispatchStep(&tail, ctx, r, &mockGlob{}, &mockLogger{})
	if e != nil || outcome != OutcomeFailed {
		t.Fatalf("%s %v", outcome, e)
	}
}

func TestImplementChangeEarlierLayoutMissingCapture(t *testing.T) {
	r, ctx, tail := completionLoopFixture(t)
	r.commit()
	delete(ctx.CapturedVariables, "implementor_response")
	r.judge = func(_ int) { t.Fatal("judge launched with undefined capture") }
	auditLog := &mockAuditLogger{}
	ctx.AuditLogger = auditLog
	outcome, e := DispatchStep(&tail, ctx, r, &mockGlob{}, &mockLogger{})
	if outcome != OutcomeFailed || ctx.StepFailure.Kind != model.FailureStep {
		t.Fatalf("%s %v %+v", outcome, e, ctx.StepFailure)
	}
	found := false
	for _, event := range auditLog.events {
		if text, ok := event.Data["error"].(string); ok && strings.Contains(text, "implementor_response") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing undefined-variable evidence")
	}
	if r.judgments != 0 {
		t.Fatal("judge invoked")
	}
}

func TestImplementChangeEarlierLayoutRemovedRepairTarget(t *testing.T) {
	w := loadBuiltinWorkflow(t, implementChangeV2CompletionRef)
	// Old failed repair replay points to a target removed by the new layout.
	frame := &model.RepairFrame{CheckID: "check-evidence", Target: "complete-evidence", Form: "rerun", Phase: model.RepairPhaseFailed}
	_, e := model.ResolveResumeStep(w.Steps, "check-evidence", false, frame)
	if e == nil || !strings.Contains(e.Error(), "complete-evidence") {
		t.Fatalf("resume: %v", e)
	}
}

func TestImplementChangeCompletionCodexImplementor(t *testing.T) {
	r, ctx, tail := completionLoopFixture(t)
	r.commit()
	ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{"implementor": {CLI: "codex", DefaultMode: "autonomous"}, "tester": {CLI: "claude", DefaultMode: "autonomous"}, "crosscheck": {CLI: "claude", DefaultMode: "autonomous"}}}
	r.judge = func(_ int) { r.verdict("continue") }
	r.pass = func(_ int) { r.write("implementation-complete.md", "done") }
	outcome, e := DispatchStep(&tail, ctx, r, &mockGlob{}, &mockLogger{})
	if e != nil || outcome != OutcomeSuccess || r.passes != 1 {
		t.Fatalf("outcome %s err %v events %v", outcome, e, r.events)
	}
	if ctx.CapturedVariables["implementor_response"].Str != "latest implementor response" {
		t.Fatalf("Codex response not captured: %+v", ctx.CapturedVariables)
	}
}
