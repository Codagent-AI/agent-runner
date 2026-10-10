package builtinworkflows

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/model"
	"gopkg.in/yaml.v3"
)

func TestExternalEntryWiring(t *testing.T) {
	for _, name := range []string{"change", "simple-change", "plan-change", "implement-change"} {
		ref, err := Resolve("openspec:" + name)
		if err != nil || ref != "builtin:openspec/"+name+"-v2.0.yaml" {
			t.Fatal(ref, err)
		}
		data, err := ReadFile(ref)
		if err != nil {
			t.Fatal(err)
		}
		var w model.Workflow
		if err := yaml.Unmarshal(data, &w); err != nil {
			t.Fatal(err)
		}
		if w.Engine != nil || w.Steps[0].ID != "resolve-openspec-root" || w.Steps[len(w.Steps)-1].ID != "report-spec-changes" {
			t.Fatalf("%s steps: first %s, last %s", name, w.Steps[0].ID, w.Steps[len(w.Steps)-1].ID)
		}
		if strings.Contains(string(data), "openspec/changes/") {
			t.Fatal("literal artifact path", name)
		}
	}
}

func TestSimpleChangePlanRunsEngineOnResolvedRoot(t *testing.T) {
	entry := readBuiltinWorkflowForTest(t, "builtin:openspec/simple-change-v2.0.yaml")
	var plan *model.Step
	for i := range entry.Steps {
		if entry.Steps[i].ID == "plan" {
			plan = &entry.Steps[i]
		}
	}
	if plan == nil || plan.Workflow != "simple-change-plan-v1.0.yaml" || plan.Params["spec_root"] != "{{openspec.spec_root}}" {
		t.Fatalf("plan step must call the engine-bearing body with the resolved root: %+v", plan)
	}
	body := readBuiltinWorkflowForTest(t, "builtin:openspec/simple-change-plan-v1.0.yaml")
	if body.Engine == nil || body.Engine.Type != "openspec" || body.Engine.Extras["change_param"] != "change_name" || body.Engine.Extras["root_param"] != "spec_root" {
		t.Fatalf("simple-change-plan engine: %+v", body.Engine)
	}
}

func TestArchiveChangeKeepsRepoLocalPath(t *testing.T) {
	w := readBuiltinWorkflowForTest(t, "builtin:openspec/archive-change-v1.0.yaml")
	defaults := map[string]string{}
	for _, p := range w.Params {
		if !p.IsRequired() {
			defaults[p.Name] = p.Default
		}
	}
	if diff := cmp.Diff(map[string]string{"context_instruction": "", "spec_external": "false", "spec_root": ""}, defaults); diff != "" {
		t.Fatal(diff)
	}
	var ids, skips []string
	for _, step := range w.Steps {
		ids = append(ids, step.ID)
		skips = append(skips, step.SkipIf)
	}
	local, external := "sh: test {{spec_external}} = true", "sh: test {{spec_external}} = false"
	if diff := cmp.Diff([]string{"archive-transition", "verify-archive-commit", "advance-validator-baseline", "archive-external"}, ids); diff != "" {
		t.Fatal(diff)
	}
	if diff := cmp.Diff([]string{local, local, local, external}, skips); diff != "" {
		t.Fatal(diff)
	}
}

func writeFakeOpenSpec(t *testing.T, script string) (env []string, calls string) {
	t.Helper()
	bin := t.TempDir()
	calls = filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte("#!/bin/sh\nset -eu\nprintf '%s %s\\n' \"$PWD\" \"$*\" >> \"$CALLS\"\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "CALLS=" + calls}, calls
}

func TestExternalArchive(t *testing.T) {
	code := t.TempDir()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "openspec", "changes", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	env, calls := writeFakeOpenSpec(t, `if [ "$1" = archive ]; then
  mkdir -p openspec/changes/archive
  mv "openspec/changes/$2" "openspec/changes/archive/2026-10-09-$2"
fi
`)
	payload := map[string]string{"change_name": "foo", "spec_root": root}
	if out, err := runExternalScript(t, "archive-external.sh", payload, code, env); err != nil {
		t.Fatal(out, err)
	}
	log, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	want := root + " validate --type change foo\n" + root + " archive foo --yes\n"
	if diff := cmp.Diff(want, string(log)); diff != "" {
		t.Fatal(diff)
	}
	if _, err := os.Stat(filepath.Join(root, "openspec", "changes", "archive", "2026-10-09-foo")); err != nil {
		t.Fatal(err)
	}

	// Resume after the archive completed: report it and do not call openspec again.
	out, err := runExternalScript(t, "archive-external.sh", payload, code, env)
	if err != nil || !strings.Contains(out, "already archived") {
		t.Fatal(out, err)
	}
	if again, _ := os.ReadFile(calls); string(again) != want {
		t.Fatal("resume called openspec:", string(again))
	}

	// Both an active change and its archive exist: refuse.
	if err := os.Mkdir(filepath.Join(root, "openspec", "changes", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runExternalScript(t, "archive-external.sh", payload, code, env); err == nil || !strings.Contains(out, "both") {
		t.Fatal(out, err)
	}

	for name, bad := range map[string]map[string]string{
		"invalid name":  {"change_name": "../foo", "spec_root": root},
		"relative root": {"change_name": "foo", "spec_root": "specs"},
		"not openspec":  {"change_name": "foo", "spec_root": code},
	} {
		if out, err := runExternalScript(t, "archive-external.sh", bad, code, env); err == nil {
			t.Fatal(name, out)
		}
	}
	if _, err := os.Stat(filepath.Join(code, "openspec")); !os.IsNotExist(err) {
		t.Fatal("wrote to the code root", err)
	}
}

func TestExternalReportWithoutGit(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "openspec", "changes", "foo")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{"spec_root": root, "change_name": "foo"}
	out, err := runExternalScript(t, "report-spec-changes.sh", payload, t.TempDir(), nil)
	if err != nil || !strings.Contains(out, "not under version control") || !strings.Contains(out, "openspec/changes/foo") {
		t.Fatal(out, err)
	}
	archived := filepath.Join(root, "openspec", "changes", "archive", "2026-10-09-foo")
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(active, archived); err != nil {
		t.Fatal(err)
	}
	out, err = runExternalScript(t, "report-spec-changes.sh", payload, t.TempDir(), nil)
	if err != nil || !strings.Contains(out, "openspec/changes/archive/2026-10-09-foo") {
		t.Fatal(out, err)
	}
}

func runExternalScript(t *testing.T, name string, payload map[string]string, dir string, env []string) (string, error) {
	t.Helper()
	script, _ := filepath.Abs(filepath.Join("openspec", name))
	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = strings.NewReader(string(body))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestExternalCreateAndValidate(t *testing.T) {
	code := t.TempDir()
	root := t.TempDir()
	bin := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
set -eu
printf '%s\n' "$PWD $*" >> "$LOG"
if [ "$1" = new ]; then mkdir -p "openspec/changes/$3"; fi
`
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "agent-validator"), []byte("#!/bin/sh\nexit 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(bin, "log")
	payload := map[string]string{"change_name": "foo", "spec_root": root, "spec_external": "true"}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "LOG=" + log}
	for _, name := range []string{"create-change.sh", "validate-change.sh"} {
		if out, err := runExternalScript(t, name, payload, code, env); err != nil {
			t.Fatal(out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(code, "openspec")); !os.IsNotExist(err) {
		t.Fatal("created local OpenSpec", err)
	}
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), root+" new change foo") || !strings.Contains(string(out), root+" validate --type change foo") {
		t.Fatal(string(out))
	}
}

func TestExternalReportGitSubdirectory(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.com")
	for _, name := range []string{"project/tracked", "outside"} {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(name)), []byte("initial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	head := string(runGitOutput(t, repo, "rev-parse", "HEAD"))
	for _, name := range []string{"project/tracked", "project/untracked", "outside"} {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(name)), []byte("updated"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	index, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := runExternalScript(t, "report-spec-changes.sh", map[string]string{"spec_root": root, "change_name": "foo"}, repo, nil)
	if err != nil || !strings.Contains(out, "tracked") || !strings.Contains(out, "untracked") || strings.Contains(out, "project/") || strings.Contains(out, "outside") {
		t.Fatal(out, err)
	}
	after, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(index, after) || string(runGitOutput(t, repo, "rev-parse", "HEAD")) != head {
		t.Fatal("report wrote Git state")
	}
}

func TestCoreContextPromptWiring(t *testing.T) {
	names := []string{"define-change", "plan-change", "review-tasks", "implement-change", "implement-task", "run-validator", "verify-change", "accept-change", "complete-simple-change", "finalize-pr"}
	for _, name := range names {
		w := readBuiltinWorkflowForTest(t, "builtin:core/"+name+"-v1.0.yaml")
		walkSteps(w.Steps, func(step *model.Step) {
			if step.Prompt != "" && !strings.Contains(step.Prompt, "{{context_instruction}}") {
				t.Errorf("%s/%s missing context", name, step.ID)
			}
			if step.Repair != nil && step.Repair.Prompt != "" && !strings.Contains(step.Repair.Prompt, "{{context_instruction}}") {
				t.Errorf("%s/%s repair missing context", name, step.ID)
			}
			for _, child := range names {
				if strings.HasSuffix(step.Workflow, child+"-v1.0.yaml") && step.Params["context_instruction"] != "{{context_instruction}}" {
					t.Errorf("%s/%s does not forward context", name, step.ID)
				}
			}
		})
	}
}

func TestExternalPlanningValidation(t *testing.T) {
	code := t.TempDir()
	root := t.TempDir()
	bin := t.TempDir()
	change := filepath.Join(root, "openspec", "changes", "foo")
	if err := os.MkdirAll(filepath.Join(change, "specs", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"proposal.md", "design.md", "test-plan.md", "specs/example/spec.md"} {
		if err := os.WriteFile(filepath.Join(change, filepath.FromSlash(name)), []byte("approved artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(bin, "log")
	fake := "#!/bin/sh\nprintf '%s %s\\n' \"$PWD\" \"$*\" >> \"$LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{"change_name": "foo", "change_dir": change, "spec_root": root, "change_kind": "openspec", "require_tasks": "false"}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "LOG=" + log}
	if out, err := runExternalScript(t, "../core/validate-planning-artifacts.sh", payload, code, env); err != nil {
		t.Fatal(out, err)
	}
	data, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), root+" validate --type change foo") {
		t.Fatal(string(data), err)
	}
	payload["spec_root"] = code
	if out, err := runExternalScript(t, "../core/validate-planning-artifacts.sh", payload, code, env); err == nil || !strings.Contains(out, "change_dir must be") {
		t.Fatal(out, err)
	}
}
