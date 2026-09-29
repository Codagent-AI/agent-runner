package runner

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
)

type finalizeCIProcessRunner struct {
	agents    []string
	scripts   []string
	captures  []string
	snapshots []string
}

func (r *finalizeCIProcessRunner) RunShell(_ string, _ bool, _ string) (exec.ProcessResult, error) {
	return exec.ProcessResult{Started: true, Stdout: "https://github.com/example/project/pull/12"}, nil
}

func (r *finalizeCIProcessRunner) RunScript(path string, stdin []byte, _ bool, workdir string) (exec.ProcessResult, error) {
	r.scripts = append(r.scripts, filepath.Base(path))
	cmd := osexec.Command("sh", path)
	cmd.Dir = workdir
	if filepath.Base(path) == "ci-wait.sh" && len(r.snapshots) > len(r.captures) {
		cmd.Env = append(os.Environ(), "CI_SNAPSHOT="+r.snapshots[len(r.captures)])
	}
	cmd.Stdin = strings.NewReader(string(stdin))
	out, err := cmd.Output()
	code := 0
	if exit, ok := err.(*osexec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		return exec.ProcessResult{}, err
	}
	if filepath.Base(path) == "ci-wait.sh" {
		r.captures = append(r.captures, string(out))
	}
	return exec.ProcessResult{Started: true, ExitCode: code, Stdout: string(out)}, nil
}

func (r *finalizeCIProcessRunner) RunAgent(options *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	r.agents = append(r.agents, strings.Join(options.Args, "\n"))
	return exec.ProcessResult{Started: true, Stdout: claudeAgentOutput("done")}, nil
}

// INT-001 crosses the loader, bundled script materialization, capture, and gate boundaries.
func TestFinalizePRGreenRunsNoWaitAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	stub := `#!/bin/sh
case "$*" in
  "pr view --json number,url") echo '{"number":12,"url":"https://github.com/example/project/pull/12"}' ;;
  "api graphql"*) cat <<'JSON'
{"data":{"repository":{"pullRequest":{"url":"https://github.com/example/project/pull/12","headRefOid":"abcdef123456","mergeable":"MERGEABLE","author":{"login":"alice","__typename":"User"},"timelineItems":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"headRef":{"target":{"checkSuites":{"nodes":[],"pageInfo":{"hasNextPage":false}},"statusCheckRollup":{"contexts":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}},"reviews":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"comments":{"nodes":[],"pageInfo":{"hasPreviousPage":false}}}}}}
JSON
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("AGENT_RUNNER_CI_WAIT_TIMINGS", `{"deadline_seconds":2,"poll_interval_seconds":0.05,"bot_start_grace_seconds":0.1,"call_timeout_seconds":0.5}`)
	ref := "builtin:core/finalize-pr-v1.0.yaml"
	workflow, err := loader.LoadWorkflow(ref, loader.Options{IsSubWorkflow: true})
	if err != nil {
		t.Fatal(err)
	}
	process := &finalizeCIProcessRunner{}
	result, err := RunWorkflow(&workflow, nil, &Options{WorkflowFile: ref, SessionDir: t.TempDir(), ProjectRoot: dir, WorkingDir: dir,
		ProcessRunner: process, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "claude"}}}})
	if err != nil || result != "success" {
		t.Fatalf("result=%s error=%v scripts=%v captures=%v", result, err, process.scripts, process.captures)
	}
	if len(process.agents) != 1 {
		t.Fatalf("agent turns=%d, want push-pr only", len(process.agents))
	}
	if len(process.captures) != 2 {
		t.Fatalf("CI waits=%d, want 2", len(process.captures))
	}
	for _, capture := range process.captures {
		if !strings.HasSuffix(strings.TrimSpace(capture), "CI_PASSED") {
			t.Fatalf("report: %s", capture)
		}
	}
}

func TestFinalizePRFailureBudgetAndIncompleteReview(t *testing.T) {
	base := `{"data":{"repository":{"pullRequest":{"url":"https://github.com/example/project/pull/12","headRefOid":"abcdef123456","mergeable":"MERGEABLE","author":{"login":"alice","__typename":"User"},"timelineItems":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"headRef":{"target":{"checkSuites":{"nodes":[],"pageInfo":{"hasNextPage":false}},"statusCheckRollup":{"contexts":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}},"reviews":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"comments":{"nodes":[],"pageInfo":{"hasPreviousPage":false}}}}}}`
	for _, tt := range []struct {
		name, snapshot, mode string
		params               map[string]string
		agents, waits        int
		result, marker       string
	}{
		{"failed checks", strings.Replace(base, `"nodes":[],"pageInfo":{"hasNextPage":false}}}}},"reviews"`, `"nodes":[{"name":"unit tests","conclusion":"FAILURE","detailsUrl":"https://github.com/example/project/actions/runs/123"}],"pageInfo":{"hasNextPage":false}}}}},"reviews"`, 1), "", map[string]string{"ci_fix_cycles": "2"}, 3, 3, "failed", "CI_FAILED"},
		{"incomplete review", base, "", map[string]string{"review_bots": "coderabbitai"}, 1, 2, "success", "CI_REVIEW_INCOMPLETE"},
		{"missing PR", base, "no_pr", map[string]string{"ci_fix_cycles": "2"}, 1, 3, "failed", ""},
		{"authentication failure", base, "auth", map[string]string{"ci_fix_cycles": "2"}, 1, 3, "failed", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), []byte(tt.snapshot), 0o600); err != nil {
				t.Fatal(err)
			}
			stub := `#!/bin/sh
if [ "$CI_GH_MODE" = no_pr ] && [ "$1" = pr ]; then echo 'no pull requests found' >&2; exit 1; fi
if [ "$CI_GH_MODE" = auth ] && [ "$1" = api ]; then echo 'HTTP 401 Bad credentials' >&2; exit 1; fi
case "$*" in
  "pr view --json number,url") echo '{"number":12,"url":"https://github.com/example/project/pull/12"}' ;;
  "api graphql"*) cat "$CI_SNAPSHOT" ;;
  "run view"*) echo 'failed job log excerpt' ;;
  *) exit 2 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			t.Setenv("CI_SNAPSHOT", filepath.Join(dir, "snapshot.json"))
			t.Setenv("CI_GH_MODE", tt.mode)
			t.Setenv("AGENT_RUNNER_CI_WAIT_TIMINGS", `{"deadline_seconds":2,"poll_interval_seconds":0.1,"bot_start_grace_seconds":0.2,"call_timeout_seconds":1}`)
			ref := "builtin:core/finalize-pr-v1.0.yaml"
			workflow, err := loader.LoadWorkflow(ref, loader.Options{IsSubWorkflow: true})
			if err != nil {
				t.Fatal(err)
			}
			process := &finalizeCIProcessRunner{}
			result, err := RunWorkflow(&workflow, tt.params, &Options{WorkflowFile: ref, SessionDir: t.TempDir(), ProjectRoot: dir, WorkingDir: dir,
				ProcessRunner: process, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "claude"}}}})
			if err != nil || string(result) != tt.result {
				t.Fatalf("result=%s error=%v captures=%v", result, err, process.captures)
			}
			if len(process.agents) != tt.agents || len(process.captures) != tt.waits {
				t.Fatalf("agents=%d waits=%d, want %d/%d; captures=%v", len(process.agents), len(process.captures), tt.agents, tt.waits, process.captures)
			}
			for _, report := range process.captures {
				if tt.marker == "" {
					if report != "" {
						t.Fatalf("fatal report=%q", report)
					}
					continue
				}
				if !strings.HasSuffix(strings.TrimSpace(report), tt.marker) {
					t.Fatalf("report=%q", report)
				}
			}
			if tt.name == "failed checks" {
				for _, prompt := range process.agents[1:] {
					for _, part := range []string{"unit tests", "failed job log excerpt", "CI_FAILED", "<ci-report>"} {
						if !strings.Contains(prompt, part) {
							t.Fatalf("fix prompt missing %q: %s", part, prompt)
						}
					}
				}
			}
		})
	}
}

// INT-002/003: the loop skips fixes for pending CI and resumes once for comments.
func TestFinalizePRSequenceThenGreen(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		first      string
		marker     string
		agentTurns int
	}{
		{"pending", "pending", "CI_PENDING", 1},
		{"comments", "comments", "CI_COMMENTS", 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runFinalizePRSequence(t, scenario.first, scenario.marker, scenario.agentTurns)
		})
	}
}

func runFinalizePRSequence(t *testing.T, firstKind, firstMarker string, agentTurns int) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	base := `{"data":{"repository":{"pullRequest":{"url":"https://github.com/example/project/pull/12","headRefOid":"abcdef123456","mergeable":"MERGEABLE","author":{"login":"alice","__typename":"User"},"timelineItems":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"headRef":{"target":{"checkSuites":{"nodes":[],"pageInfo":{"hasNextPage":false}},"statusCheckRollup":{"contexts":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}},"reviews":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"comments":{"nodes":[],"pageInfo":{"hasPreviousPage":false}}}}}}`
	pending := strings.Replace(base, `"nodes":[],"pageInfo":{"hasNextPage":false}}}}},"reviews"`, `"nodes":[{"name":"build","status":"IN_PROGRESS"}],"pageInfo":{"hasNextPage":false}}}}},"reviews"`, 1)
	comments := strings.Replace(base, `"comments":{"nodes":[],"pageInfo":{"hasPreviousPage":false}}`, `"comments":{"nodes":[{"author":{"login":"reviewer","__typename":"User"},"body":"please fix"}],"pageInfo":{"hasPreviousPage":false}}`, 1)
	first := pending
	if firstKind == "comments" {
		first = comments
	}
	paths := make([]string, 3)
	for i, fixture := range []string{first, base, base} {
		paths[i] = filepath.Join(dir, string(rune('0'+i))+".json")
		if err := os.WriteFile(paths[i], []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stub := `#!/bin/sh
case "$*" in
  "pr view --json number,url") echo '{"number":12,"url":"https://github.com/example/project/pull/12"}' ;;
  "api graphql"*) cat "$CI_SNAPSHOT" ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("AGENT_RUNNER_CI_WAIT_TIMINGS", `{"deadline_seconds":2,"poll_interval_seconds":0.1,"bot_start_grace_seconds":0.2,"call_timeout_seconds":1}`)
	ref := "builtin:core/finalize-pr-v1.0.yaml"
	workflow, err := loader.LoadWorkflow(ref, loader.Options{IsSubWorkflow: true})
	if err != nil {
		t.Fatal(err)
	}
	process := &finalizeCIProcessRunner{snapshots: paths}
	result, err := RunWorkflow(&workflow, map[string]string{"ci_fix_cycles": "2"}, &Options{WorkflowFile: ref, SessionDir: t.TempDir(), ProjectRoot: dir, WorkingDir: dir,
		ProcessRunner: process, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "claude"}}}})
	if err != nil || result != "success" {
		t.Fatalf("result=%s error=%v reports=%v", result, err, process.captures)
	}
	if len(process.agents) != agentTurns || len(process.captures) != 3 {
		t.Fatalf("agents=%d waits=%d reports=%v", len(process.agents), len(process.captures), process.captures)
	}
	for i, marker := range []string{firstMarker, "CI_PASSED", "CI_PASSED"} {
		if !strings.HasSuffix(strings.TrimSpace(process.captures[i]), marker) {
			t.Fatalf("wait %d report=%s", i, process.captures[i])
		}
	}
	if firstKind == "comments" && !strings.Contains(process.agents[1], "please fix") {
		t.Fatalf("fix prompt missing first cycle report: %s", process.agents[1])
	}
}
