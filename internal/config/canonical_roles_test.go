package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLoadCanonicalizesLegacyOverrideBeforeLayerPrecedence(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	projectPath := filepath.Join(root, "repo", ".agent-runner", "config.yaml")
	t.Setenv("HOME", home)

	writeConfigFile(t, filepath.Join(home, ".agent-runner", "config.yaml"), `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: codex
        model: gpt-5.6-sol
`)

	cfg, err := Load(projectPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	resolved, err := cfg.Resolve("lead")
	if err != nil {
		t.Fatalf("Resolve(lead) error = %v", err)
	}
	if resolved.CLI != "codex" || resolved.Model != "gpt-5.6-sol" {
		t.Fatalf("Resolve(lead) = %+v, want legacy planner override", resolved)
	}
	if diff := cmp.Diff([]string{"planner->lead"}, deprecationPairs(cfg.Deprecations)); diff != "" {
		t.Fatalf("config deprecations mismatch (-want +got):\n%s", diff)
	}
}

func TestDefaultProfileContainsApprovedSevenCanonicalAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantNames := []string{
		"autonomous_base",
		"crosscheck",
		"implementor",
		"interactive_base",
		"lead",
		"summarizer",
		"tester",
	}
	gotNames := make([]string, 0, len(cfg.ActiveAgents))
	for name := range cfg.ActiveAgents {
		gotNames = append(gotNames, name)
	}
	slices.Sort(gotNames)
	if diff := cmp.Diff(wantNames, gotNames); diff != "" {
		t.Fatalf("default agents mismatch (-want +got):\n%s", diff)
	}

	tester, err := cfg.Resolve("tester")
	if err != nil {
		t.Fatalf("Resolve(tester) error = %v", err)
	}
	if tester.DefaultMode != "autonomous" || tester.CLI != "claude" || tester.Model != "sonnet" || tester.Effort != "high" {
		t.Fatalf("Resolve(tester) = %+v, want autonomous Claude Sonnet/high", tester)
	}
}

func TestResolveCanonicalizesLegacyReference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	resolved, err := cfg.Resolve("planner")
	if err != nil {
		t.Fatalf("Resolve(planner) error = %v", err)
	}
	if resolved.DefaultMode != "interactive" || resolved.CLI != "claude" || resolved.Model != "opus" || resolved.Effort != "high" {
		t.Fatalf("Resolve(planner) = %+v, want canonical Lead defaults", resolved)
	}
	if diff := cmp.Diff([]string{"planner->lead"}, deprecationPairs(resolved.Deprecations)); diff != "" {
		t.Fatalf("resolution deprecations mismatch (-want +got):\n%s", diff)
	}

	reviewer, err := cfg.Resolve("reviewer")
	if err != nil {
		t.Fatalf("Resolve(reviewer) error = %v", err)
	}
	if reviewer.DefaultMode != "autonomous" || reviewer.CLI != "claude" || reviewer.Model != "opus" || reviewer.Effort != "high" {
		t.Fatalf("Resolve(reviewer) = %+v, want canonical Crosscheck defaults", reviewer)
	}
	if diff := cmp.Diff([]string{"reviewer->crosscheck"}, deprecationPairs(reviewer.Deprecations)); diff != "" {
		t.Fatalf("resolution deprecations mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadCanonicalizesLegacyExtendsTarget(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(root, ".agent-runner", "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      child:
        extends: reviewer
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	resolved, err := cfg.Resolve("child")
	if err != nil {
		t.Fatalf("Resolve(child) error = %v", err)
	}
	if resolved.DefaultMode != "autonomous" || resolved.CLI != "claude" || resolved.Model != "opus" || resolved.Effort != "high" {
		t.Fatalf("Resolve(child) = %+v, want canonical Crosscheck defaults", resolved)
	}
	if diff := cmp.Diff([]string{"reviewer->crosscheck"}, deprecationPairs(cfg.Deprecations)); diff != "" {
		t.Fatalf("config deprecations mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadDeprecationsExcludeInactiveProfileSets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), ".agent-runner", "config.yaml")
	writeConfigFile(t, path, `active_profile: active
profiles:
  active:
    extends: base
  base:
    agents:
      reviewer:
        default_mode: autonomous
        cli: claude
        model: opus
  inactive:
    agents:
      planner:
        default_mode: interactive
        cli: codex
        model: gpt-5.6-sol
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if diff := cmp.Diff([]string{"reviewer->crosscheck"}, deprecationPairs(cfg.Deprecations)); diff != "" {
		t.Fatalf("config deprecations mismatch (-want +got):\n%s", diff)
	}
	if _, ok := cfg.Profiles["inactive"].Agents["lead"]; !ok {
		t.Fatal("inactive planner alias was not canonicalized to lead")
	}
}

func TestLoadHigherLayerCanonicalNameOverridesLegacySynonym(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	projectPath := filepath.Join(root, "repo", ".agent-runner", "config.yaml")
	t.Setenv("HOME", home)
	writeConfigFile(t, filepath.Join(home, ".agent-runner", "config.yaml"), `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: codex
`)
	writeConfigFile(t, projectPath, `profiles:
  default:
    agents:
      lead:
        default_mode: interactive
        cli: copilot
`)

	cfg, err := Load(projectPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	resolved, err := cfg.Resolve("lead")
	if err != nil {
		t.Fatalf("Resolve(lead) error = %v", err)
	}
	if resolved.CLI != "copilot" {
		t.Fatalf("Resolve(lead).CLI = %q, want project canonical override", resolved.CLI)
	}
}

func TestLoadRejectsSameLayerCanonicalAndLegacySynonyms(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), ".agent-runner", "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      crosscheck:
        extends: autonomous_base
      reviewer:
        extends: autonomous_base
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want same-layer synonym conflict")
	}
	for _, text := range []string{"crosscheck", "reviewer", "keep \"crosscheck\""} {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("Load() error = %q, want actionable text %q", err, text)
		}
	}
}

func TestLoadRejectsSameLayerSynonymsEvenWhenOneEntryIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), ".agent-runner", "config.yaml")
	writeConfigFile(t, path, `profiles:
  default:
    agents:
      lead:
      planner:
        extends: interactive_base
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want same-layer synonym conflict")
	}
	for _, text := range []string{"lead", "planner", "keep \"lead\""} {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("Load() error = %q, want actionable text %q", err, text)
		}
	}
}

func TestLoadLegacyAliasesDoesNotRewriteFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), ".agent-runner", "config.yaml")
	contents := []byte(`profiles:
  default:
    agents:
      planner:
        extends: interactive_base
      reviewer:
        extends: autonomous_base
`)
	writeConfigFile(t, path, string(contents))

	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if diff := cmp.Diff(contents, after); diff != "" {
		t.Fatalf("config file changed on load (-want +got):\n%s", diff)
	}
}

func deprecationPairs(deprecations []Deprecation) []string {
	pairs := make([]string, 0, len(deprecations))
	for _, deprecation := range deprecations {
		pairs = append(pairs, deprecation.Alias+"->"+deprecation.Canonical)
	}
	return pairs
}
