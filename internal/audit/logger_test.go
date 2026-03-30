package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(n int) *int { return &n }

func TestBuildPrefix(t *testing.T) {
	t.Run("builds prefix for top-level step", func(t *testing.T) {
		result := BuildPrefix(nil, "validate")
		if result != "[validate]" {
			t.Fatalf("expected '[validate]', got %q", result)
		}
	})

	t.Run("builds prefix for step inside loop", func(t *testing.T) {
		path := []NestingInfo{{StepID: "task-loop", Iteration: intPtr(0)}}
		result := BuildPrefix(path, "implement")
		if result != "[task-loop:0, implement]" {
			t.Fatalf("expected '[task-loop:0, implement]', got %q", result)
		}
	})

	t.Run("builds prefix for step inside sub-workflow inside loop", func(t *testing.T) {
		path := []NestingInfo{
			{StepID: "task-loop", Iteration: intPtr(0)},
			{StepID: "verify", SubWorkflowName: "verify-task"},
		}
		result := BuildPrefix(path, "check")
		expected := "[task-loop:0, verify, sub:verify-task, check]"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("builds prefix for plain nesting segment", func(t *testing.T) {
		path := []NestingInfo{{StepID: "parent"}}
		result := BuildPrefix(path, "child")
		if result != "[parent, child]" {
			t.Fatalf("expected '[parent, child]', got %q", result)
		}
	})
}

func TestEncodePath(t *testing.T) {
	t.Run("replaces slashes dots and underscores with dashes", func(t *testing.T) {
		result := EncodePath("/Users/paul/codagent/agent-runner")
		if result != "-Users-paul-codagent-agent-runner" {
			t.Fatalf("expected '-Users-paul-codagent-agent-runner', got %q", result)
		}
	})

	t.Run("replaces dots and underscores", func(t *testing.T) {
		result := EncodePath("my_project.v2")
		if result != "my-project-v2" {
			t.Fatalf("expected 'my-project-v2', got %q", result)
		}
	})
}

func TestSanitizeWorkflowName(t *testing.T) {
	t.Run("passes through simple names", func(t *testing.T) {
		result := SanitizeWorkflowName("deploy-service")
		if result != "deploy-service" {
			t.Fatalf("expected 'deploy-service', got %q", result)
		}
	})

	t.Run("replaces path traversal", func(t *testing.T) {
		result := SanitizeWorkflowName("../../etc/passwd")
		if strings.Contains(result, "..") {
			t.Fatalf("expected no path traversal, got %q", result)
		}
	})

	t.Run("replaces file-unsafe characters", func(t *testing.T) {
		result := SanitizeWorkflowName("my:workflow*name")
		if strings.ContainsAny(result, `:*?"<>|`) {
			t.Fatalf("expected no unsafe chars, got %q", result)
		}
	})

	t.Run("returns workflow for empty input", func(t *testing.T) {
		result := SanitizeWorkflowName("")
		if result != "workflow" {
			t.Fatalf("expected 'workflow', got %q", result)
		}
	})
}

func TestLogger(t *testing.T) {
	t.Run("writes events to log file", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "test.log")
		logger, err := NewLogger(logPath)
		if err != nil {
			t.Fatalf("create logger: %v", err)
		}

		logger.Emit(Event{
			Timestamp: "2024-01-01T00:00:00Z",
			Prefix:    "[step1]",
			Type:      EventStepStart,
			Data:      map[string]any{"command": "echo hi"},
		})
		logger.Close()

		content, _ := os.ReadFile(logPath)
		line := string(content)
		if !strings.Contains(line, "2024-01-01T00:00:00Z") {
			t.Fatal("expected timestamp")
		}
		if !strings.Contains(line, "[step1]") {
			t.Fatal("expected prefix")
		}
		if !strings.Contains(line, "step_start") {
			t.Fatal("expected event type")
		}
		if !strings.Contains(line, `"command":"echo hi"`) {
			t.Fatal("expected data")
		}
	})

	t.Run("writes events without prefix", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "test.log")
		logger, _ := NewLogger(logPath)

		logger.Emit(Event{
			Timestamp: "2024-01-01T00:00:00Z",
			Type:      EventRunStart,
			Data:      map[string]any{},
		})
		logger.Close()

		content, _ := os.ReadFile(logPath)
		line := string(content)
		// Should be "timestamp run_start {}" with no extra space for prefix
		if strings.Contains(line, "  run_start") {
			t.Fatal("expected no double space before event type")
		}
	})

	t.Run("does not write after close", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "test.log")
		logger, _ := NewLogger(logPath)
		logger.Close()

		logger.Emit(Event{
			Timestamp: "2024-01-01T00:00:00Z",
			Type:      EventStepStart,
			Data:      map[string]any{},
		})

		content, _ := os.ReadFile(logPath)
		if len(content) != 0 {
			t.Fatal("expected empty file after close")
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "nested", "deep", "test.log")
		logger, err := NewLogger(logPath)
		if err != nil {
			t.Fatalf("create logger: %v", err)
		}
		logger.Close()

		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			t.Fatal("expected log file to exist")
		}
	})
}
