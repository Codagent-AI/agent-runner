package profilewrite

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestRequestExposesCanonicalFourRoleContract(t *testing.T) {
	typ := reflect.TypeOf(Request{})
	for _, name := range []string{
		"TargetPath",
		"LeadCLI", "LeadModel",
		"CrosscheckCLI", "CrosscheckModel",
		"ImplementorCLI", "ImplementorModel",
		"TesterCLI", "TesterModel",
	} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("Request is missing field %s", name)
		}
	}
	for _, name := range []string{
		"InteractiveCLI", "InteractiveModel", "HeadlessCLI", "HeadlessModel",
	} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("Request retains obsolete field %s", name)
		}
	}
}

func TestStagePreservesOriginalUntilAtomicCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	original := "profiles:\n  default:\n    agents:\n      existing: {}\n"
	mustWrite(t, target, original, 0o640)
	req := validRequest(target)

	staged, err := Stage(&req)
	if err != nil {
		t.Fatalf("Stage() returned error: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(body) != original {
		t.Fatalf("Stage() modified target before commit:\n%s", body)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".agent-runner-*.tmp"))
	if err != nil {
		t.Fatalf("glob staged files: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("staged files = %v, want one", matches)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("staged file mode = %o, want 600", got)
	}

	if err := staged.Commit(); err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}
	agents := readYAMLMap(t, target)["profiles"].(map[string]any)["default"].(map[string]any)["agents"].(map[string]any)
	for _, role := range []string{"lead", "crosscheck", "implementor", "tester"} {
		if _, ok := agents[role]; !ok {
			t.Errorf("committed profile missing %s", role)
		}
	}
	matches, err = filepath.Glob(filepath.Join(dir, ".agent-runner-*.tmp"))
	if err != nil {
		t.Fatalf("glob staged files after commit: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("staged files remain after commit: %v", matches)
	}
}

func TestStageDiscardRemovesTemporaryFileAndPreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	original := "unrelated: true\n"
	mustWrite(t, target, original, 0o600)
	req := validRequest(target)

	staged, err := Stage(&req)
	if err != nil {
		t.Fatalf("Stage() returned error: %v", err)
	}
	if err := staged.Discard(); err != nil {
		t.Fatalf("Discard() returned error: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(body) != original {
		t.Fatalf("Discard() changed target:\n%s", body)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".agent-runner-*.tmp"))
	if err != nil {
		t.Fatalf("glob staged files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("staged files remain after discard: %v", matches)
	}
}

func TestWriteMergesCanonicalRolesAndPreservesUnmanagedContent(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".agent-runner", "config.yaml")
	mustWrite(t, target, `
profiles:
  default:
    agents:
      interactive_base:
        cli: claude
      autonomous_base:
        cli: codex
      summarizer:
        cli: copilot
      team_implementor:
        extends: autonomous_base
      planner:
        cli: legacy
      reviewer:
        cli: legacy
      lead:
        cli: old
  production:
    agents:
      planner:
        cli: preserved
other_top_level: true
`, 0o640)

	err := Write(&Request{
		TargetPath:       target,
		LeadCLI:          "claude",
		LeadModel:        "opus",
		CrosscheckCLI:    "codex",
		CrosscheckModel:  "gpt-5.6-sol",
		ImplementorCLI:   "codex",
		ImplementorModel: "gpt-5.6-terra",
		TesterCLI:        "claude",
		TesterModel:      "sonnet",
	})
	if err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	got := readYAMLMap(t, target)
	profiles := got["profiles"].(map[string]any)
	agents := profiles["default"].(map[string]any)["agents"].(map[string]any)
	wantManaged := map[string]map[string]any{
		"lead":        {"default_mode": "interactive", "cli": "claude", "model": "opus"},
		"crosscheck":  {"default_mode": "autonomous", "cli": "codex", "model": "gpt-5.6-sol"},
		"implementor": {"default_mode": "autonomous", "cli": "codex", "model": "gpt-5.6-terra"},
		"tester":      {"default_mode": "autonomous", "cli": "claude", "model": "sonnet"},
	}
	for name, want := range wantManaged {
		if diff := cmp.Diff(want, agents[name]); diff != "" {
			t.Errorf("%s mismatch (-want +got):\n%s", name, diff)
		}
		if _, ok := agents[name].(map[string]any)["extends"]; ok {
			t.Errorf("%s unexpectedly uses extends", name)
		}
	}
	for _, name := range []string{"interactive_base", "autonomous_base", "summarizer", "team_implementor"} {
		if _, ok := agents[name]; !ok {
			t.Errorf("unmanaged agent %s was not preserved", name)
		}
	}
	for _, name := range []string{"planner", "reviewer"} {
		if _, ok := agents[name]; ok {
			t.Errorf("legacy alias %s was not removed", name)
		}
	}
	if profiles["production"].(map[string]any)["agents"].(map[string]any)["planner"] == nil {
		t.Fatal("legacy alias outside profiles.default was changed")
	}
	if got["other_top_level"] != true {
		t.Fatal("unrelated top-level content was not preserved")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("target mode = %o, want 600", got)
	}
}

func TestWriteOmitsEmptyModels(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	err := Write(&Request{
		TargetPath:     target,
		LeadCLI:        "claude",
		CrosscheckCLI:  "codex",
		ImplementorCLI: "cursor",
		TesterCLI:      "opencode",
	})
	if err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}
	agents := readYAMLMap(t, target)["profiles"].(map[string]any)["default"].(map[string]any)["agents"].(map[string]any)
	for _, name := range []string{"lead", "crosscheck", "implementor", "tester"} {
		if _, ok := agents[name].(map[string]any)["model"]; ok {
			t.Errorf("%s included an empty model", name)
		}
	}
}

func TestRequestValidationRequiresTargetAndEveryCLI(t *testing.T) {
	valid := Request{
		TargetPath:     "config.yaml",
		LeadCLI:        "claude",
		CrosscheckCLI:  "codex",
		ImplementorCLI: "codex",
		TesterCLI:      "claude",
	}
	tests := []struct {
		name    string
		mutate  func(*Request)
		wantErr string
	}{
		{"target", func(r *Request) { r.TargetPath = "" }, "target_path"},
		{"lead", func(r *Request) { r.LeadCLI = "" }, "lead_cli"},
		{"crosscheck", func(r *Request) { r.CrosscheckCLI = "" }, "crosscheck_cli"},
		{"implementor", func(r *Request) { r.ImplementorCLI = "" }, "implementor_cli"},
		{"tester", func(r *Request) { r.TesterCLI = "" }, "tester_cli"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid
			tt.mutate(&req)
			err := Write(&req)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Write() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestCollisionsReportsSortedCanonicalAndLegacyManagedEntriesOnly(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.yaml")
	mustWrite(t, target, `
profiles:
  default:
    agents:
      tester: {}
      summarizer: {}
      reviewer: {}
      autonomous_base: {}
      lead: {}
      planner: {}
      team_implementor: {}
      implementor: {}
      crosscheck: {}
`, 0o600)

	got, err := Collisions(target)
	if err != nil {
		t.Fatalf("Collisions() returned error: %v", err)
	}
	want := []string{"crosscheck", "implementor", "lead", "planner", "reviewer", "tester"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Collisions() mismatch (-want +got):\n%s", diff)
	}
}

func TestCollisionsRejectsNonMappingProfilePath(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"root", "- not\n- a\n- mapping\n", "config root must be a mapping"},
		{"profiles", "profiles: disabled\n", "profiles must be a mapping"},
		{"default", "profiles:\n  default: []\n", "profiles.default must be a mapping"},
		{"agents", "profiles:\n  default:\n    agents: false\n", "profiles.default.agents must be a mapping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "config.yaml")
			mustWrite(t, target, tt.body, 0o600)
			_, err := Collisions(target)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Collisions() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestWriteRejectsMalformedOrNonMappingYAMLWithoutChangingOriginal(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed", "profiles: [\n"},
		{"root", "- not\n- a\n- mapping\n"},
		{"profiles", "profiles: disabled\n"},
		{"default", "profiles:\n  default: []\n"},
		{"agents", "profiles:\n  default:\n    agents: false\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "config.yaml")
			mustWrite(t, target, tt.body, 0o600)
			err := Write(&Request{
				TargetPath: target, LeadCLI: "claude", CrosscheckCLI: "codex",
				ImplementorCLI: "codex", TesterCLI: "claude",
			})
			if err == nil {
				t.Fatal("Write() returned nil error")
			}
			got, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatalf("read original: %v", readErr)
			}
			if string(got) != tt.body {
				t.Fatalf("original changed after failed write:\n%s", got)
			}
		})
	}
}

func TestAtomicWriteFailureLeavesTargetAndNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target collision: %v", err)
	}

	err := writeAtomic0600(target, []byte("replacement"))
	if err == nil || !strings.Contains(err.Error(), "rename temporary file") {
		t.Fatalf("writeAtomic0600() error = %v, want rename failure", err)
	}
	info, statErr := os.Stat(target)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("original target changed: info=%v err=%v", info, statErr)
	}
	matches, globErr := filepath.Glob(filepath.Join(dir, ".agent-runner-*.tmp"))
	if globErr != nil {
		t.Fatalf("glob temporary files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left after failure: %v", matches)
	}
}

func TestWriteCreatesMissingParentAndPreservesExistingParentMode(t *testing.T) {
	t.Run("creates missing parent", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "home", ".agent-runner", "config.yaml")
		writeValid(t, target)
		info, err := os.Stat(filepath.Dir(target))
		if err != nil {
			t.Fatalf("stat parent: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Fatalf("parent mode = %o, want 755", got)
		}
	})
	t.Run("preserves existing parent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), ".agent-runner")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}
		writeValid(t, filepath.Join(dir, "config.yaml"))
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat parent: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("parent mode = %o, want 700", got)
		}
	})
}

func writeValid(t *testing.T, target string) {
	t.Helper()
	req := validRequest(target)
	if err := Write(&req); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}
}

func validRequest(target string) Request {
	return Request{
		TargetPath: target, LeadCLI: "claude", CrosscheckCLI: "codex",
		ImplementorCLI: "codex", TesterCLI: "claude",
	}
}

func mustWrite(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readYAMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return got
}
