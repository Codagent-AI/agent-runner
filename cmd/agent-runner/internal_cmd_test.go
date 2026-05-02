package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	interactive := defaultAgents["interactive_base"].(map[string]any)
	if interactive["default_mode"] != "interactive" || interactive["cli"] != "claude" || interactive["model"] != "opus" {
		t.Fatalf("interactive_base = %#v", interactive)
	}
	headless := defaultAgents["headless_base"].(map[string]any)
	if headless["default_mode"] != "headless" || headless["cli"] != "codex" {
		t.Fatalf("headless_base = %#v", headless)
	}
	if _, ok := headless["model"]; ok {
		t.Fatalf("headless_base included empty model: %#v", headless)
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
	if !strings.Contains(string(body), "interactive_base:") || !strings.Contains(string(body), "implementor:") {
		t.Fatalf("written config missing expected agents:\n%s", body)
	}
}

func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
