package profilewrite

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWriteMergesFourAgentShapeAndPreservesExistingConfig(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".agent-runner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(target, []byte(`
profiles:
  default:
    agents:
      team_implementor:
        cli: claude
      planner:
        cli: old
      summarizer:
        cli: old-summary
  team:
    agents:
      teammate:
        cli: codex
other_top_level: true
`), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	err := Write(Request{
		TargetPath:       target,
		InteractiveCLI:   "claude",
		InteractiveModel: "opus",
		HeadlessCLI:      "codex",
		HeadlessModel:    "gpt-5",
	})
	if err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	var got map[string]any
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if err := yaml.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal target: %v", err)
	}
	defaultAgents := got["profiles"].(map[string]any)["default"].(map[string]any)["agents"].(map[string]any)
	for _, want := range []string{"interactive_base", "headless_base", "planner", "implementor", "team_implementor", "summarizer"} {
		if _, ok := defaultAgents[want]; !ok {
			t.Fatalf("default agents missing %q in %#v", want, defaultAgents)
		}
	}
	if got["profiles"].(map[string]any)["team"] == nil || got["other_top_level"] != true {
		t.Fatalf("existing config was not preserved: %#v", got)
	}
	planner := defaultAgents["planner"].(map[string]any)
	if len(planner) != 1 || planner["extends"] != "interactive_base" {
		t.Fatalf("planner = %#v", planner)
	}
	implementor := defaultAgents["implementor"].(map[string]any)
	if len(implementor) != 1 || implementor["extends"] != "headless_base" {
		t.Fatalf("implementor = %#v", implementor)
	}
}

func TestWriteCreatesParentAnd0600File(t *testing.T) {
	target := filepath.Join(t.TempDir(), "home", ".agent-runner", "config.yaml")

	err := Write(Request{
		TargetPath:     target,
		InteractiveCLI: "claude",
		HeadlessCLI:    "codex",
	})
	if err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("parent dir mode = %v, want 0755", got)
	}
	fileInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("target mode = %v, want 0600", got)
	}
}

func TestCollisionsReportsManagedEntriesOnly(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(target, []byte(`
profiles:
  default:
    agents:
      planner: {}
      implementor: {}
      team_implementor: {}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := Collisions(target)
	if err != nil {
		t.Fatalf("Collisions() returned error: %v", err)
	}
	want := []string{"implementor", "planner"}
	if !slices.Equal(got, want) {
		t.Fatalf("Collisions() = %v, want %v", got, want)
	}
}

func TestWriteLeavesOriginalOnFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	original := "profiles:\n  default:\n    agents:\n      team: {}\n"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	err := Write(Request{TargetPath: target, InteractiveCLI: "claude"})
	if err == nil || !strings.Contains(err.Error(), "headless_cli") {
		t.Fatalf("Write() error = %v, want validation error", err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(body) != original {
		t.Fatalf("target changed after failed write:\n%s", body)
	}
}
