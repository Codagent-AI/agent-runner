package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVerifyTaskCommitScript(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Agent Runner Test")
	runGit(t, repo, "config", "user.email", "agent-runner@example.com")
	mustWriteFile(t, filepath.Join(repo, "file.txt"), "initial\n")
	runGit(t, repo, "add", "file.txt")
	runGit(t, repo, "commit", "-m", "initial")
	startingHead := strings.TrimSpace(string(runGitOutput(t, repo, "rev-parse", "HEAD")))

	script, err := ReadAsset("core/verify-task-commit.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/verify-task-commit.sh): %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "verify-task-commit.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	run := func(t *testing.T, head string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", scriptPath)
		cmd.Dir = repo
		cmd.Stdin = strings.NewReader(`{"starting_head":` + strconv.Quote(head) + `}`)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("rejects unchanged HEAD", func(t *testing.T) {
		out, err := run(t, startingHead)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "did not produce a commit") {
			t.Fatalf("output = %q, want missing-commit explanation", out)
		}
	})

	t.Run("rejects unknown hexadecimal starting commit", func(t *testing.T) {
		unknown := strings.Repeat("f", 40)
		out, err := run(t, unknown)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "invalid starting commit") {
			t.Fatalf("output = %q, want invalid-commit explanation", out)
		}
	})

	t.Run("rejects an empty commit", func(t *testing.T) {
		runGit(t, repo, "commit", "--allow-empty", "-m", "empty")

		out, err := run(t, startingHead)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "no tracked implementation changes") {
			t.Fatalf("output = %q, want empty-change explanation", out)
		}

		runGit(t, repo, "reset", "--hard", startingHead)
	})

	t.Run("accepts a descendant commit", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(repo, "file.txt"), "implemented\n")
		runGit(t, repo, "add", "file.txt")
		runGit(t, repo, "commit", "-m", "implement task")

		out, err := run(t, startingHead)
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "produced 1 commit") {
			t.Fatalf("output = %q, want commit count", out)
		}
	})

	t.Run("rejects rewritten history", func(t *testing.T) {
		runGit(t, repo, "checkout", "--orphan", "unrelated")
		runGit(t, repo, "rm", "-f", "file.txt")
		mustWriteFile(t, filepath.Join(repo, "other.txt"), "unrelated\n")
		runGit(t, repo, "add", "other.txt")
		runGit(t, repo, "commit", "-m", "unrelated")

		out, err := run(t, startingHead)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "not a descendant") {
			t.Fatalf("output = %q, want history explanation", out)
		}
	})
}
