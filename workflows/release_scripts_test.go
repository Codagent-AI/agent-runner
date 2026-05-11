package builtinworkflows

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseInfoOnMainClassifiesMergedPRsSinceLastTag(t *testing.T) {
	repo := newReleaseScriptRepo(t)
	writeFakeReleaseGH(t, repo.binDir, `[
		{"number":31,"title":"feat: add release skill","mergedAt":"2026-05-01T00:00:00Z","labels":[]},
		{"number":32,"title":"fix: stop auto release","mergedAt":"2026-05-02T00:00:00Z","labels":[]},
		{"number":33,"title":"refactor!: change release state","mergedAt":"2026-05-03T00:00:00Z","labels":[]},
		{"number":34,"title":"chore: release v0.8.0","mergedAt":"2026-05-04T00:00:00Z","labels":[]}
	]`, "")

	out := runReleaseScript(t, repo, releaseInfoScript(t))

	var got releaseInfoResult
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode release info: %v\n%s", err, out)
	}
	if got.CurrentVersion != "0.7.0" {
		t.Fatalf("current_version = %q, want 0.7.0", got.CurrentVersion)
	}
	if got.NewVersion != "1.0.0" {
		t.Fatalf("new_version = %q, want 1.0.0\n%s", got.NewVersion, out)
	}
	if got.Major[0].Number != 33 || got.Minor[0].Number != 31 || got.Patch[0].Number != 32 {
		t.Fatalf("classification = major=%v minor=%v patch=%v", got.Major, got.Minor, got.Patch)
	}
}

func TestReleaseInfoOnBranchIncludesCurrentBranchPR(t *testing.T) {
	repo := newReleaseScriptRepo(t)
	runGit(t, repo.workdir, "switch", "-c", "feature/release-skill")
	mustWriteFile(t, filepath.Join(repo.workdir, "branch.txt"), "branch change\n")
	runGit(t, repo.workdir, "add", "branch.txt")
	runGit(t, repo.workdir, "commit", "-m", "feat: branch work")
	writeFakeReleaseGH(t, repo.binDir, `[
		{"number":31,"title":"fix: merged fix","mergedAt":"2026-05-01T00:00:00Z","labels":[]}
	]`, `{"number":35,"title":"feat: release current branch","labels":[]}`)

	out := runReleaseScript(t, repo, releaseInfoScript(t))

	var got releaseInfoResult
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode release info: %v\n%s", err, out)
	}
	if got.NewVersion != "0.8.0" {
		t.Fatalf("new_version = %q, want 0.8.0\n%s", got.NewVersion, out)
	}
	if len(got.Minor) != 1 || got.Minor[0].Number != 35 {
		t.Fatalf("minor PRs = %v, want current branch PR 35", got.Minor)
	}
	if len(got.Patch) != 1 || got.Patch[0].Number != 31 {
		t.Fatalf("patch PRs = %v, want merged PR 31", got.Patch)
	}
}

func TestCreateReleasePROnMainCreatesReleaseBranch(t *testing.T) {
	repo := newReleaseScriptRepo(t)
	changelog := filepath.Join(repo.workdir, "release-changelog.md")
	mustWriteFile(t, changelog, "## 0.8.0\n\n### Minor Changes\n\n- [#35](https://github.com/Codagent-AI/agent-runner/pull/35) Add release skill\n")
	writeFakeReleaseGH(t, repo.binDir, "[]", "")

	out := runReleaseScript(t, repo, createReleasePRScript(t), "0.8.0", changelog)

	if got := strings.TrimSpace(string(out)); got != "https://github.com/Codagent-AI/agent-runner/pull/99" {
		t.Fatalf("script output = %q, want PR URL", got)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(repo.workdir, "VERSION"))); got != "0.8.0" {
		t.Fatalf("VERSION = %q, want 0.8.0", got)
	}
	if got := currentBranch(t, repo.workdir); got != "release/v0.8.0" {
		t.Fatalf("branch = %q, want release/v0.8.0", got)
	}
	log := string(runGitOutput(t, repo.workdir, "log", "-1", "--format=%s"))
	if strings.TrimSpace(log) != "chore: release v0.8.0" {
		t.Fatalf("commit subject = %q", log)
	}
}

type releaseInfoResult struct {
	CurrentVersion string      `json:"current_version"`
	NewVersion     string      `json:"new_version"`
	Major          []releasePR `json:"major"`
	Minor          []releasePR `json:"minor"`
	Patch          []releasePR `json:"patch"`
}

type releasePR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type releaseScriptRepo struct {
	workdir string
	origin  string
	binDir  string
}

func newReleaseScriptRepo(t *testing.T) releaseScriptRepo {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	workdir := filepath.Join(root, "work")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	runGit(t, root, "init", "--bare", origin)
	runGit(t, root, "clone", origin, workdir)
	runGit(t, workdir, "config", "user.name", "Test User")
	runGit(t, workdir, "config", "user.email", "test@example.com")
	mustWriteFile(t, filepath.Join(workdir, "README.md"), "# test\n")
	mustWriteFile(t, filepath.Join(workdir, "VERSION"), "0.7.0\n")
	mustWriteFile(t, filepath.Join(workdir, "CHANGELOG.md"), "# agent-runner\n\n## 0.7.0\n\n### Minor Changes\n\n- Existing release\n")
	runGit(t, workdir, "add", "README.md", "VERSION", "CHANGELOG.md")
	runGit(t, workdir, "commit", "-m", "chore: initial release")
	runGit(t, workdir, "branch", "-M", "main")
	runGit(t, workdir, "tag", "v0.7.0")
	runGit(t, workdir, "push", "-u", "origin", "main", "--tags")
	return releaseScriptRepo{workdir: workdir, origin: origin, binDir: binDir}
}

func writeFakeReleaseGH(t *testing.T, binDir, mergedPRs, branchPR string) {
	t.Helper()
	writeFakeBinary(t, binDir, "gh", `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  cat <<'JSON'
`+mergedPRs+`
JSON
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  if [ "$#" -ge 3 ] && [ "$3" != "--json" ]; then
    printf 'https://github.com/Codagent-AI/agent-runner/pull/99\n'
    exit 0
  fi
  if [ -z '`+branchPR+`' ]; then
    printf 'no pull request found\n' >&2
    exit 1
  fi
  cat <<'JSON'
`+branchPR+`
JSON
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  printf 'https://github.com/Codagent-AI/agent-runner/pull/99\n'
  exit 0
fi
printf 'unexpected gh invocation: %s\n' "$*" >&2
exit 99
`)
}

func runReleaseScript(t *testing.T, repo releaseScriptRepo, script string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = repo.workdir
	cmd.Env = append(os.Environ(), "PATH="+repo.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", filepath.Base(script), err, out)
	}
	return out
}

func releaseInfoScript(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", ".claude", "skills", "release", "scripts", "release-info.sh"))
	if err != nil {
		t.Fatalf("resolve release-info.sh: %v", err)
	}
	return path
}

func createReleasePRScript(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", ".claude", "skills", "release", "scripts", "create-release-pr.sh"))
	if err != nil {
		t.Fatalf("resolve create-release-pr.sh: %v", err)
	}
	return path
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runGitOutput(t, dir, args...)
}

func runGitOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(string(runGitOutput(t, dir, "branch", "--show-current")))
}
