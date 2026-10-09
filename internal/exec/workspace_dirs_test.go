package exec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"

	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	"github.com/codagent/agent-runner/internal/model"
)

func TestWorkspaceDirs(t *testing.T) {
	project, _ := filepath.EvalSymlinks(t.TempDir())
	external, _ := filepath.EvalSymlinks(t.TempDir())
	ctx := &model.ExecutionContext{ProjectRoot: project, Params: map[string]string{"root": external}, WorkspaceDirTemplates: []string{"", project, "{{root}}", "{{root}}"}}
	if err := evaluateWorkspaceDirs(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if len(ctx.WorkspaceDirs) != 1 || ctx.WorkspaceDirs[0] != external {
		t.Fatal(ctx.WorkspaceDirs)
	}
	next, _ := filepath.EvalSymlinks(t.TempDir())
	ctx.Params["root"] = next
	if err := evaluateWorkspaceDirs(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if len(ctx.WorkspaceDirs) != 1 || ctx.WorkspaceDirs[0] != next {
		t.Fatal("stale workspace directories", ctx.WorkspaceDirs)
	}
	ctx.WorkspaceDirTemplates = []string{"{{openspec.spec_root}}"}
	ctx.CapturedVariables = map[string]model.CapturedValue{}
	if err := evaluateWorkspaceDirs(ctx, "before-resolver"); err == nil {
		t.Fatal("missing capture accepted")
	}
	ctx.CapturedVariables["openspec"] = model.NewCapturedMap(map[string]string{"spec_root": external})
	if err := evaluateWorkspaceDirs(ctx, "after-resolver"); err != nil {
		t.Fatal(err)
	}
	if len(ctx.WorkspaceDirs) != 1 || ctx.WorkspaceDirs[0] != external {
		t.Fatal(ctx.WorkspaceDirs)
	}
	ctx.WorkspaceDirTemplates = []string{"{{missing}}"}
	if err := evaluateWorkspaceDirs(ctx, "test"); err == nil {
		t.Fatal("undefined variable accepted")
	}
	ctx.WorkspaceDirTemplates = []string{filepath.Join(external, "missing")}
	if err := evaluateWorkspaceDirs(ctx, "test"); err == nil {
		t.Fatal("missing directory accepted")
	}
	ctx.WorkspaceDirTemplates = []string{"relative"}
	if err := evaluateWorkspaceDirs(ctx, "test"); err == nil {
		t.Fatal("relative accepted")
	}
	file := filepath.Join(external, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx.WorkspaceDirTemplates = []string{file}
	if err := evaluateWorkspaceDirs(ctx, "test"); err == nil {
		t.Fatal("file accepted")
	}
}

func TestWorkspaceDirsThroughSubWorkflow(t *testing.T) {
	for _, name := range []string{"claude", "codex", "copilot", "opencode", "cursor"} {
		t.Run(name, func(t *testing.T) {
			root, _ := filepath.EvalSymlinks(t.TempDir())
			ctx := makeCtx()
			ctx.WorkspaceDirTemplates = []string{"{{root}}"}
			ctx.Params["root"] = root
			ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: name, DefaultMode: "autonomous"}}}
			dir := filepath.Join(t.TempDir(), ".agent-runner", "workflows")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			ctx.WorkflowFile = "parent-v1.0.yaml"
			path := filepath.Join(dir, "child-v1.0.yaml")
			if err := os.WriteFile(path, []byte("name: child\nsteps:\n  - id: act\n    agent: lead\n    session: new\n    mode: autonomous\n    prompt: test\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "done"}}}
			outcome, err := ExecuteSubWorkflowStep(&model.Step{ID: "child", Workflow: "child-v1.0.yaml"}, ctx, runner, &mockGlob{}, &mockLogger{})
			if name == "cursor" {
				if outcome != OutcomeFailed || len(runner.calls) != 0 {
					t.Fatal(outcome, err, runner.calls)
				}
				return
			}
			if err != nil || outcome != OutcomeSuccess || len(runner.calls) != 1 {
				t.Fatal(outcome, err, runner.calls)
			}
			index := slices.Index(runner.calls[0], "--add-dir")
			if name == "opencode" {
				if index >= 0 {
					t.Fatal(runner.calls)
				}
				return
			}
			if index < 0 || runner.calls[0][index+1] != root {
				t.Fatal(runner.calls)
			}
		})
	}
}

func TestEngineContextBindingThroughExec(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".agent-runner", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	fake := `#!/bin/sh
set -eu
printf '%s\n' "$PWD" >> "$OPENSPEC_CALL_LOG"
if [ "$1" = status ]; then
 printf '{"changeDir":"%s/openspec/changes/foo","artifacts":[{"id":"proposal","status":"done"}]}\n' "$PWD"
else
 printf '{"changeDir":"%s/openspec/changes/foo","outputPath":"proposal.md","instruction":"write"}\n' "$PWD"
fi
`
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("OPENSPEC_CALL_LOG", log)
	child := `name: child
params:
  - name: change_name
  - name: spec_root
    required: false
engine: {type: openspec, change_param: change_name, root_param: spec_root}
steps:
  - id: nested
    workflow: nested-v1.0.yaml
    params: {change_name: "{{change_name}}"}
`
	nested := `name: nested
params:
  - name: change_name
steps:
  - id: proposal
    agent: lead
    session: new
    mode: autonomous
    prompt: make proposal
`
	for name, body := range map[string]string{"child-v1.0.yaml": child, "nested-v1.0.yaml": nested} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	for _, value := range []string{"", "relative", filepath.Join(root, "missing"), root} {
		ctx := makeCtx()
		ctx.WorkflowFile = "parent-v1.0.yaml"
		ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "claude", DefaultMode: "autonomous"}}}
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "done"}}}
		step := &model.Step{ID: "child", Workflow: "child-v1.0.yaml", Params: map[string]string{"change_name": "foo", "spec_root": value}}
		outcome, err := ExecuteSubWorkflowStep(step, ctx, runner, &mockGlob{}, &mockLogger{})
		if value != root {
			if err == nil || !strings.Contains(err.Error(), "spec_root") || len(runner.calls) != 0 {
				t.Fatal(outcome, err, runner.calls)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatal("openspec called before binding")
			}
			continue
		}
		if err != nil || outcome != OutcomeSuccess || len(runner.calls) != 1 {
			t.Fatal(outcome, err, runner.calls)
		}
		if !strings.Contains(strings.Join(runner.calls[0], " "), filepath.Join(root, "openspec", "changes", "foo", "proposal.md")) {
			t.Fatal(runner.calls)
		}
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Fields(string(data)) {
			if line != root {
				t.Fatal(string(data))
			}
		}
	}
}

func TestExternalReplyWorkspaceDirs(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	ctx := makeCtx()
	ctx.WorkspaceDirTemplates = []string{"{{root}}"}
	ctx.Params["root"] = root
	args, err := externalReplyArgs(&model.Step{ID: "reply"}, ctx, &config.ResolvedAgent{}, &cli.CodexAdapter{}, "continue", "", "/runner")
	if err != nil {
		t.Fatal(err)
	}
	index := slices.Index(args, "--add-dir")
	if index < 0 || args[index+1] != root {
		t.Fatal(args)
	}
	if _, err := externalReplyArgs(&model.Step{ID: "reply"}, ctx, &config.ResolvedAgent{}, &cli.CursorAdapter{}, "continue", "", "/runner"); err == nil || !strings.Contains(err.Error(), root) {
		t.Fatal(err)
	}
}

func TestCalledAgentWorkspaceDirs(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "done"}}}
	options := testAgentCallOptions(t.TempDir(), runner, &cli.CodexAdapter{})
	options.Context.WorkspaceDirTemplates = []string{"{{root}}"}
	options.Context.Params["root"] = root
	handler := NewAgentCallHandler(options)
	response := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "workspace-call", Payload: json.RawMessage(`{"agent":"implementor","prompt":"work"}`)}))
	if response.Error != nil || len(runner.calls) != 1 {
		t.Fatal(response, runner.calls)
	}
	index := slices.Index(runner.calls[0], "--add-dir")
	if index < 0 || runner.calls[0][index+1] != root {
		t.Fatal(runner.calls)
	}
}
