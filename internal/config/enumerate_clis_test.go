package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestEnumerateCLIs_MergesUserAndProjectConfigs(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	projectPath := filepath.Join(dir, "project.yaml")

	os.WriteFile(globalPath, []byte(`
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
      implementor:
        default_mode: headless
        cli: codex
`), 0o600)

	os.WriteFile(projectPath, []byte(`
profiles:
  default:
    agents:
      reviewer:
        default_mode: headless
        cli: copilot
`), 0o600)

	got, err := EnumerateCLIs(globalPath, projectPath)
	if err != nil {
		t.Fatalf("EnumerateCLIs() error = %v", err)
	}
	want := []string{"claude", "codex", "copilot"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("EnumerateCLIs() mismatch (-want +got):\n%s", diff)
	}
}

func TestEnumerateCLIs_DeduplicatesAcrossProfiles(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")

	os.WriteFile(globalPath, []byte(`
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
      implementor:
        default_mode: headless
        cli: claude
  other:
    agents:
      worker:
        default_mode: headless
        cli: claude
`), 0o600)

	got, err := EnumerateCLIs(globalPath, "")
	if err != nil {
		t.Fatalf("EnumerateCLIs() error = %v", err)
	}
	want := []string{"claude"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("EnumerateCLIs() mismatch (-want +got):\n%s", diff)
	}
}

func TestEnumerateCLIs_MissingFilesAreEmpty(t *testing.T) {
	got, err := EnumerateCLIs("/no/such/global.yaml", "/no/such/project.yaml")
	if err != nil {
		t.Fatalf("EnumerateCLIs() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("EnumerateCLIs() = %v, want empty", got)
	}
}

func TestEnumerateCLIs_SkipsEmptyCLIValues(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")

	os.WriteFile(globalPath, []byte(`
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
      overlay:
        extends: planner
`), 0o600)

	got, err := EnumerateCLIs(globalPath, "")
	if err != nil {
		t.Fatalf("EnumerateCLIs() error = %v", err)
	}
	want := []string{"claude"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("EnumerateCLIs() mismatch (-want +got):\n%s", diff)
	}
}

func TestEnumerateCLIs_ScansCLIsFromAllProfiles(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")

	os.WriteFile(globalPath, []byte(`
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
  staging:
    agents:
      planner:
        default_mode: interactive
        cli: cursor
  production:
    agents:
      planner:
        default_mode: interactive
        cli: copilot
`), 0o600)

	got, err := EnumerateCLIs(globalPath, "")
	if err != nil {
		t.Fatalf("EnumerateCLIs() error = %v", err)
	}
	want := []string{"claude", "copilot", "cursor"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("EnumerateCLIs() mismatch (-want +got):\n%s", diff)
	}
}
