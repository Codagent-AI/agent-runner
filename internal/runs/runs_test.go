package runs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

// setupSession creates a session directory with optional state.json and lock file.
func setupSession(t *testing.T, projectDir, sessionID string, opts sessionOpts) string {
	t.Helper()
	sessionDir := filepath.Join(projectDir, "runs", sessionID)
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if opts.stateJSON != nil {
		if err := stateio.WriteState(opts.stateJSON, sessionDir); err != nil {
			t.Fatal(err)
		}
	}
	if opts.lockPID != 0 {
		os.WriteFile(filepath.Join(sessionDir, "lock"), []byte(fmt.Sprintf("%d\n", opts.lockPID)), 0o600)
	}
	return sessionDir
}

type sessionOpts struct {
	stateJSON *model.RunState
	lockPID   int // 0 means no lock file
}

func TestListForDir(t *testing.T) {
	t.Run("returns nil for missing runs directory", func(t *testing.T) {
		projectDir := t.TempDir()
		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if infos != nil {
			t.Fatalf("expected nil, got %v", infos)
		}
	})

	t.Run("detects active run from live lock", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{
			lockPID: os.Getpid(), // current process is alive
			stateJSON: &model.RunState{
				WorkflowName: "deploy",
				CurrentStep:  model.CurrentStep{StepID: "build"},
				Params:       map[string]string{},
			},
		})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(infos) != 1 {
			t.Fatalf("expected 1 run, got %d", len(infos))
		}
		if infos[0].Status != StatusActive {
			t.Fatalf("expected StatusActive, got %d", infos[0].Status)
		}
		if infos[0].WorkflowName != "deploy" {
			t.Fatalf("expected workflow name 'deploy', got %q", infos[0].WorkflowName)
		}
		if infos[0].CurrentStep != "build" {
			t.Fatalf("expected current step 'build', got %q", infos[0].CurrentStep)
		}
	})

	t.Run("detects inactive run from stale lock", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{
			lockPID: 999999999, // dead PID
			stateJSON: &model.RunState{
				WorkflowName: "deploy",
				CurrentStep:  model.CurrentStep{StepID: "test"},
				Params:       map[string]string{},
			},
		})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if infos[0].Status != StatusInactive {
			t.Fatalf("expected StatusInactive, got %d", infos[0].Status)
		}
	})

	t.Run("detects inactive run from state file without lock", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{
			stateJSON: &model.RunState{
				WorkflowName: "deploy",
				CurrentStep:  model.CurrentStep{StepID: "validate"},
				Params:       map[string]string{},
			},
		})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if infos[0].Status != StatusInactive {
			t.Fatalf("expected StatusInactive, got %d", infos[0].Status)
		}
	})

	t.Run("detects completed run (no lock, no state)", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if infos[0].Status != StatusCompleted {
			t.Fatalf("expected StatusCompleted, got %d", infos[0].Status)
		}
		if infos[0].CurrentStep != "" {
			t.Fatalf("expected empty current step for completed, got %q", infos[0].CurrentStep)
		}
	})

	t.Run("parses workflow name from session ID for completed runs", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "plan-change-2026-04-11T09-14-00-000000000Z", sessionOpts{})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if infos[0].WorkflowName != "plan-change" {
			t.Fatalf("expected 'plan-change', got %q", infos[0].WorkflowName)
		}
	})

	t.Run("parses start time from session ID", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := time.Date(2026, 4, 11, 9, 14, 0, 0, time.UTC)
		if !infos[0].StartTime.Equal(expected) {
			t.Fatalf("expected start time %v, got %v", expected, infos[0].StartTime)
		}
	})

	t.Run("sorts most recent first", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-10T08-00-00-000000000Z", sessionOpts{})
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(infos) != 2 {
			t.Fatalf("expected 2 runs, got %d", len(infos))
		}
		if infos[0].StartTime.Before(infos[1].StartTime) {
			t.Fatal("expected most recent first")
		}
	})

	t.Run("reads nested current step", func(t *testing.T) {
		projectDir := t.TempDir()
		setupSession(t, projectDir, "deploy-2026-04-11T09-14-00-000000000Z", sessionOpts{
			stateJSON: &model.RunState{
				WorkflowName: "deploy",
				CurrentStep: model.CurrentStep{
					Nested: &model.NestedStepState{
						StepID:            "outer",
						SessionIDs:        map[string]string{},
						CapturedVariables: map[string]string{},
						Child: &model.NestedStepState{
							StepID:            "inner",
							SessionIDs:        map[string]string{},
							CapturedVariables: map[string]string{},
						},
					},
				},
				Params: map[string]string{},
			},
		})

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if infos[0].CurrentStep != "inner" {
			t.Fatalf("expected nested step 'inner', got %q", infos[0].CurrentStep)
		}
	})

	t.Run("skips non-directory entries", func(t *testing.T) {
		projectDir := t.TempDir()
		runsDir := filepath.Join(projectDir, "runs")
		os.MkdirAll(runsDir, 0o750)
		os.WriteFile(filepath.Join(runsDir, "stray-file.txt"), []byte("hi"), 0o600)

		infos, err := ListForDir(projectDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(infos) != 0 {
			t.Fatalf("expected 0 runs, got %d", len(infos))
		}
	})
}

func TestReadProjectPath(t *testing.T) {
	t.Run("reads path from meta.json", func(t *testing.T) {
		dir := t.TempDir()
		meta := map[string]string{"path": "/home/user/myproject"}
		data, _ := json.Marshal(meta)
		os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600)

		result := ReadProjectPath(dir)
		if result != "/home/user/myproject" {
			t.Fatalf("expected '/home/user/myproject', got %q", result)
		}
	})

	t.Run("falls back to directory basename without meta.json", func(t *testing.T) {
		dir := t.TempDir()
		result := ReadProjectPath(dir)
		want := "? " + filepath.Base(dir)
		if result != want {
			t.Fatalf("expected %q, got %q", want, result)
		}
	})

	t.Run("falls back for malformed meta.json", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "meta.json"), []byte("not json"), 0o600)

		result := ReadProjectPath(dir)
		want := "? " + filepath.Base(dir)
		if result != want {
			t.Fatalf("expected %q, got %q", want, result)
		}
	})

	t.Run("falls back for empty path in meta.json", func(t *testing.T) {
		dir := t.TempDir()
		meta := map[string]string{"path": ""}
		data, _ := json.Marshal(meta)
		os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600)

		result := ReadProjectPath(dir)
		want := "? " + filepath.Base(dir)
		if result != want {
			t.Fatalf("expected %q, got %q", want, result)
		}
	})
}

func TestParseWorkflowName(t *testing.T) {
	tests := []struct {
		sessionID string
		want      string
	}{
		{"deploy-2026-04-11T09-14-00-000000000Z", "deploy"},
		{"plan-change-2026-04-11T09-14-00-000000000Z", "plan-change"},
		{"no-timestamp-here", "no-timestamp-here"},
	}
	for _, tt := range tests {
		t.Run(tt.sessionID, func(t *testing.T) {
			got := parseWorkflowName(tt.sessionID)
			if got != tt.want {
				t.Fatalf("parseWorkflowName(%q) = %q, want %q", tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestParseStartTime(t *testing.T) {
	t.Run("parses RFC3339Nano session ID", func(t *testing.T) {
		got := parseStartTime("deploy-2026-04-11T09-14-00-000000000Z")
		expected := time.Date(2026, 4, 11, 9, 14, 0, 0, time.UTC)
		if !got.Equal(expected) {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	})

	t.Run("parses RFC3339 session ID without nanos", func(t *testing.T) {
		got := parseStartTime("deploy-2026-04-11T09-14-00Z")
		expected := time.Date(2026, 4, 11, 9, 14, 0, 0, time.UTC)
		if !got.Equal(expected) {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	})

	t.Run("returns zero time for unparseable session ID", func(t *testing.T) {
		got := parseStartTime("no-timestamp-here")
		if !got.IsZero() {
			t.Fatalf("expected zero time, got %v", got)
		}
	})
}
