package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestWriteProfilePayloadValidation(t *testing.T) {
	var payload writeProfilePayload
	err := decodeWriteProfilePayload(strings.NewReader(`{"interactive_cli":"claude","headless_cli":"codex","target_path":"config.yaml"}`), &payload)
	if err != nil {
		t.Fatalf("decodeWriteProfilePayload() returned error: %v", err)
	}
	if payload.InteractiveCLI != "claude" || payload.HeadlessCLI != "codex" || payload.TargetPath != "config.yaml" {
		t.Fatalf("decoded payload = %#v", payload)
	}

	err = decodeWriteProfilePayload(strings.NewReader(`{"interactive_cli":"","headless_cli":"codex","target_path":"config.yaml"}`), &payload)
	if err == nil || !strings.Contains(err.Error(), "interactive_cli") {
		t.Fatalf("missing interactive_cli error = %v, want field name", err)
	}
}

func TestMergeProfileAgentsPreservesExistingYAML(t *testing.T) {
	input := []byte(`
profiles:
  default:
    agents:
      team_implementor:
        cli: claude
      planner:
        cli: old
  prod:
    agents:
      deployer:
        cli: codex
other_top_level: true
`)
	var doc yaml.Node
	if err := yaml.Unmarshal(input, &doc); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}

	err := mergeProfileAgents(&doc, &writeProfilePayload{
		InteractiveCLI:   "claude",
		InteractiveModel: "opus",
		HeadlessCLI:      "codex",
		HeadlessModel:    "",
		TargetPath:       "ignored.yaml",
	})
	if err != nil {
		t.Fatalf("mergeProfileAgents() returned error: %v", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	defaultAgents := got["profiles"].(map[string]any)["default"].(map[string]any)["agents"].(map[string]any)
	if _, ok := defaultAgents["team_implementor"]; !ok {
		t.Fatal("existing non-colliding agent was not preserved")
	}
	if got["profiles"].(map[string]any)["prod"] == nil || got["other_top_level"] != true {
		t.Fatalf("top-level/profile data not preserved: %#v", got)
	}
	if _, ok := defaultAgents["summarizer"]; ok {
		t.Fatal("merge wrote unexpected summarizer agent")
	}

	planner := defaultAgents["planner"].(map[string]any)
	if planner["default_mode"] != "interactive" || planner["cli"] != "claude" || planner["model"] != "opus" {
		t.Fatalf("planner = %#v", planner)
	}
	implementor := defaultAgents["implementor"].(map[string]any)
	if implementor["default_mode"] != "headless" || implementor["cli"] != "codex" {
		t.Fatalf("implementor = %#v", implementor)
	}
	if _, ok := implementor["model"]; ok {
		t.Fatalf("implementor included empty model: %#v", implementor)
	}
	for _, absent := range []string{"interactive_base", "headless_base"} {
		if _, ok := defaultAgents[absent]; ok {
			t.Fatalf("merge should not write %q", absent)
		}
	}
}

func TestMergeProfileAgentsRejectsNonMappingDocumentRoot(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("- not\n- a\n- mapping\n"), &doc); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}

	err := mergeProfileAgents(&doc, &writeProfilePayload{
		InteractiveCLI: "claude",
		HeadlessCLI:    "codex",
		TargetPath:     "ignored.yaml",
	})
	if err == nil || !strings.Contains(err.Error(), "config root must be a mapping") {
		t.Fatalf("mergeProfileAgents() error = %v, want config root mapping error", err)
	}
}

func TestMergeProfileAgentsRejectsNonMappingProfilePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "profiles",
			input:   "profiles: disabled\n",
			wantErr: "profiles must be a mapping",
		},
		{
			name:    "default",
			input:   "profiles:\n  default: []\n",
			wantErr: "profiles.default must be a mapping",
		},
		{
			name:    "agents",
			input:   "profiles:\n  default:\n    agents: false\n",
			wantErr: "profiles.default.agents must be a mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(tt.input), &doc); err != nil {
				t.Fatalf("unmarshal input: %v", err)
			}

			err := mergeProfileAgents(&doc, &writeProfilePayload{
				InteractiveCLI: "claude",
				HeadlessCLI:    "codex",
				TargetPath:     "ignored.yaml",
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("mergeProfileAgents() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestWriteProfileCommandRoundTripCreates0600File(t *testing.T) {
	target := filepath.Join(t.TempDir(), "home", ".agent-runner", "config.yaml")
	stdin := strings.NewReader(`{"interactive_cli":"claude","interactive_model":"opus","headless_cli":"codex","headless_model":"gpt-5","target_path":` + quoteJSON(target) + `}`)
	var stderr bytes.Buffer

	code := handleInternalWithIO([]string{"write-profile"}, stdin, &stderr)
	if code != 0 {
		t.Fatalf("handleInternalWithIO() = %d, stderr: %s", code, stderr.String())
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("target mode = %v, want 0600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatalf("stat target dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("target dir mode = %v, want 0755", got)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(body), "planner:") || !strings.Contains(string(body), "implementor:") {
		t.Fatalf("written config missing expected agents:\n%s", body)
	}
}

func TestWriteProfileCommandDoesNotRelaxExistingParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".agent-runner")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}
	target := filepath.Join(dir, "config.yaml")
	stdin := strings.NewReader(`{"interactive_cli":"claude","headless_cli":"codex","target_path":` + quoteJSON(target) + `}`)
	var stderr bytes.Buffer

	code := handleInternalWithIO([]string{"write-profile"}, stdin, &stderr)
	if code != 0 {
		t.Fatalf("handleInternalWithIO() = %d, stderr: %s", code, stderr.String())
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("target dir mode = %v, want preserved 0700", got)
	}
}

func TestConfiguredAgentCLIsJoinsExplicitConfigValues(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	writeFileForInternalTest(t, filepath.Join(home, ".agent-runner", "config.yaml"), `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
      reviewer:
        default_mode: headless
        cli: codex
`)
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")
	writeFileForInternalTest(t, projectPath, `profiles:
  default:
    agents:
      implementor:
        default_mode: headless
        cli: copilot
      inherited:
        extends: planner
`)

	got, err := configuredAgentCLIs(projectPath)
	if err != nil {
		t.Fatalf("configuredAgentCLIs() returned error: %v", err)
	}
	if got != "claude,codex,copilot" {
		t.Fatalf("configuredAgentCLIs() = %q, want claude,codex,copilot", got)
	}
}

func TestValidatorInitArgsUsesConfiguredCLIs(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	writeFileForInternalTest(t, filepath.Join(home, ".agent-runner", "config.yaml"), `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)
	projectPath := filepath.Join(repo, ".agent-runner", "config.yaml")
	writeFileForInternalTest(t, projectPath, `profiles:
  default:
    agents:
      implementor:
        default_mode: headless
        cli: codex
`)

	got, err := validatorInitArgs(projectPath)
	if err != nil {
		t.Fatalf("validatorInitArgs() returned error: %v", err)
	}
	want := []string{"init", "--agents", "claude,codex"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("validatorInitArgs() mismatch (-want +got):\n%s", diff)
	}
}

func TestValidatorInitArgsOmitsAgentsWhenNoExplicitConfigCLIs(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	t.Setenv("HOME", home)

	got, err := validatorInitArgs(filepath.Join(repo, ".agent-runner", "config.yaml"))
	if err != nil {
		t.Fatalf("validatorInitArgs() returned error: %v", err)
	}
	want := []string{"init"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("validatorInitArgs() mismatch (-want +got):\n%s", diff)
	}
}

func TestInternalJSONHelpersDecodeValidJSON(t *testing.T) {
	t.Run("string field", func(t *testing.T) {
		got, err := decodeJSONStringField(strings.NewReader(`{
			"value": "gpt-5.4 \"stable\""
		}`), "value")
		if err != nil {
			t.Fatalf("decodeJSONStringField() returned error: %v", err)
		}
		if got != `gpt-5.4 "stable"` {
			t.Fatalf("decodeJSONStringField() = %q", got)
		}
	})

	t.Run("list field", func(t *testing.T) {
		got, err := decodeJSONStringListField(strings.NewReader(`{
			"items": ["planner, implementor", "codex"]
		}`), "items")
		if err != nil {
			t.Fatalf("decodeJSONStringListField() returned error: %v", err)
		}
		want := []string{"planner, implementor", "codex"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("decodeJSONStringListField() mismatch (-want +got):\n%s", diff)
		}
	})
}

func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

func writeFileForInternalTest(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
