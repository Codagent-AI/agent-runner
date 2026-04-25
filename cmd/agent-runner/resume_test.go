package main

import (
	"strings"
	"testing"
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
