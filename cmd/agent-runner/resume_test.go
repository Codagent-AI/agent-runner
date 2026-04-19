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
