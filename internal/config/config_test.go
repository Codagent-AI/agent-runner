package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain isolates HOME so tests don't read the developer's real
// ~/.agent-runner/config.yaml. Individual tests that exercise global-config
// behavior override HOME themselves via t.Setenv.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	sandbox, err := os.MkdirTemp("", "agent-runner-config-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating sandbox HOME: %v\n", err)
		return 1
	}
	defer os.RemoveAll(sandbox)
	_ = os.Setenv("HOME", sandbox)
	return m.Run()
}

func TestLoadOrGenerate_CreatesDefault(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	dir := filepath.Join(root, "repo")
	path := filepath.Join(dir, ".agent-runner", "config.yaml")

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should NOT be created on disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected config file to NOT be created on disk")
	}

	// Should have one profile set named "default".
	if len(cfg.Profiles) != 1 {
		t.Fatalf("expected 1 profile set, got %d", len(cfg.Profiles))
	}
	defaultSet := cfg.Profiles["default"]
	if defaultSet == nil {
		t.Fatal("expected 'default' profile set")
	}
	if len(defaultSet.Agents) != 5 {
		t.Fatalf("expected 5 agents in default profile set, got %d", len(defaultSet.Agents))
	}

	// Active agents should have 5 entries.
	if len(cfg.ActiveAgents) != 5 {
		t.Fatalf("expected 5 active agents, got %d", len(cfg.ActiveAgents))
	}

	// Verify interactive_base.
	ib := cfg.ActiveAgents["interactive_base"]
	if ib == nil {
		t.Fatal("expected interactive_base agent")
	}
	if ib.DefaultMode != "interactive" || ib.CLI != "claude" || ib.Model != "opus" || ib.Effort != "high" {
		t.Fatalf("unexpected interactive_base: %+v", ib)
	}

	// Verify headless_base.
	hb := cfg.ActiveAgents["headless_base"]
	if hb == nil {
		t.Fatal("expected headless_base agent")
	}
	if hb.DefaultMode != "headless" || hb.CLI != "claude" || hb.Model != "opus" || hb.Effort != "high" {
		t.Fatalf("unexpected headless_base: %+v", hb)
	}

	// Verify planner extends interactive_base.
	pl := cfg.ActiveAgents["planner"]
	if pl == nil || pl.Extends != "interactive_base" {
		t.Fatalf("expected planner to extend interactive_base, got %+v", pl)
	}

	// Verify implementor extends headless_base.
	im := cfg.ActiveAgents["implementor"]
	if im == nil || im.Extends != "headless_base" {
		t.Fatalf("expected implementor to extend headless_base, got %+v", im)
	}

	// Verify summarizer.
	sum := cfg.ActiveAgents["summarizer"]
	if sum == nil {
		t.Fatal("expected summarizer agent")
	}
	if sum.Extends != "" {
		t.Fatalf("expected summarizer to be standalone, got extends=%q", sum.Extends)
	}
	if sum.DefaultMode != "headless" || sum.CLI != "claude" || sum.Model != "haiku" || sum.Effort != "low" {
		t.Fatalf("unexpected summarizer: %+v", sum)
	}
}

func TestLoadOrGenerate_SkipsGlobalConfigWhenHomeDirUnavailable(t *testing.T) {
	original := userHomeDir
	userHomeDir = func() (string, error) { return "", fmt.Errorf("home unavailable") }
	t.Cleanup(func() { userHomeDir = original })

	dir := t.TempDir()
	path := filepath.Join(dir, ".agent-runner", "config.yaml")

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// File should NOT be created on disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected config file to NOT be created on disk")
	}
	if len(cfg.ActiveAgents) != 5 {
		t.Fatalf("expected 5 default active agents, got %d", len(cfg.ActiveAgents))
	}
}

func TestLoadOrGenerate_LoadsExisting(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	dir := filepath.Join(root, "repo")
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  default:
    agents:
      custom:
        default_mode: headless
        cli: codex
        model: o3
`
	writeConfigFile(t, path, content)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Built-in defaults (5 agents) layer under the project's 1 custom agent.
	if len(cfg.ActiveAgents) != 6 {
		t.Fatalf("expected 6 active agents (5 defaults + 1 custom), got %d", len(cfg.ActiveAgents))
	}
	p := cfg.ActiveAgents["custom"]
	if p == nil || p.DefaultMode != "headless" || p.CLI != "codex" || p.Model != "o3" {
		t.Fatalf("unexpected agent: %+v", p)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading existing config: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected existing config to remain untouched\n got: %q\nwant: %q", string(got), content)
	}
}

func TestLoadOrGenerate_AllFieldsSpecified(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  default:
    agents:
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
	p := cfg.ActiveAgents["full"]
	if p.DefaultMode != "interactive" || p.CLI != "claude" || p.Model != "opus" || p.Effort != "high" || p.SystemPrompt != "be helpful" {
		t.Fatalf("expected all fields loaded, got %+v", p)
	}
}

func TestLoadOrGenerate_OptionalFieldsOmitted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  default:
    agents:
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
	p := cfg.ActiveAgents["minimal"]
	if p.Model != "" || p.Effort != "" || p.SystemPrompt != "" {
		t.Fatalf("expected optional fields to be empty, got %+v", p)
	}
}

func TestLoadOrGenerate_UnrecognizedFieldIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `profiles:
  default:
    agents:
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
	if cfg.ActiveAgents["test"] == nil {
		t.Fatal("expected test agent to be loaded")
	}
}

func TestLoadOrGenerate_MergesGlobalAndProjectProfiles(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      headless_base:
        default_mode: headless
        cli: claude
        model: sonnet
      implementor:
        extends: headless_base
        model: opus
        effort: high
      global_only:
        extends: headless_base
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      implementor:
        extends: headless_base
        cli: copilot
      project_only:
        extends: headless_base
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ActiveAgents["global_only"] == nil {
		t.Fatal("expected global-only agent in merged config")
	}
	if cfg.ActiveAgents["project_only"] == nil {
		t.Fatal("expected project-only agent in merged config")
	}

	implementor := cfg.ActiveAgents["implementor"]
	if implementor == nil {
		t.Fatal("expected merged implementor agent")
	}
	if implementor.Extends != "headless_base" || implementor.CLI != "copilot" {
		t.Fatalf("unexpected merged implementor agent: %+v", implementor)
	}
	if implementor.Model != "" || implementor.Effort != "" {
		t.Fatalf("expected project agent to replace global one without field merging, got %+v", implementor)
	}

	rp, err := cfg.Resolve("project_only")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if rp.DefaultMode != "headless" || rp.CLI != "claude" || rp.Model != "sonnet" {
		t.Fatalf("expected project agent to inherit from global base, got %+v", rp)
	}
}

func TestLoadOrGenerate_GlobalProfileCanExtendProjectProfile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      summarizer:
        extends: team_base
        model: haiku
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      team_base:
        default_mode: interactive
        cli: copilot
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rp, err := cfg.Resolve("summarizer")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if rp.DefaultMode != "interactive" || rp.CLI != "copilot" || rp.Model != "haiku" {
		t.Fatalf("unexpected resolved agent: %+v", rp)
	}
}

func TestLoadOrGenerate_DetectsCrossFileCycle(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      a:
        extends: b
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      b:
        extends: a
`)

	_, err := LoadOrGenerate(projectPath)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func TestLoadOrGenerate_GlobalInvalidYAMLIncludesPath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, "profiles:\n  bad: [\n")

	_, err := LoadOrGenerate(projectPath)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), globalPath) {
		t.Fatalf("expected error to mention global path %q, got %v", globalPath, err)
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestLoadOrGenerate_DoesNotCreateGlobalConfigWhenMissing(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ActiveAgents) != 5 {
		t.Fatalf("expected default project config, got %d active agents", len(cfg.ActiveAgents))
	}

	globalDir := filepath.Join(home, ".agent-runner")
	if _, err := os.Stat(globalDir); !os.IsNotExist(err) {
		t.Fatalf("expected no global config directory to be created, stat err = %v", err)
	}
}

func TestLoadOrGenerate_GlobalOverridesDefaultAgent(t *testing.T) {
	// Regression: with no project config on disk, built-in defaults must not
	// stomp a global agent that shares a name. Previously the default
	// implementor (which only extends headless_base) replaced the global
	// implementor, so users with a global `implementor: cli: copilot` still
	// got claude/sonnet.
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      implementor:
        extends: headless_base
        cli: copilot
        model: gpt-5.4
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rp, err := cfg.Resolve("implementor")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if rp.CLI != "copilot" {
		t.Fatalf("expected global implementor.cli=copilot to override default, got %q", rp.CLI)
	}
	if rp.Model != "gpt-5.4" {
		t.Fatalf("expected global implementor.model=gpt-5.4 to override default, got %q", rp.Model)
	}
	// default_mode and effort are inherited from the default headless_base,
	// proving defaults remain available as a fallback base layer.
	if rp.DefaultMode != "headless" {
		t.Fatalf("expected default_mode headless from default headless_base, got %q", rp.DefaultMode)
	}
}

func TestLoadOrGenerate_ProjectExistsStillIncludesDefaultAgents(t *testing.T) {
	// When a project config defines only a custom agent, the built-in
	// defaults (planner, implementor, etc.) must still be available so that
	// shipped workflows referencing those agents continue to work.
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)

	repo := filepath.Join(root, "repo")
	path := filepath.Join(repo, ".agent-runner", "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      custom:
        default_mode: headless
        cli: codex
        model: o3
`)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaultSet := cfg.Profiles["default"]
	if defaultSet == nil {
		t.Fatal("expected default profile set to be present")
	}
	if defaultSet.Agents["custom"] == nil {
		t.Fatal("expected project-defined custom agent to be present")
	}
	for _, name := range []string{"planner", "implementor", "headless_base", "interactive_base", "summarizer"} {
		if defaultSet.Agents[name] == nil {
			t.Fatalf("expected default agent %q to remain available alongside project config", name)
		}
	}
}

func TestLoadOrGenerate_DefaultsAndMergesGlobalProfiles(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      global_only:
        default_mode: headless
        cli: copilot
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should NOT be created on disk.
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatal("expected project config file to NOT be created on disk")
	}
	if cfg.ActiveAgents["global_only"] == nil {
		t.Fatal("expected global agent to be merged into default config")
	}
	if cfg.ActiveAgents["planner"] == nil {
		t.Fatal("expected default agents to be present")
	}
}

// --- Legacy flat shape rejection ---

func TestLoadOrGenerate_RejectsLegacyFlatShapeProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  planner:
    extends: interactive_base
    cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected error for legacy flat shape")
	}
	if !strings.Contains(err.Error(), "restructure") {
		t.Fatalf("expected restructure message, got: %v", err)
	}
}

func TestLoadOrGenerate_RejectsLegacyFlatShapeGlobal(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  headless_base:
    default_mode: headless
    cli: claude
`)

	_, err := LoadOrGenerate(projectPath)
	if err == nil {
		t.Fatal("expected error for legacy flat shape in global config")
	}
	if !strings.Contains(err.Error(), "restructure") {
		t.Fatalf("expected restructure message, got: %v", err)
	}
}

func TestLoadOrGenerate_RejectsLegacyMixedShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
  headless_base:
    default_mode: headless
    cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected error for mixed legacy/new shape")
	}
	if !strings.Contains(err.Error(), "restructure") {
		t.Fatalf("expected restructure message, got: %v", err)
	}
}

// --- active_profile selection ---

func TestLoadOrGenerate_ActiveProfileSelects(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	dir := filepath.Join(root, "repo")
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `active_profile: copilot
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
  copilot:
    agents:
      planner:
        default_mode: headless
        cli: copilot
`)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := cfg.ActiveAgents["planner"]
	if p == nil {
		t.Fatal("expected planner in active agents")
	}
	if p.CLI != "copilot" {
		t.Fatalf("expected copilot profile's planner, got %+v", p)
	}
}

func TestLoadOrGenerate_NoActiveProfileFallsToDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ActiveAgents["planner"] == nil {
		t.Fatal("expected planner in active agents from default profile set")
	}
}

func TestLoadOrGenerate_ActiveProfileMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `active_profile: missing
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected error for missing active profile set")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected error naming the missing profile, got: %v", err)
	}
}

func TestLoadOrGenerate_ActiveProfileInGlobalConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `active_profile: default
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)

	_, err := LoadOrGenerate(projectPath)
	if err == nil {
		t.Fatal("expected error for active_profile in global config")
	}
	if !strings.Contains(err.Error(), "active_profile") {
		t.Fatalf("expected error about active_profile, got: %v", err)
	}
	if !strings.Contains(err.Error(), "global") {
		t.Fatalf("expected error to mention global config, got: %v", err)
	}
}

// --- Profile set merging ---

func TestLoadOrGenerate_MergesDisjointProfileSets(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  work:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)
	writeConfigFile(t, projectPath, `active_profile: personal
profiles:
  personal:
    agents:
      planner:
        default_mode: headless
        cli: codex
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Profiles["work"] == nil {
		t.Fatal("expected 'work' profile set from global")
	}
	if cfg.Profiles["personal"] == nil {
		t.Fatal("expected 'personal' profile set from project")
	}
	// Active agents should come from personal.
	if cfg.ActiveAgents["planner"] == nil || cfg.ActiveAgents["planner"].CLI != "codex" {
		t.Fatalf("expected personal planner active, got %+v", cfg.ActiveAgents["planner"])
	}
}

func TestLoadOrGenerate_MergesSameProfileSetDisjointAgents(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      implementor:
        default_mode: headless
        cli: claude
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ActiveAgents["planner"] == nil {
		t.Fatal("expected planner from global in merged default set")
	}
	if cfg.ActiveAgents["implementor"] == nil {
		t.Fatal("expected implementor from project in merged default set")
	}
}

func TestLoadOrGenerate_MergesSameProfileSetOverlappingAgents(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  default:
    agents:
      implementor:
        default_mode: headless
        cli: claude
        model: opus
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      implementor:
        default_mode: headless
        cli: copilot
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	im := cfg.ActiveAgents["implementor"]
	if im == nil {
		t.Fatal("expected implementor in active agents")
	}
	if im.CLI != "copilot" {
		t.Fatalf("expected project version of implementor (copilot), got %+v", im)
	}
	if im.Model != "" {
		t.Fatalf("expected no model (project version has none), got %q", im.Model)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsInheritsAgentsAndSupportsAgentLevelExtends(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  team_base:
    agents:
      headless_base:
        default_mode: headless
        cli: claude
        model: sonnet
      planner:
        default_mode: interactive
        cli: claude
`)
	writeConfigFile(t, projectPath, `active_profile: copilot
profiles:
  copilot:
    extends: team_base
    agents:
      implementor:
        extends: headless_base
        cli: copilot
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ActiveAgents["headless_base"] == nil {
		t.Fatal("expected inherited headless_base in active profile")
	}
	if cfg.ActiveAgents["planner"] == nil {
		t.Fatal("expected inherited planner in active profile")
	}
	if cfg.ActiveAgents["implementor"] == nil {
		t.Fatal("expected local implementor in active profile")
	}

	rp, err := cfg.Resolve("implementor")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if rp.DefaultMode != "headless" || rp.CLI != "copilot" || rp.Model != "sonnet" {
		t.Fatalf("unexpected resolved implementor: %+v", rp)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsOverrideReplacesParentAgentWholly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `active_profile: child
profiles:
  parent:
    agents:
      headless_base:
        default_mode: headless
        cli: claude
      implementor:
        default_mode: headless
        cli: claude
        model: opus
  child:
    extends: parent
    agents:
      implementor:
        extends: headless_base
        cli: copilot
`)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	im := cfg.ActiveAgents["implementor"]
	if im == nil {
		t.Fatal("expected implementor in active agents")
	}
	if im.Extends != "headless_base" || im.CLI != "copilot" {
		t.Fatalf("unexpected active implementor: %+v", im)
	}
	if im.DefaultMode != "" || im.Model != "" {
		t.Fatalf("expected child agent to replace parent wholesale, got %+v", im)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsProjectWinsOverGlobal(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  base_a:
    agents:
      headless_base:
        default_mode: headless
        cli: claude
  base_b:
    agents:
      headless_base:
        default_mode: headless
        cli: copilot
  copilot:
    extends: base_a
    agents:
      implementor:
        extends: headless_base
`)
	writeConfigFile(t, projectPath, `active_profile: copilot
profiles:
  copilot:
    extends: base_b
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rp, err := cfg.Resolve("implementor")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if rp.CLI != "copilot" {
		t.Fatalf("expected project extends parent to win, got %+v", rp)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsMergeThenExtendAcrossGlobalAndProject(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")

	writeConfigFile(t, globalPath, `profiles:
  team_base:
    agents:
      headless_base:
        default_mode: headless
        cli: claude
  copilot:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)
	writeConfigFile(t, projectPath, `active_profile: copilot
profiles:
  copilot:
    extends: team_base
    agents:
      implementor:
        extends: headless_base
`)

	cfg, err := LoadOrGenerate(projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ActiveAgents["headless_base"] == nil {
		t.Fatal("expected inherited headless_base in active agents")
	}
	if cfg.ActiveAgents["planner"] == nil {
		t.Fatal("expected merged global planner in active agents")
	}
	if cfg.ActiveAgents["implementor"] == nil {
		t.Fatal("expected project implementor in active agents")
	}
}

func TestLoadOrGenerate_ProfileSetExtendsMultiLevelChain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `active_profile: a
profiles:
  c:
    agents:
      headless_base:
        default_mode: headless
        cli: claude
  b:
    extends: c
    agents:
      planner:
        default_mode: interactive
        cli: claude
  a:
    extends: b
    agents:
      implementor:
        extends: headless_base
`)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ActiveAgents["headless_base"] == nil {
		t.Fatal("expected headless_base inherited through chain")
	}
	if cfg.ActiveAgents["planner"] == nil {
		t.Fatal("expected planner inherited through chain")
	}
	if cfg.ActiveAgents["implementor"] == nil {
		t.Fatal("expected implementor in active agents")
	}
}

func TestLoadOrGenerate_NonActiveProfileSetInheritedInvalidAgentBlocksLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  shared:
    agents:
      reviewer:
        default_mode: headless
        cli: copilot
        effort: extreme
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
  copilot:
    extends: shared
    agents:
      implementor:
        default_mode: headless
        cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error for inherited invalid agent in non-active profile set")
	}
	if !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "effort") {
		t.Fatalf("expected inherited invalid agent error, got: %v", err)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsMissingParent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `active_profile: copilot
profiles:
  copilot:
    extends: missing
    agents:
      implementor:
        default_mode: headless
        cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected missing parent error")
	}
	if !strings.Contains(err.Error(), `profile set "copilot"`) || !strings.Contains(err.Error(), `"missing"`) {
		t.Fatalf("expected missing profile set parent in error, got: %v", err)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsRejectsNonString(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  copilot:
    extends:
      - base_a
      - base_b
    agents:
      implementor:
        default_mode: headless
        cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected extends type error")
	}
	if !strings.Contains(err.Error(), "extends") || !strings.Contains(err.Error(), "single profile set name") {
		t.Fatalf("expected extends type error, got: %v", err)
	}
}

func TestLoadOrGenerate_ProfileSetExtendsCycles(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name: "direct cycle",
			content: `profiles:
  a:
    extends: b
    agents:
      planner:
        default_mode: interactive
        cli: claude
  b:
    extends: a
    agents:
      implementor:
        default_mode: headless
        cli: claude
`,
			want: []string{"cycle", "a", "b"},
		},
		{
			name: "self reference",
			content: `profiles:
  a:
    extends: a
    agents:
      planner:
        default_mode: interactive
        cli: claude
`,
			want: []string{"cycle", "a"},
		},
		{
			name: "indirect cycle",
			content: `profiles:
  a:
    extends: b
    agents:
      planner:
        default_mode: interactive
        cli: claude
  b:
    extends: c
    agents:
      implementor:
        default_mode: headless
        cli: claude
  c:
    extends: a
    agents:
      reviewer:
        default_mode: headless
        cli: copilot
`,
			want: []string{"cycle", "a", "b", "c"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			writeConfigFile(t, path, tc.content)

			_, err := LoadOrGenerate(path)
			if err == nil {
				t.Fatal("expected cycle error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("expected error to contain %q, got: %v", want, err)
				}
			}
		})
	}
}

func TestLoadOrGenerate_NonActiveProfileSetInvalidAgentBlocksLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
  copilot:
    agents:
      reviewer:
        default_mode: headless
        cli: copilot
        effort: extreme
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error for invalid agent in non-active profile set")
	}
	if !strings.Contains(err.Error(), "effort") {
		t.Fatalf("expected error about invalid effort, got: %v", err)
	}
}

func TestLoadOrGenerate_AgentLevelExtendsCannotReachUnrelatedProfileSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `active_profile: copilot
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
  copilot:
    agents:
      implementor:
        extends: planner
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected error for agent-level extends across unrelated profile sets")
	}
	if !strings.Contains(err.Error(), "planner") {
		t.Fatalf("expected error naming missing parent, got: %v", err)
	}
}

// --- Validation tests (new YAML format) ---

func TestValidation_BaseAgentMissingDefaultMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      bad:
        cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "default_mode") {
		t.Fatalf("expected error about default_mode, got: %v", err)
	}
}

func TestValidation_BaseAgentMissingCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      bad:
        default_mode: headless
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cli") {
		t.Fatalf("expected error about cli, got: %v", err)
	}
}

func TestValidation_ChildAgentOmitsFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      parent:
        default_mode: interactive
        cli: claude
      child:
        extends: parent
`)

	_, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidation_InvalidEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      bad:
        default_mode: interactive
        cli: claude
        effort: maximum
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid effort") {
		t.Fatalf("expected error about invalid effort, got: %v", err)
	}
}

func TestValidation_InvalidCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      bad:
        default_mode: headless
        cli: unknown
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid cli") {
		t.Fatalf("expected error about invalid cli, got: %v", err)
	}
}

func TestValidation_CopilotCLIAccepted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      copilot_base:
        default_mode: headless
        cli: copilot
`)

	cfg, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := cfg.ActiveAgents["copilot_base"]
	if p == nil || p.CLI != "copilot" {
		t.Fatalf("expected copilot agent, got %+v", p)
	}
}

func TestValidation_InvalidDefaultMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      bad:
        default_mode: auto
        cli: claude
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid default_mode") {
		t.Fatalf("expected error about invalid default_mode, got: %v", err)
	}
}

func TestValidation_ExtendsNonexistent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      child:
        extends: nonexistent
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected error about nonexistent parent, got: %v", err)
	}
}

func TestValidation_CycleDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      a:
        extends: b
      b:
        extends: a
`)

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func TestValidation_NilAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("profiles:\n  default:\n    agents:\n      empty:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerate(path)
	if err == nil {
		t.Fatal("expected validation error for nil agent")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected 'must not be empty' error, got: %v", err)
	}
}

// --- Resolve tests (new types) ---

func TestResolve_ChildOverridesOneField(t *testing.T) {
	cfg := &Config{
		ActiveAgents: map[string]*Agent{
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
		ActiveAgents: map[string]*Agent{
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
		ActiveAgents: map[string]*Agent{
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
		ActiveAgents: map[string]*Agent{
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

func TestResolve_AgentNotFound(t *testing.T) {
	cfg := &Config{
		ActiveAgents: map[string]*Agent{},
	}

	_, err := cfg.Resolve("missing")
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestResolve_CycleInResolve(t *testing.T) {
	cfg := &Config{
		ActiveAgents: map[string]*Agent{
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

func TestResolve_DefaultConfigAgents(t *testing.T) {
	// Loading with no project or global file yields the built-in defaults only.
	t.Setenv("HOME", t.TempDir())
	cfg, err := LoadOrGenerate(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Planner should resolve to interactive_base values.
	rp, err := cfg.Resolve("planner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "interactive" || rp.CLI != "claude" || rp.Model != "opus" || rp.Effort != "high" {
		t.Fatalf("unexpected planner resolution: %+v", rp)
	}

	// Implementor should resolve to headless_base values (opus per spec).
	rp, err = cfg.Resolve("implementor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "headless" || rp.CLI != "claude" || rp.Model != "opus" || rp.Effort != "high" {
		t.Fatalf("unexpected implementor resolution: %+v", rp)
	}

	// Summarizer should resolve to its standalone low-cost defaults.
	rp, err = cfg.Resolve("summarizer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.DefaultMode != "headless" || rp.CLI != "claude" || rp.Model != "haiku" || rp.Effort != "low" {
		t.Fatalf("unexpected summarizer resolution: %+v", rp)
	}
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
