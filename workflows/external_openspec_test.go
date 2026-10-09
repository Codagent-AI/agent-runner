package builtinworkflows

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"gopkg.in/yaml.v3"
)

func TestExternalEntryWiring(t *testing.T) {
	for _, name := range []string{"change", "simple-change", "plan-change", "implement-change"} {
		old, _ := ReadFile("builtin:openspec/" + name + "-v2.0.yaml")
		data, err := ReadFile("builtin:openspec/" + name + "-v2.1.yaml")
		if err != nil {
			t.Fatal(err)
		}
		var previous, current model.Workflow
		if err := yaml.Unmarshal(old, &previous); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(data, &current); err != nil {
			t.Fatal(err)
		}
		if current.Engine != nil || current.Steps[0].ID != "resolve-openspec-root" || current.Steps[len(current.Steps)-1].ID != "report-spec-changes" {
			t.Fatal(current)
		}
		for _, step := range previous.Steps {
			found := false
			for _, next := range current.Steps {
				if next.ID == step.ID {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s missing %s", name, step.ID)
			}
		}
		if strings.Contains(string(data), "openspec/changes/") {
			t.Fatal("literal artifact path", name)
		}
		ref, err := Resolve("openspec:" + name)
		if err != nil || !strings.HasSuffix(ref, "-v2.1.yaml") {
			t.Fatal(ref, err)
		}
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

func TestExternalArchiveRecovery(t *testing.T) {
	root := t.TempDir()
	session := t.TempDir()
	bin := t.TempDir()
	for _, dir := range []string{"openspec/changes/foo", "openspec/specs", "openspec/changes/archive/2026-10-09-bar-foo"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"changed", "deleted", "same"} {
		if err := os.WriteFile(filepath.Join(root, "openspec", "specs", name), []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fake := `#!/bin/sh
set -eu
if [ "$1" = validate ]; then exit 0; fi
if [ -f interrupt ]; then exit 1; fi
mkdir -p openspec/changes/archive
mv openspec/changes/foo openspec/changes/archive/2026-10-09-foo
printf updated > openspec/specs/changed
printf added > openspec/specs/added
rm openspec/specs/deleted
`
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{"change_name": "foo", "spec_root": root, "session_dir": session}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH")}
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runExternalScript(t, "verify-archive-external.sh", payload, root, env); err != nil {
		t.Fatal(out, err)
	}
	// A completed transition may be retried without calling archive again.
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err != nil {
		t.Fatal(out, err)
	}
	out, err := runExternalScript(t, "report-spec-changes.sh", payload, root, env)
	if err != nil || !strings.Contains(out, "deleted") || !strings.Contains(out, "added") || strings.Contains(out, "same") {
		t.Fatal(out, err)
	}
	// A duplicate active directory must not cause a completed archive to run again.
	if err := os.Mkdir(filepath.Join(root, "openspec", "changes", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err == nil || !strings.Contains(out, "both active change and archive exist") {
		t.Fatal(out, err)
	}
	if err := os.Remove(filepath.Join(root, "openspec", "changes", "foo")); err != nil {
		t.Fatal(err)
	}
	payload["session_dir"] = t.TempDir()
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err == nil || !strings.Contains(out, "2026-10-09-foo") {
		t.Fatal(out, err)
	}
	payload["session_dir"] = session
	if err := os.WriteFile(filepath.Join(root, "foreign"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runExternalScript(t, "verify-archive-external.sh", payload, root, env); err == nil || !strings.Contains(out, "foreign") {
		t.Fatal(out, err)
	}
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
	session := t.TempDir()
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
	out, err := runExternalScript(t, "report-spec-changes.sh", map[string]string{"spec_root": root, "session_dir": session, "change_name": "foo"}, repo, nil)
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

func TestSpecLeakGuard(t *testing.T) {
	for _, location := range []string{"clean", "prefix", "dot-prefix", "relative", "commit", "commit-period", "file", "file-url", "title", "title-url", "body", "body-period"} {
		t.Run(location, func(t *testing.T) {
			repo := t.TempDir()
			root := t.TempDir()
			bin := t.TempDir()
			remote := filepath.Join(t.TempDir(), "remote.git")
			runGit(t, repo, "init", "-b", "main")
			runGit(t, repo, "config", "user.name", "Test")
			runGit(t, repo, "config", "user.email", "test@example.com")
			runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
			runGit(t, repo, "init", "--bare", remote)
			runGit(t, repo, "remote", "add", "origin", remote)
			runGit(t, repo, "push", "origin", "main")
			runGit(t, repo, "checkout", "-b", "feature")
			message := "feature"
			contents := "safe"
			title, body := "safe", "safe"
			switch location {
			case "prefix":
				contents = root + "-code/file"
			case "dot-prefix":
				contents = root + ".backup/file"
			case "relative":
				contents = "specs describe behavior"
			case "commit":
				message = root
			case "commit-period":
				message = "see " + root + "."
			case "file-url":
				contents = "file://" + root + "/openspec/foo"
			case "title-url":
				title = "file://" + root
			case "body-period":
				body = "see " + root + "."
			case "file":
				contents = root + "/openspec/foo"
			case "title":
				title = root
			case "body":
				body = root
			}
			if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			runGit(t, repo, "add", "file.txt")
			runGit(t, repo, "commit", "-m", message)
			data, err := json.Marshal(map[string]string{"title": title, "body": body})
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(bin, "pr.json")
			if err := os.WriteFile(fixture, data, 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_LOG"
if [ "$4" = number ]; then printf '{"number":1}'; else cat "$PR_FIXTURE"; fi
`
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			head := string(runGitOutput(t, repo, "rev-parse", "HEAD"))
			out, err := runExternalScript(t, "check-spec-leak.sh", map[string]string{"spec_root": root, "spec_root_input": "specs"}, repo, []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "PR_FIXTURE=" + fixture, "GH_LOG=" + filepath.Join(bin, "gh.log")})
			if location == "clean" || location == "prefix" || location == "dot-prefix" || location == "relative" {
				if err != nil {
					t.Fatal(out, err)
				}
			} else {
				label := map[string]string{"commit": "commit ", "commit-period": "commit ", "file": "file file.txt", "file-url": "file file.txt", "title": "PR title", "title-url": "PR title", "body": "PR body", "body-period": "PR body"}[location]
				if err == nil || !strings.Contains(out, label) {
					t.Fatal(out, err)
				}
			}
			if head != string(runGitOutput(t, repo, "rev-parse", "HEAD")) {
				t.Fatal("rewrote history")
			}
		})
	}
}

func TestSpecLeakGuardFindsNonMainDefaultBranch(t *testing.T) {
	for _, mode := range []string{"github-default", "init-default"} {
		t.Run(mode, func(t *testing.T) {
			repo := t.TempDir()
			root := t.TempDir()
			bin := t.TempDir()
			runGit(t, repo, "init", "-b", "develop")
			runGit(t, repo, "config", "user.name", "Test")
			runGit(t, repo, "config", "user.email", "test@example.com")
			runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
			if mode == "github-default" {
				remote := filepath.Join(t.TempDir(), "remote.git")
				runGit(t, repo, "init", "--bare", remote)
				runGit(t, repo, "remote", "add", "origin", remote)
				runGit(t, repo, "push", "origin", "develop")
			} else {
				runGit(t, repo, "config", "init.defaultBranch", "develop")
			}
			runGit(t, repo, "checkout", "-b", "feature")
			if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte(root+"/openspec/foo"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGit(t, repo, "add", "file.txt")
			runGit(t, repo, "commit", "-m", "feature")
			fake := `#!/bin/sh
set -eu
if [ "$1" = repo ]; then printf 'develop\n'; exit 0; fi
printf 'no pull requests found for branch "feature"\n' >&2
exit 1
`
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			out, err := runExternalScript(t, "check-spec-leak.sh", map[string]string{"spec_root": root}, repo, []string{"PATH=" + bin + ":" + os.Getenv("PATH")})
			if err == nil || !strings.Contains(out, "file file.txt") {
				t.Fatalf("want leak in file.txt using develop as base, got err=%v\n%s", err, out)
			}
		})
	}
}

func TestSpecLeakGuardPRLookup(t *testing.T) {
	for _, mode := range []string{"no-pr", "gh-error", "no-remote"} {
		t.Run(mode, func(t *testing.T) {
			repo := t.TempDir()
			root := t.TempDir()
			bin := t.TempDir()
			runGit(t, repo, "init", "-b", "main")
			runGit(t, repo, "config", "user.name", "Test")
			runGit(t, repo, "config", "user.email", "test@example.com")
			runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
			if mode != "no-remote" {
				remote := filepath.Join(t.TempDir(), "remote.git")
				runGit(t, repo, "init", "--bare", remote)
				runGit(t, repo, "remote", "add", "origin", remote)
				runGit(t, repo, "push", "origin", "main")
			}
			runGit(t, repo, "checkout", "-b", "feature")
			runGit(t, repo, "commit", "--allow-empty", "-m", "feature")
			fake := `#!/bin/sh
if [ "$1" = repo ]; then printf 'main\n'; exit 0; fi
if [ "$MODE" = no-pr ]; then printf 'no pull requests found for branch "feature"\n' >&2; exit 1; fi
if [ "$MODE" = no-remote ]; then printf 'unexpected gh call\n' >&2; exit 1; fi
printf 'HTTP 401: Bad credentials\n' >&2
exit 1
`
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			out, err := runExternalScript(t, "check-spec-leak.sh", map[string]string{"spec_root": root}, repo, []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "MODE=" + mode})
			if mode == "gh-error" {
				if err == nil || !strings.Contains(out, "Bad credentials") {
					t.Fatalf("want failure naming the gh error, got err=%v\n%s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("want pass with no PR to scan, got err=%v\n%s", err, out)
			}
		})
	}
}

func TestExternalArchiveInterrupted(t *testing.T) {
	root := t.TempDir()
	session := t.TempDir()
	bin := t.TempDir()
	crash := filepath.Join(bin, "crash")
	if err := os.MkdirAll(filepath.Join(root, "openspec", "changes", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crash, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$CALLS"
if [ "$1" = validate ]; then exit 0; fi
if [ -f "$CRASH" ]; then exit 1; fi
mkdir -p openspec/changes/archive
mv openspec/changes/foo openspec/changes/archive/2026-10-09-foo
`
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(bin, "calls")
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "CRASH=" + crash, "CALLS=" + calls}
	payload := map[string]string{"change_name": "foo", "spec_root": root, "session_dir": session}
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err == nil {
		t.Fatal(out)
	}
	if out, err := runExternalScript(t, "verify-archive-external.sh", payload, root, env); err == nil || !strings.Contains(out, "active change directory still exists") {
		t.Fatal(out, err)
	}
	if err := os.Remove(crash); err != nil {
		t.Fatal(err)
	}
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runExternalScript(t, "verify-archive-external.sh", payload, root, env); err != nil {
		t.Fatal(out, err)
	}
	before, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err != nil {
		t.Fatal(out, err)
	}
	after, err := os.ReadFile(calls)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retry called openspec", err)
	}
	payload["change_name"] = "missing"
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err == nil || !strings.Contains(out, "missing active change directory") {
		t.Fatal(out, err)
	}
	payload["change_name"] = "foo"
	recordPath := filepath.Join(session, "output", "archive-transition", "foo-external.json")
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record["run_id"] = "another-run"
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runExternalScript(t, "archive-external.sh", payload, root, env); err == nil || !strings.Contains(out, "mismatched archive transition record") {
		t.Fatal(out, err)
	}
}

func TestOlderOpenSpecVersionsUnchanged(t *testing.T) {
	hashes := map[string]string{
		"change-v2.0.yaml":           "af5a1c3790b88fb69bcccb9178cc83ec3667d048facf8f9ce45c18cc0e37b74b",
		"simple-change-v2.0.yaml":    "b6d999ed0639e7cbc1acc68c71d32f4a51233ca51ea1ceaa096fd9f4edb23771",
		"plan-change-v2.0.yaml":      "d59eb466a30902ca6fd0c3a6262951451dfbef63f1e875009aa5d4ec970e956f",
		"implement-change-v2.0.yaml": "5ed91065cdb771d236f3b2fa1c20f0a6dea2f9be2ca7b7f1defdc669503e4058",
		"archive-change-v1.0.yaml":   "bda26dac45482c942e98ce682c3de2a807e66567e4ebdbb3f2aca22fb1337d94",
	}
	for name, want := range hashes {
		body, err := ReadFile("builtin:openspec/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != want {
			t.Fatalf("historical workflow %s changed", name)
		}
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

func TestArchiveSnapshotStreaming(t *testing.T) {
	root := t.TempDir()
	excluded := filepath.Join(root, "openspec", "changes", "foo")
	for _, dir := range []string{excluded, filepath.Join(root, ".git"), filepath.Join(root, "node_modules")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data := bytes.Repeat([]byte("large binary\x00"), 200000)
	for _, path := range []string{filepath.Join(root, "node_modules", "binary"), filepath.Join(excluded, "ignored"), filepath.Join(root, ".git", "ignored")} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	helper, err := filepath.Abs("openspec")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "-B", "-c", `
import sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from archive_snapshot import snapshot
original = Path.open
class BoundedReader:
    def __init__(self, file): self.file = file
    def __enter__(self): return self
    def __exit__(self, *args): self.file.close()
    def read(self, size=-1):
        assert 0 < size <= 1024 * 1024, size
        return self.file.read(size)
Path.open = lambda path, *args, **kwargs: BoundedReader(original(path, *args, **kwargs))
canonical, other = snapshot(Path(sys.argv[2]), Path(sys.argv[3]))
assert canonical == {}, canonical
assert other == {'node_modules/binary': sys.argv[4]}, other
`, helper, root, excluded, fmt.Sprintf("%x", sha256.Sum256(data)))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
}
