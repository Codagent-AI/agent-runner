package builtinworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSpecCreateChangeReportsExistingChange(t *testing.T) {
	workdir := t.TempDir()
	changeDir := filepath.Join(workdir, "openspec", "changes", "demo-change")
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatalf("create existing change dir: %v", err)
	}

	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "agent-validator", `#!/bin/sh
if [ "$1" = detect ]; then
  printf 'No changes detected.\n'
  exit 2
fi
exit 1
`)
	writeFakeBinary(t, binDir, "openspec", `#!/bin/sh
printf 'openspec should not be called\n' >&2
exit 99
`)

	cmd := exec.Command("sh", openSpecCreateChangeScript(t))
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(`{"change_name":"demo-change"}`)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("create-change unexpectedly succeeded:\n%s", out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("create-change exit = %v, output:\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "OpenSpec change 'demo-change' already exists at ") {
		t.Fatalf("existing-change error missing, got:\n%s", output)
	}
	if strings.Contains(output, "Unvalidated changes detected") {
		t.Fatalf("validator error should not be reported for existing change, got:\n%s", output)
	}
	if strings.Contains(output, "openspec should not be called") {
		t.Fatalf("openspec new should not run when change dir exists, got:\n%s", output)
	}
}

func TestOpenSpecCreateChangeReportsUnvalidatedChanges(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "openspec", "changes"), 0o755); err != nil {
		t.Fatalf("create openspec changes dir: %v", err)
	}

	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "agent-validator", `#!/bin/sh
if [ "$1" = detect ]; then
  printf 'Modified files detected.\n'
  exit 0
fi
exit 1
`)
	writeFakeBinary(t, binDir, "openspec", `#!/bin/sh
printf 'openspec should not be called\n' >&2
exit 99
`)

	cmd := exec.Command("sh", openSpecCreateChangeScript(t))
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(`{"change_name":"demo-change"}`)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("create-change unexpectedly succeeded:\n%s", out)
	}
	output := string(out)
	if !strings.Contains(output, "Unvalidated changes detected. Run agent-validator before planning.") {
		t.Fatalf("validator error missing, got:\n%s", output)
	}
	if strings.Contains(output, "already exists") {
		t.Fatalf("existing-change error should not be reported for validator failure, got:\n%s", output)
	}
}

func TestOpenSpecCreateChangeCreatesMissingChangeWhenClean(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "openspec", "changes"), 0o755); err != nil {
		t.Fatalf("create openspec changes dir: %v", err)
	}

	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "agent-validator", `#!/bin/sh
if [ "$1" = detect ]; then
  printf 'No changes detected.\n'
  exit 2
fi
exit 1
`)
	writeFakeBinary(t, binDir, "openspec", `#!/bin/sh
if [ "$1" = new ] && [ "$2" = change ]; then
  printf '%s\n' "$3" > created-change-name
  exit 0
fi
exit 1
`)

	cmd := exec.Command("sh", openSpecCreateChangeScript(t))
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(`{"change_name":"demo-change"}`)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create-change failed: %v\n%s", err, out)
	}
	created, err := os.ReadFile(filepath.Join(workdir, "created-change-name"))
	if err != nil {
		t.Fatalf("openspec was not invoked: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(created)); got != "demo-change" {
		t.Fatalf("openspec change name = %q, want demo-change", got)
	}
}

func writeFakeBinary(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func openSpecCreateChangeScript(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("openspec", "create-change.sh"))
	if err != nil {
		t.Fatalf("resolve create-change.sh: %v", err)
	}
	return path
}
