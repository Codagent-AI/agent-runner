package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestHandleDebugShowWorkflow(t *testing.T) {
	t.Run("builtin ref prints embedded bytes", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := handleDebug([]string{"--show-workflow", "core:finalize-pr"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("handleDebug() = %d, stderr: %s", code, stderr.String())
		}
		want, err := builtinworkflows.ReadFile("builtin:core/finalize-pr.yaml")
		if err != nil {
			t.Fatalf("read builtin: %v", err)
		}
		if diff := cmp.Diff(string(want), stdout.String()); diff != "" {
			t.Fatalf("stdout mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("disk ref prints unnormalized bytes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := "# comment\n\nname: custom\nsteps:\n  - workflow: plan-change.yaml\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write workflow: %v", err)
		}
		var stdout, stderr bytes.Buffer
		code := handleDebug([]string{"--show-workflow", path}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("handleDebug() = %d, stderr: %s", code, stderr.String())
		}
		if stdout.String() != content {
			t.Fatalf("stdout = %q, want %q", stdout.String(), content)
		}
	})

	t.Run("unknown ref names missing ref", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := handleDebug([]string{"--show-workflow", "./missing.yaml"}, &stdout, &stderr)
		if code == 0 {
			t.Fatal("handleDebug() = 0, want non-zero")
		}
		if !strings.Contains(stderr.String(), "./missing.yaml") {
			t.Fatalf("stderr = %q, want missing ref", stderr.String())
		}
	})

	t.Run("empty ref is parse error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := handleDebug([]string{"--show-workflow", ""}, &stdout, &stderr)
		if code == 0 {
			t.Fatal("handleDebug() = 0, want non-zero")
		}
		if !strings.Contains(stderr.String(), "parse") {
			t.Fatalf("stderr = %q, want parse error", stderr.String())
		}
	})
}

func TestHandleDebugState(t *testing.T) {
	home, repo := setupDebugRunHome(t, "run-123")
	t.Setenv("HOME", home)
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := handleDebug([]string{"--state", "run-123"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("handleDebug() = %d, stderr: %s", code, stderr.String())
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	for _, field := range []string{"workflowFile", "params"} {
		if _, ok := got[field]; !ok {
			t.Fatalf("state JSON missing %q: %#v", field, got)
		}
	}
	if _, ok := got["status"]; ok {
		t.Fatalf("state JSON injected status field: %#v", got)
	}
	if got["workflowFile"] != "workflow.yaml" {
		t.Fatalf("workflowFile = %v, want workflow.yaml", got["workflowFile"])
	}
}

func TestHandleDebugStatePreservesRawStateJSON(t *testing.T) {
	home, repo := setupDebugRunHome(t, "run-123")
	t.Setenv("HOME", home)
	t.Chdir(repo)
	sessionDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repo), "runs", "run-123")
	raw := `{"workflowFile":"workflow.yaml","params":{"task":"debug"},"status":"failed","customField":true}`
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write raw state: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := handleDebug([]string{"--state", "run-123"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("handleDebug() = %d, stderr: %s", code, stderr.String())
	}
	if stdout.String() != raw {
		t.Fatalf("stdout = %q, want raw state JSON %q", stdout.String(), raw)
	}
}

func TestHandleDebugStateUnknownRun(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := handleDebug([]string{"--state", "missing-run"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("handleDebug() = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "missing-run") {
		t.Fatalf("stderr = %q, want missing run id", stderr.String())
	}
}

func TestHandleDebugAuditSummary(t *testing.T) {
	home, repo := setupDebugRunHome(t, "run-123")
	t.Setenv("HOME", home)
	t.Chdir(repo)
	sessionDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repo), "runs", "run-123")
	logPath := filepath.Join(sessionDir, "audit.log")
	originalLog := auditLineForDebugTest(t, audit.Event{
		Timestamp: "2026-05-24T10:00:00Z",
		Prefix:    "[triage]",
		Type:      audit.EventError,
		Data:      map[string]any{"message": "failed with ghp_AbC123XyZ"},
	})
	if err := os.WriteFile(logPath, []byte(originalLog), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := handleDebug([]string{"--audit-summary", "run-123"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("handleDebug() = %d, stderr: %s", code, stderr.String())
	}

	var got struct {
		Path   string `json:"path"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Truncated          bool `json:"truncated"`
		DroppedEventsCount int  `json:"dropped_events_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.Path != logPath {
		t.Fatalf("path = %q, want %q", got.Path, logPath)
	}
	if got.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if got.DroppedEventsCount != 0 {
		t.Fatalf("dropped = %d, want 0", got.DroppedEventsCount)
	}
	if len(got.Errors) != 1 || got.Errors[0].Message != "failed with <REDACTED>" {
		t.Fatalf("errors = %#v, want redacted error", got.Errors)
	}
	after, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log after summary: %v", err)
	}
	if string(after) != originalLog {
		t.Fatal("audit log was modified by summary command")
	}
}

func TestHandleDebugAuditSummaryMissingLog(t *testing.T) {
	home, repo := setupDebugRunHome(t, "run-123")
	t.Setenv("HOME", home)
	t.Chdir(repo)
	sessionDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repo), "runs", "run-123")

	var stdout, stderr bytes.Buffer
	code := handleDebug([]string{"--audit-summary", "run-123"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("handleDebug() = %d, stderr: %s", code, stderr.String())
	}
	var got struct {
		Path               string          `json:"path"`
		Steps              json.RawMessage `json:"steps"`
		Truncated          bool            `json:"truncated"`
		DroppedEventsCount int             `json:"dropped_events_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.Path != filepath.Join(sessionDir, "audit.log") {
		t.Fatalf("path = %q, want missing audit log path", got.Path)
	}
	if got.Truncated || got.DroppedEventsCount != 0 {
		t.Fatalf("summary truncation = %v/%d, want false/0", got.Truncated, got.DroppedEventsCount)
	}
}

func TestRouteDebugCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := routeDebugCommand([]string{"debug", "--show-workflow", "core:finalize-pr"}, &stdout, &stderr)
	if !handled {
		t.Fatal("handled = false, want true")
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr.String())
	}

	handled, _ = routeDebugCommand([]string{"core:finalize-pr"}, &stdout, &stderr)
	if handled {
		t.Fatal("handled = true for non-debug args, want false")
	}
}

func setupDebugRunHome(t *testing.T, runID string) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	repo = t.TempDir()
	sessionDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repo), "runs", runID)
	state := &model.RunState{
		WorkflowFile: "workflow.yaml",
		WorkflowName: "workflow",
		CurrentStep:  model.CurrentStep{StepID: "triage"},
		Params:       map[string]string{"task": "debug"},
	}
	if err := stateio.WriteState(state, sessionDir); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return home, repo
}

func auditLineForDebugTest(t *testing.T, event audit.Event) string {
	t.Helper()
	data, err := json.Marshal(event.Data)
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}
	prefix := ""
	if event.Prefix != "" {
		prefix = " " + event.Prefix
	}
	return event.Timestamp + prefix + " " + string(event.Type) + " " + string(data) + "\n"
}
