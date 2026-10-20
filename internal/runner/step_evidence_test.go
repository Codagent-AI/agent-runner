package runner

import (
	"bytes"
	"encoding/json"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
)

// evidenceProcessRunner uses real shell, script and stub-agent processes.
type evidenceProcessRunner struct{ root string }

func (r evidenceProcessRunner) execute(cmd *osExec.Cmd) (exec.ProcessResult, error) {
	if cmd.Dir == "" {
		cmd.Dir = r.root
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if e, ok := err.(*osExec.ExitError); ok {
		exit = e.ExitCode()
	} else if err != nil {
		return exec.ProcessResult{}, err
	}
	return exec.ProcessResult{Started: true, ExitCode: exit, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}
func (r evidenceProcessRunner) RunShell(command string, _ bool, workdir string) (exec.ProcessResult, error) {
	cmd := osExec.Command("sh", "-c", command)
	cmd.Dir = workdir
	return r.execute(cmd)
}
func (r evidenceProcessRunner) RunScript(path string, stdin []byte, _ bool, workdir string) (exec.ProcessResult, error) {
	cmd := osExec.Command(path)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Dir = workdir
	return r.execute(cmd)
}
func (r evidenceProcessRunner) RunAgent(opts *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	cmd := osExec.Command(opts.Args[0], opts.Args[1:]...)
	cmd.Dir = opts.Workdir
	cmd.Env = exec.BuildAgentEnvironment(os.Environ(), opts.DropEnv, opts.Env)
	return r.execute(cmd)
}

func TestINT001RunnerStepEvidence(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := osExec.Command("git", append([]string{"-C", repo}, args...)...)
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", data, err)
		}
	}
	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(repo, "tracked"), []byte("base\n"), 0o600)
	git("add", ".")
	git("commit", "-m", "initial")
	session := t.TempDir()
	bin := t.TempDir()
	stub := `#!/bin/sh
printf 'B\n' >> "$EVIDENCE_SESSION/output/ledger.md"
touch "$EVIDENCE_SESSION/repaired"
printf '%s\n' '{"type":"thread.started","thread_id":"repair"}' '{"type":"item.completed","item":{"type":"agent_message","text":"repaired"}}' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'
`
	_ = os.WriteFile(filepath.Join(bin, "codex"), []byte(stub), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EVIDENCE_SESSION", session)
	workflowFile, _ := filepath.Abs("../../testdata/step-evidence-v1.0.yaml")
	workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runLog := &mockLog{}
	result, err := RunWorkflow(&workflow, nil, &Options{WorkflowFile: workflowFile, SessionDir: session, ProjectRoot: repo, WorkingDir: repo, ProcessRunner: evidenceProcessRunner{root: repo}, GlobExpander: &mockGlob{}, Log: runLog, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "codex", DefaultMode: "autonomous", Model: "test"}}}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("run %s: %v\n%s", result, err, strings.Join(runLog.lines, "\n"))
	}
	log, err := os.Open(filepath.Join(session, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	records := map[string][]audit.WriteRecord{}
	patches := map[string]audit.WriteRecord{}
	contributions := map[string]int{}
	err = audit.VisitAuditLines(log, func(_ int, line string) error {
		event, e := audit.ParseAuditLine(line)
		if e != nil || event.Type != audit.EventStepEnd {
			return e
		}
		if raw, ok := event.Data["output_writes"]; ok {
			var record audit.WriteRecord
			data, _ := json.Marshal(raw)
			_ = json.Unmarshal(data, &record)
			records[event.Prefix] = append(records[event.Prefix], record)
			for _, f := range record.Files {
				if f.Path == "_x_.out" {
					t.Error("runner stream included")
				}
				if f.Contribution != nil && f.Contribution.Blob != "" {
					content, e := os.ReadFile(filepath.Join(session, f.Contribution.Blob))
					if e != nil {
						t.Error(e)
					}
					contributions[string(content)]++
				}
			}
		}
		if raw, ok := event.Data["repository_patch"]; ok {
			data, _ := json.Marshal(raw)
			var r audit.WriteRecord
			_ = json.Unmarshal(data, &r)
			patches[event.Prefix] = r
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"A\n", "B\n", "C\n", "appended section\n", "large append\n", "script contribution\n"} {
		if contributions[text] != 1 {
			t.Fatalf("contribution %q recorded %d times", text, contributions[text])
		}
	}
	for _, id := range []string{"empty", "scratch"} {
		r := records["["+id+"]"][0]
		if r.Status != "observed" || r.Files == nil || len(r.Files) > 0 {
			t.Fatalf("%s: %+v", id, r)
		}
	}
	rounds := map[string]bool{}
	for prefix, rs := range records {
		if !strings.Contains(prefix, "rounds:") {
			continue
		}
		for _, r := range rs {
			for _, f := range r.Files {
				if f.Path == "round.md" && f.After != nil {
					data, e := os.ReadFile(filepath.Join(session, f.After.Blob))
					if e != nil {
						t.Fatal(e)
					}
					rounds[string(data)] = true
				}
			}
		}
	}
	if len(rounds) != 2 {
		t.Fatalf("retained rounds: %v", rounds)
	}
	if r := records["[large-baseline]"][0]; !r.Files[0].Contribution.Truncated {
		t.Fatal("large created file not marked truncated")
	}
	if _, ok := records["[rounds]"]; ok {
		t.Fatal("container recorded writes")
	}
	if r := records["[delete]"][0]; len(r.Files) != 1 || r.Files[0].Change != "deleted" {
		t.Fatal(r)
	}
	// Binary content limits only the file's contribution, not the record.
	if r := records["[binary]"][0]; r.Status != "observed" || r.Files[0].Contribution.Kind != "unavailable" {
		t.Fatal(r)
	}
	for _, id := range []string{"edit-one", "edit-two"} {
		r := patches["["+id+"]"]
		if len(r.Files) != 1 || r.Files[0].Contribution.Kind != "diff" {
			t.Fatalf("%s: %+v", id, r)
		}
		data, _ := os.ReadFile(filepath.Join(session, r.Files[0].Contribution.Blob))
		if id == "edit-two" && strings.Contains(string(data), "-base") {
			t.Fatal(string(data))
		}
	}
}

func (r evidenceProcessRunner) RunScriptWithEnv(path string, stdin []byte, _ bool, workdir string, environment []string) (exec.ProcessResult, error) {
	cmd := osExec.Command(path)
	cmd.Dir = workdir
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = append(os.Environ(), environment...)
	return r.execute(cmd)
}
