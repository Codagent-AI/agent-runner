package main

import (
	"testing"
)

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
