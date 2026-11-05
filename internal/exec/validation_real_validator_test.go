package exec

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

// realValidatorRunner runs run-validator.sh against the real Validator build
// and stubs only the repair agent.
type realValidatorRunner struct {
	t       *testing.T
	project string
	fixes   int
}

func (r *realValidatorRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	return runProcess(osexec.Command("sh", "-c", cmd), r.project, nil)
}

func (r *realValidatorRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	switch filepath.Base(path) {
	case "run-validator.sh", "check-task-compliance.sh":
		return runProcess(osexec.Command("sh", path), r.project, stdin)
	}
	r.t.Fatalf("unexpected script %s", path)
	return ProcessResult{}, nil
}

// RunScriptWithEnv receives the Validator metrics environment the runner builds
// for steps with metrics_source.
func (r *realValidatorRunner) RunScriptWithEnv(path string, stdin []byte, _ bool, _ string, environment []string) (ProcessResult, error) {
	cmd := osexec.Command("sh", path)
	cmd.Env = append(os.Environ(), environment...)
	return runProcess(cmd, r.project, stdin)
}

func (r *realValidatorRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	if !strings.Contains(options.Prefix, "fix-violations-lead") {
		r.t.Fatalf("unexpected agent step %s", options.Prefix)
	}
	r.fixes++
	return ProcessResult{Started: true, Stdout: claudeUsageOutput("fixed", 0)}, nil
}

// INT-010: the built-in agent-validator workflow drives a real Validator build.
// A passing check writes PASS; a failing check is repaired in the named
// session, then ends with FAIL and a warning-completed parent. Opt-in like
// TestLocalValidatorDeliveryIntegration, and required in CI.
func TestBuiltinAgentValidatorWorkflowAgainstRealValidator(t *testing.T) {
	entry := os.Getenv("AGENT_RUNNER_TEST_VALIDATOR_ENTRYPOINT")
	if entry == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI must set AGENT_RUNNER_TEST_VALIDATOR_ENTRYPOINT to a Validator dist/index.js built from main")
		}
		t.Skip("set AGENT_RUNNER_TEST_VALIDATOR_ENTRYPOINT to the local Validator dist/index.js")
	}
	node, err := osexec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, command, wantResult string
		wantFixes                 int
	}{
		{"passing check", "true", "PASS", 0},
		{"failing check exhausts repairs", "false", "FAIL", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			setRunnerShim(t)
			root := t.TempDir()
			project := filepath.Join(root, "project")
			if err := os.MkdirAll(filepath.Join(project, ".validator"), 0o700); err != nil {
				t.Fatal(err)
			}
			config := "base_branch: main\nlog_dir: logs\nmax_previous_logs: 0\nallow_parallel: false\ncli:\n  default_preference: [codex]\n  adapters:\n    codex:\n      allow_tool_use: false\nentry_points:\n  - path: .\n    checks:\n      - local-check:\n          command: '" + tc.command + "'\n"
			files := map[string]string{".validator/config.yml": config, ".gitignore": "logs/\n", "example.go": "package example\nconst value=1\n"}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(project, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=f", "-c", "user.email=f@example.invalid", "commit", "-m", "initial"}} {
				gitIn(t, project, args...)
			}
			if err := os.WriteFile(filepath.Join(project, "example.go"), []byte("package example\nconst value=2\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(root, "agent-validator")
			if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+node+" "+entry+" \"$@\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", wrapper)
			for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_BASE_REF", "GITHUB_SHA"} {
				t.Setenv(key, "")
			}
			t.Chdir(project)

			const ref = "builtin:core/validation-agent-validator-v1.0.yaml"
			workflow, err := loader.LoadWorkflow(ref, loader.Options{})
			if err != nil {
				t.Fatal(err)
			}
			resultFile := filepath.Join(root, "result.txt")
			ctx := model.NewRootContext(&model.RootContextOptions{
				Params:            map[string]string{"repair_session": "lead-agent", "task_file": "", "task_compliance": "false", "result_file": resultFile},
				WorkflowFile:      ref,
				SessionDir:        filepath.Join(root, "session"),
				NamedSessionDecls: map[string]string{"implementor-agent": "implementor", "lead-agent": "lead"},
			})
			ctx.AuditLogger = metrics.NewPipeline(metrics.NewCollector(ctx.SessionDir, "run", "wf", time.Now()), nil)
			runner := &realValidatorRunner{t: t, project: project}
			group := model.Step{ID: "validation", Steps: workflow.Steps}
			if _, err := DispatchStep(&group, ctx, runner, &mockGlob{}, &mockLogger{}); err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			body, err := os.ReadFile(resultFile)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.SplitN(string(body), "\n", 2)[0]; got != tc.wantResult {
				t.Errorf("result = %q, want %q (full %q)", got, tc.wantResult, body)
			}
			if runner.fixes != tc.wantFixes {
				t.Errorf("repair runs = %d, want %d", runner.fixes, tc.wantFixes)
			}
		})
	}
}

func setRunnerShim(t *testing.T) {
	t.Helper()
	jq, err := osexec.LookPath("jq")
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(t.TempDir(), "runner")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec "+jq+" -r --arg key \"$3\" '.[$key] // \"\"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_RUNNER_EXECUTABLE", shim)
}
