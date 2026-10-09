package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/openspecroot"
)

func TestExternalContextInValidatorAndArchiveRepairs(t *testing.T) {
	code, _ := filepath.EvalSymlinks(t.TempDir())
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Mkdir(filepath.Join(root, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("working rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := openspecroot.Resolve(code, openspecroot.Input{ChangeName: "foo", SpecRoot: root, Operation: "guard"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"validator", "archive"} {
		t.Run(name, func(t *testing.T) {
			ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: "parent-v1.0.yaml", SessionDir: t.TempDir(), ProjectRoot: code, WorkingDir: code, SessionIDs: map[string]string{"parent": "session"}, SessionProfiles: map[string]string{"parent": "lead"}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"lead": {CLI: "claude", DefaultMode: "autonomous"}}}})
			ctx.LastSessionStepID = "parent"
			step := &model.Step{ID: name, Workflow: "builtin:core/run-validator-v1.0.yaml", Params: map[string]string{"context_instruction": resolved["context_instruction"]}}
			if name == "archive" {
				step.Workflow = "builtin:openspec/archive-change-v1.1.yaml"
				step.Params["spec_root"] = root
				step.Params["spec_external"] = "true"
				step.Params["change_name"] = "foo"
			}
			runner := &externalContextRunner{mockRunner: mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: "invalid artifact"}, {ExitCode: 0, Stdout: "fixed"}, {ExitCode: 0}, {ExitCode: 0}}}}
			outcome, err := ExecuteSubWorkflowStep(step, ctx, runner, &mockGlob{}, &mockLogger{})
			if err != nil || outcome != OutcomeSuccess {
				t.Fatal(outcome, err, runner.calls)
			}
			var prompt string
			for _, args := range runner.calls {
				if len(args) > 0 && args[0] == "claude" {
					prompt = strings.Join(args, " ")
					break
				}
			}
			for _, value := range []string{code, root, filepath.Join(root, "AGENTS.md"), "Never write the spec-root path"} {
				if !strings.Contains(prompt, value) {
					t.Fatalf("%s repair missing %s: %s", name, value, prompt)
				}
			}
			if name == "archive" && (!strings.Contains(prompt, "You may edit only `"+filepath.Join(root, "openspec", "changes", "foo")+"/`") || !strings.Contains(prompt, "never run openspec archive")) {
				t.Fatal(prompt)
			}
		})
	}
}

type externalContextRunner struct{ mockRunner }

func (r *externalContextRunner) RunScriptWithEnv(path string, stdin []byte, capture bool, workdir string, _ []string) (ProcessResult, error) {
	return r.RunScript(path, stdin, capture, workdir)
}
