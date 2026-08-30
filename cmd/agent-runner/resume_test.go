package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
)

func TestSpawnAgentResume_RejectsSlashInCLI(t *testing.T) {
	err := spawnAgentResume("/usr/bin/claude", "sess")
	if err == nil {
		t.Fatal("expected error for CLI containing slash")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error: %v", err)
	}
}

func TestSpawnAgentResume_RejectsUnknownCLI(t *testing.T) {
	err := spawnAgentResume("curl", "sess")
	if err == nil {
		t.Fatal("expected error for CLI not in allowlist")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error: %v", err)
	}
}

func TestSpawnAgentResume_CLINotInPATH_ReturnsError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := spawnAgentResume("claude", "sess")
	if err == nil {
		t.Fatal("expected error when claude not in PATH")
	}
	if !strings.Contains(err.Error(), "PATH") && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "executable") {
		t.Errorf("expected PATH-related error, got: %v", err)
	}
}

func TestSpawnAgentResume_CursorAllowedByAllowlist(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := spawnAgentResume("cursor", "sess")
	if err == nil {
		t.Fatal("expected error when cursor not in PATH")
	}
	if strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected cursor to be allowed and fail on PATH lookup instead, got: %v", err)
	}
}

// TestResumeInspectPaths verifies that a resume state-file path (the caller
// for -resume) maps cleanly to the session and project directories that the
// run-view expects when a completed workflow is opened for inspection.
//
// Layout: <home>/.agent-runner/projects/<encoded>/runs/<run-id>/state.json
//
//	projectDir = <home>/.agent-runner/projects/<encoded>
//	sessionDir = projectDir/runs/<run-id>
func TestResumeInspectPaths(t *testing.T) {
	statePath := "/home/user/.agent-runner/projects/enc/runs/run-123/state.json"
	sessionDir, projectDir := resumeInspectPaths(statePath)

	wantSession := "/home/user/.agent-runner/projects/enc/runs/run-123"
	wantProject := "/home/user/.agent-runner/projects/enc"

	if sessionDir != wantSession {
		t.Errorf("sessionDir = %q, want %q", sessionDir, wantSession)
	}
	if projectDir != wantProject {
		t.Errorf("projectDir = %q, want %q", projectDir, wantProject)
	}
}

func TestResolveResumeStatePathCanonicalizesSymlinkedWorkingDirectory(t *testing.T) {
	home, logicalWorkspace, canonicalWorkspace, runID := setupSymlinkedRun(t)
	statePath := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(canonicalWorkspace), "runs", runID, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	chdirLogicalWorkspace(t, logicalWorkspace)
	got, err := resolveResumeStatePath(runID)
	if err != nil {
		t.Fatalf("resolve resume state path: %v", err)
	}
	if got != statePath {
		t.Fatalf("state path = %q, want %q", got, statePath)
	}
}

func TestResolveInspectSessionCanonicalizesSymlinkedWorkingDirectory(t *testing.T) {
	home, logicalWorkspace, canonicalWorkspace, runID := setupSymlinkedRun(t)
	projectDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(canonicalWorkspace))
	sessionDir := filepath.Join(projectDir, "runs", runID)

	chdirLogicalWorkspace(t, logicalWorkspace)
	gotSession, gotProject, err := resolveInspectSession(runID)
	if err != nil {
		t.Fatalf("resolve inspect session: %v", err)
	}
	if gotSession != sessionDir {
		t.Fatalf("session dir = %q, want %q", gotSession, sessionDir)
	}
	if gotProject != projectDir {
		t.Fatalf("project dir = %q, want %q", gotProject, projectDir)
	}
}

func setupSymlinkedRun(t *testing.T) (home, logicalWorkspace, canonicalWorkspace, runID string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinked working-directory lookup uses POSIX symlinks")
	}

	temp := t.TempDir()
	home = filepath.Join(temp, "home")
	realParent := filepath.Join(temp, "real")
	logicalParent := filepath.Join(temp, "logical")
	canonicalWorkspace = filepath.Join(realParent, "workspace")
	logicalWorkspace = filepath.Join(logicalParent, "workspace")
	if err := os.MkdirAll(canonicalWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(canonicalWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realParent, logicalParent); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	runID = "run-symlinked-workspace"
	sessionDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(canonicalWorkspace), "runs", runID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home, logicalWorkspace, canonicalWorkspace, runID
}

func chdirLogicalWorkspace(t *testing.T, logicalWorkspace string) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(logicalWorkspace); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD", logicalWorkspace)
}
