package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReacceptanceStatusGateScript(t *testing.T) {
	script, err := ReadAsset("core/reacceptance-status-gate.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/reacceptance-status-gate.sh): %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "reacceptance-status-gate.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	run := func(t *testing.T, statusFile string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", scriptPath)
		cmd.Stdin = strings.NewReader(`{"status_file":` + strconv.Quote(statusFile) + `}`)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	runWithoutJQ := func(t *testing.T, statusFile string) (string, error) {
		t.Helper()
		binDir := t.TempDir()
		for _, name := range []string{"cat", "sed", "tail"} {
			resolved, err := exec.LookPath(name)
			if err != nil {
				t.Fatalf("resolve %s: %v", name, err)
			}
			if err := os.Symlink(resolved, filepath.Join(binDir, name)); err != nil {
				t.Fatalf("link %s: %v", name, err)
			}
		}
		cmd := exec.Command("sh", scriptPath)
		cmd.Env = []string{"PATH=" + binDir}
		cmd.Stdin = strings.NewReader(`{"status_file":` + strconv.Quote(statusFile) + `}`)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("rejects missing status", func(t *testing.T) {
		out, err := run(t, filepath.Join(t.TempDir(), "missing.txt"))
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "did not record a result") {
			t.Fatalf("output = %q, want missing-result explanation", out)
		}
	})

	t.Run("rejects failed status", func(t *testing.T) {
		statusFile := filepath.Join(t.TempDir(), "status.txt")
		if err := os.WriteFile(statusFile, []byte("REACCEPTANCE_FAILED\n"), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		out, err := run(t, statusFile)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "remains incomplete") {
			t.Fatalf("output = %q, want incomplete explanation", out)
		}
	})

	t.Run("accepts complete status", func(t *testing.T) {
		statusFile := filepath.Join(t.TempDir(), "status.txt")
		if err := os.WriteFile(statusFile, []byte("REACCEPTANCE_COMPLETE\n"), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		if out, err := run(t, statusFile); err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
	})

	t.Run("rejects invalid status", func(t *testing.T) {
		statusFile := filepath.Join(t.TempDir(), "status.txt")
		if err := os.WriteFile(statusFile, []byte("UNKNOWN\n"), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		out, err := run(t, statusFile)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "invalid re-acceptance result") {
			t.Fatalf("output = %q, want invalid-result explanation", out)
		}
	})

	t.Run("requires a JSON parser", func(t *testing.T) {
		statusFile := filepath.Join(t.TempDir(), "status.txt")
		if err := os.WriteFile(statusFile, []byte("REACCEPTANCE_COMPLETE\n"), 0o600); err != nil {
			t.Fatalf("write status: %v", err)
		}
		out, err := runWithoutJQ(t, statusFile)
		if err == nil {
			t.Fatalf("script succeeded without jq:\n%s", out)
		}
		if !strings.Contains(out, "jq is required") {
			t.Fatalf("output = %q, want missing-jq explanation", out)
		}
	})
}
