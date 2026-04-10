package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrGenerate_CreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agent-runner", "config.yaml")

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should have been created.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to be created")
	}

	// Should have four default profiles.
	if len(cfg.Profiles) != 4 {
		t.Fatalf("expected 4 profiles, got %d", len(cfg.Profiles))
	}

	// Verify interactive_base.
	ib := cfg.Profiles["interactive_base"]
	if ib == nil {
		t.Fatal("expected interactive_base profile")
	}
	if ib.DefaultMode != "interactive" || ib.CLI != "claude" || ib.Model != "opus" || ib.Effort != "high" {
		t.Fatalf("unexpected interactive_base: %+v", ib)
	}

	// Verify headless_base.
	hb := cfg.Profiles["headless_base"]
	if hb == nil {
		t.Fatal("expected headless_base profile")
	}
	if hb.DefaultMode != "headless" || hb.CLI != "claude" || hb.Model != "opus" || hb.Effort != "high" {
		t.Fatalf("unexpected headless_base: %+v", hb)
	}

	// Verify planner extends interactive_base.
	pl := cfg.Profiles["planner"]
	if pl == nil || pl.Extends != "interactive_base" {
		t.Fatalf("expected planner to extend interactive_base, got %+v", pl)
	}

	// Verify implementor extends headless_base.
	im := cfg.Profiles["implementor"]
	if im == nil || im.Extends != "headless_base" {
		t.Fatalf("expected implementor to extend headless_base, got %+v", im)
	}
}

func TestLoadOrGenerate_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  custom:
    default_mode: headless
    cli: codex
    model: o3
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(cfg.Profiles))
	}
	p := cfg.Profiles["custom"]
	if p == nil || p.DefaultMode != "headless" || p.CLI != "codex" || p.Model != "o3" {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestLoadOrGenerate_AllFieldsSpecified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  full:
    default_mode: interactive
    cli: claude
    model: opus
    effort: high
    system_prompt: be helpful
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := cfg.Profiles["full"]
	if p.DefaultMode != "interactive" || p.CLI != "claude" || p.Model != "opus" || p.Effort != "high" || p.SystemPrompt != "be helpful" {
		t.Fatalf("expected all fields loaded, got %+v", p)
	}
}

func TestLoadOrGenerate_OptionalFieldsOmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  minimal:
    default_mode: headless
    cli: claude
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := cfg.Profiles["minimal"]
	if p.Model != "" || p.Effort != "" || p.SystemPrompt != "" {
		t.Fatalf("expected optional fields to be empty, got %+v", p)
	}
}

func TestLoadOrGenerate_UnrecognizedFieldIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  test:
    default_mode: interactive
    cli: claude
    unknown_field: should be ignored
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Profiles["test"] == nil {
		t.Fatal("expected test profile to be loaded")
	}
}

func TestValidation_BaseProfileMissingDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  bad:
    cli: claude
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "default_mode") {
		t.Fatalf("expected error about default_mode, got: %v", err)
	}
}

func TestValidation_BaseProfileMissingCLI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  bad:
    default_mode: headless
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cli") {
		t.Fatalf("expected error about cli, got: %v", err)
	}
}

func TestValidation_ChildProfileOmitsFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  parent:
    default_mode: interactive
    cli: claude
  child:
    extends: parent
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidation_InvalidEffort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  bad:
    default_mode: interactive
    cli: claude
    effort: maximum
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid effort") {
		t.Fatalf("expected error about invalid effort, got: %v", err)
	}
}

func TestValidation_InvalidDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  bad:
    default_mode: auto
    cli: claude
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid default_mode") {
		t.Fatalf("expected error about invalid default_mode, got: %v", err)
	}
}

func TestValidation_ExtendsNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  child:
    extends: nonexistent
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected error about nonexistent parent, got: %v", err)
	}
}

func TestValidation_CycleDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  a:
    extends: b
  b:
    extends: a
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func TestResolve_ChildOverridesOneField(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]*Profile{
			"parent": {
				DefaultMode:  "interactive",
				CLI:          "claude",
				Model:        "opus",
				Effort:       "high",
				SystemPrompt: "be helpful",
			},
			"child": {
				Extends: "parent",
				Model:   "sonnet",
			},
		},
	}

	rp, err := cfg.Resolve("child")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "interactive" {
		t.Fatalf("expected inherited default_mode 'interactive', got %q", rp.DefaultMode)
	}
	if rp.CLI != "claude" {
		t.Fatalf("expected inherited cli 'claude', got %q", rp.CLI)
	}
	if rp.Model != "sonnet" {
		t.Fatalf("expected overridden model 'sonnet', got %q", rp.Model)
	}
	if rp.Effort != "high" {
		t.Fatalf("expected inherited effort 'high', got %q", rp.Effort)
	}
	if rp.SystemPrompt != "be helpful" {
		t.Fatalf("expected inherited system_prompt 'be helpful', got %q", rp.SystemPrompt)
	}
}

func TestResolve_EffortUnsetAfterMerge(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]*Profile{
			"parent": {
				DefaultMode: "headless",
				CLI:         "claude",
			},
			"child": {
				Extends: "parent",
			},
		},
	}

	rp, err := cfg.Resolve("child")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Effort != "" {
		t.Fatalf("expected effort to be empty, got %q", rp.Effort)
	}
}

func TestResolve_SystemPromptInherited(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]*Profile{
			"parent": {
				DefaultMode:  "interactive",
				CLI:          "claude",
				SystemPrompt: "inherited prompt",
			},
			"child": {
				Extends: "parent",
			},
		},
	}

	rp, err := cfg.Resolve("child")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.SystemPrompt != "inherited prompt" {
		t.Fatalf("expected inherited system_prompt, got %q", rp.SystemPrompt)
	}
}

func TestResolve_MultiLevelInheritance(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]*Profile{
			"a": {
				DefaultMode: "headless",
				CLI:         "codex",
			},
			"b": {
				Extends: "a",
				Model:   "o3",
			},
			"c": {
				Extends: "b",
				Effort:  "low",
			},
		},
	}

	rp, err := cfg.Resolve("c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "headless" {
		t.Fatalf("expected default_mode from A, got %q", rp.DefaultMode)
	}
	if rp.CLI != "codex" {
		t.Fatalf("expected cli from A, got %q", rp.CLI)
	}
	if rp.Model != "o3" {
		t.Fatalf("expected model from B, got %q", rp.Model)
	}
	if rp.Effort != "low" {
		t.Fatalf("expected effort from C, got %q", rp.Effort)
	}
}

func TestResolve_ProfileNotFound(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]*Profile{},
	}

	_, err := cfg.Resolve("missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestResolve_CycleInResolve(t *testing.T) {
	// Build config directly (bypassing validate) to test Resolve's own cycle detection.
	cfg := &Config{
		Profiles: map[string]*Profile{
			"a": {Extends: "b"},
			"b": {Extends: "a"},
		},
	}

	_, err := cfg.Resolve("a")
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func TestResolve_DefaultConfigProfiles(t *testing.T) {
	cfg := defaultConfig()

	// Planner should resolve to interactive_base values.
	rp, err := cfg.Resolve("planner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "interactive" || rp.CLI != "claude" || rp.Model != "opus" || rp.Effort != "high" {
		t.Fatalf("unexpected planner resolution: %+v", rp)
	}

	// Implementor should resolve to headless_base values.
	rp, err = cfg.Resolve("implementor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "headless" || rp.CLI != "claude" || rp.Model != "opus" || rp.Effort != "high" {
		t.Fatalf("unexpected implementor resolution: %+v", rp)
	}
}
