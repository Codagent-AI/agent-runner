package profilewrite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestWriteMergesTwoAgentShapeAndPreservesExistingConfig(t *testing.T) {
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

	err := Write(&Request{
		TargetPath:       target,
		InteractiveCLI:   "claude",
		InteractiveModel: "opus",
		HeadlessCLI:      "codex",
		HeadlessModel:    "gpt-5",
	})
	if err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	var got struct {
		Profiles map[string]struct {
			Agents map[string]map[string]any `yaml:"agents"`
		} `yaml:"profiles"`
		OtherTopLevel bool `yaml:"other_top_level"`
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if err := yaml.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal target: %v", err)
	}
	defaultAgents := got.Profiles["default"].Agents
	for _, want := range []string{"planner", "implementor", "team_implementor", "summarizer"} {
		if _, ok := defaultAgents[want]; !ok {
			t.Fatalf("default agents missing %q in %#v", want, defaultAgents)
		}
	}
	for _, absent := range []string{"interactive_base", "headless_base"} {
		if _, ok := defaultAgents[absent]; ok {
			t.Fatalf("default agents should not contain %q", absent)
		}
	}
	if _, ok := got.Profiles["team"]; !ok || !got.OtherTopLevel {
		t.Fatalf("existing config was not preserved: %#v", got)
	}
	if diff := cmp.Diff(map[string]any{"default_mode": "interactive", "cli": "claude", "model": "opus"}, defaultAgents["planner"]); diff != "" {
		t.Fatalf("planner mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]any{"default_mode": "headless", "cli": "codex", "model": "gpt-5"}, defaultAgents["implementor"]); diff != "" {
		t.Fatalf("implementor mismatch (-want +got):\n%s", diff)
	}
}

func TestWriteCreatesParentAnd0600File(t *testing.T) {
	target := filepath.Join(t.TempDir(), "home", ".agent-runner", "config.yaml")

	err := Write(&Request{
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
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Collisions() mismatch (-want +got):\n%s", diff)
	}
}

func TestWriteLeavesOriginalOnFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	original := "profiles:\n  default:\n    agents:\n      team: {}\n"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	err := Write(&Request{TargetPath: target, InteractiveCLI: "claude"})
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
