package openspecroot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	wd := t.TempDir()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "openspec", "changes", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := os.WriteFile(filepath.Join(root, file), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Resolve(wd, Input{ChangeName: "foo", Operation: "continue"}, map[string]string{wd: root})
	if err != nil {
		t.Fatal(err)
	}
	if result["spec_root"] != root || result["external"] != "true" || result["commit_plan"] != "false" || !strings.Contains(result["context_instruction"], filepath.Join(root, "AGENTS.md")) {
		t.Fatal(result)
	}
	if _, err := Resolve(wd, Input{ChangeName: "foo", Operation: "create", SpecRoot: root}, nil); err == nil {
		t.Fatal("expected collision")
	}
	if _, err := Resolve(wd, Input{ChangeName: "foo", Operation: "guard"}, map[string]string{wd: root}); err == nil {
		t.Fatal("accepted unknown operation")
	}
	result, err = Resolve(wd, Input{ChangeName: "foo", Operation: "create"}, nil)
	if err != nil || result["change_dir"] != "openspec/changes/foo" || result["context_instruction"] != "" {
		t.Fatal(result, err)
	}
	if _, err := Resolve(wd, Input{ChangeName: "../foo", Operation: "create"}, nil); err == nil {
		t.Fatal("expected invalid name")
	}
}

func TestResolveGitWorktreeAndPathExpansion(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(base, "code")
	root := filepath.Join(base, "specs", "plugins", "foo")
	other := filepath.Join(base, "other")
	for _, dir := range []string{repo, filepath.Join(root, "openspec"), filepath.Join(other, "openspec")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	git("commit", "--allow-empty", "-m", "initial")
	worktree := filepath.Join(repo, "worktrees", "feature")
	git("worktree", "add", "-b", "feature", worktree)
	link := filepath.Join(base, "code-link")
	specLink := filepath.Join(base, "spec-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, specLink); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", base)
	roots := map[string]string{link: "~/specs/plugins/foo"}
	for _, wd := range []string{repo, worktree} {
		result, err := Resolve(wd, Input{ChangeName: "foo", Operation: "create"}, roots)
		if err != nil || result["spec_root"] != root || result["source"] != "setting" {
			t.Fatal(result, err)
		}
		result, err = Resolve(wd, Input{ChangeName: "foo", Operation: "create", SpecRoot: other}, roots)
		if err != nil || result["spec_root"] != other || result["source"] != "param" {
			t.Fatal(result, err)
		}
	}
	for _, value := range []string{"../spec-link", "~/spec-link"} {
		result, err := Resolve(repo, Input{ChangeName: "foo", Operation: "create", SpecRoot: value}, roots)
		if err != nil || result["spec_root"] != root {
			t.Fatal(result, err)
		}
	}
	for _, value := range []string{filepath.Join(base, "missing"), base} {
		if _, err := Resolve(repo, Input{ChangeName: "foo", Operation: "create", SpecRoot: value}, nil); err == nil || !strings.Contains(err.Error(), "param") || !strings.Contains(err.Error(), value) {
			t.Fatal(err)
		}
	}
	if _, err := Resolve(repo, Input{ChangeName: "foo", Operation: "continue", SpecRoot: root}, nil); err == nil {
		t.Fatal("continued missing change")
	}
	archive := filepath.Join(root, "openspec", "changes", "archive", "2026-10-09-foo")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(repo, Input{ChangeName: "foo", Operation: "create", SpecRoot: root}, nil); err == nil || !strings.Contains(err.Error(), archive) {
		t.Fatal(err)
	}
}

func TestResolveArchiveNameBoundary(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "openspec", "changes", "archive")
	if err := os.MkdirAll(filepath.Join(archive, "2026-10-09-bar-foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	input := Input{ChangeName: "foo", Operation: "create", SpecRoot: root}
	if _, err := Resolve(t.TempDir(), input, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(archive, "2026-10-09-foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(t.TempDir(), input, nil); err == nil {
		t.Fatal("accepted exact archive collision")
	}
}

func TestResolveExternalInstructionWording(t *testing.T) {
	wd := t.TempDir()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Resolve(wd, Input{ChangeName: "foo", Operation: "create", SpecRoot: root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := filepath.Join(root, "openspec", "changes", "foo")
	want := map[string]string{
		"location_instruction":        "Keep every OpenSpec definition and planning artifact under `" + change + "/` in the OpenSpec project at `" + root + "`.",
		"validate_instruction":        "When an approved artifact changed, run `openspec validate --type change \"foo\"` from `" + root + "`.",
		"accept_validate_instruction": "If a specification changed, run `openspec validate --type change \"foo\"` from `" + root + "`.",
	}
	for key, text := range want {
		if result[key] != text {
			t.Errorf("%s = %q, want %q", key, result[key], text)
		}
	}
}
