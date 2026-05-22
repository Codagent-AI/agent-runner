package agentplugin

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestResolve_BuildsCorrectPlanForUserScope(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "/usr/local/bin/agent-plugin", nil }
	t.Cleanup(func() { lookPath = orig })

	plan, err := Resolve(&Request{
		CLIs:  []string{"claude", "codex"},
		Scope: "global",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if plan.Binary != "/usr/local/bin/agent-plugin" {
		t.Fatalf("Binary = %q, want /usr/local/bin/agent-plugin", plan.Binary)
	}
	want := []string{"claude", "codex"}
	if diff := cmp.Diff(want, plan.CLIs); diff != "" {
		t.Fatalf("CLIs mismatch (-want +got):\n%s", diff)
	}
	if plan.Project {
		t.Fatal("Project = true, want false for global scope")
	}
}

func TestResolve_SetsProjectFlagForProjectScope(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "/usr/local/bin/agent-plugin", nil }
	t.Cleanup(func() { lookPath = orig })

	plan, err := Resolve(&Request{
		CLIs:  []string{"claude"},
		Scope: "project",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !plan.Project {
		t.Fatal("Project = false, want true for project scope")
	}
}

func TestResolve_ReturnsBinaryMissingWhenNotOnPATH(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = orig })

	_, err := Resolve(&Request{CLIs: []string{"claude"}, Scope: "global"})
	if !errors.Is(err, ErrBinaryMissing) {
		t.Fatalf("Resolve() error = %v, want ErrBinaryMissing", err)
	}
}

func TestResolve_NilRequestReturnsError(t *testing.T) {
	_, err := Resolve(nil)
	if err == nil {
		t.Fatal("Resolve(nil) error = nil, want error")
	}
}

func TestResolve_EmptyCLIsReturnsNilPlan(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "/usr/local/bin/agent-plugin", nil }
	t.Cleanup(func() { lookPath = orig })

	plan, err := Resolve(&Request{CLIs: nil, Scope: "global"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if plan != nil {
		t.Fatalf("Resolve() = %+v, want nil for empty CLIs", plan)
	}
}

func TestDryRun_InvokesBinaryWithDryRunFlag(t *testing.T) {
	var captured []string
	orig := runCommand
	runCommand = func(binary string, args []string) (string, error) {
		captured = append(captured, binary)
		captured = append(captured, args...)
		return "Would install agent-skills for claude, codex", nil
	}
	t.Cleanup(func() { runCommand = orig })

	plan := &Plan{
		Binary:  "/usr/local/bin/agent-plugin",
		CLIs:    []string{"claude", "codex"},
		Project: false,
	}
	preview, err := DryRun(plan)
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	wantArgs := []string{"/usr/local/bin/agent-plugin", "add", Source, "--agent", "claude", "--agent", "codex", "--dry-run"}
	if diff := cmp.Diff(wantArgs, captured); diff != "" {
		t.Fatalf("command mismatch (-want +got):\n%s", diff)
	}
	if preview.Output != "Would install agent-skills for claude, codex" {
		t.Fatalf("Output = %q", preview.Output)
	}
}

func TestDryRun_IncludesProjectFlag(t *testing.T) {
	var captured []string
	orig := runCommand
	runCommand = func(binary string, args []string) (string, error) {
		captured = append(captured, binary)
		captured = append(captured, args...)
		return "", nil
	}
	t.Cleanup(func() { runCommand = orig })

	plan := &Plan{
		Binary:  "/usr/local/bin/agent-plugin",
		CLIs:    []string{"claude"},
		Project: true,
	}
	_, err := DryRun(plan)
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	found := false
	for _, arg := range captured {
		if arg == "--project" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --project in args: %v", captured)
	}
}

func TestInstall_InvokesBinaryWithYesFlag(t *testing.T) {
	var captured []string
	orig := runCommand
	runCommand = func(binary string, args []string) (string, error) {
		captured = append(captured, binary)
		captured = append(captured, args...)
		return "Installed agent-skills for claude", nil
	}
	t.Cleanup(func() { runCommand = orig })

	plan := &Plan{
		Binary:  "/usr/local/bin/agent-plugin",
		CLIs:    []string{"claude"},
		Project: false,
	}
	result, err := Install(plan)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	wantArgs := []string{"/usr/local/bin/agent-plugin", "add", Source, "--agent", "claude", "--yes"}
	if diff := cmp.Diff(wantArgs, captured); diff != "" {
		t.Fatalf("command mismatch (-want +got):\n%s", diff)
	}
	if result.Output != "Installed agent-skills for claude" {
		t.Fatalf("Output = %q", result.Output)
	}
}

func TestInstall_CapturesWarningOnNonZeroExit(t *testing.T) {
	orig := runCommand
	runCommand = func(string, []string) (string, error) {
		return "partial output", errors.New("exit status 1")
	}
	t.Cleanup(func() { runCommand = orig })

	plan := &Plan{
		Binary: "/usr/local/bin/agent-plugin",
		CLIs:   []string{"claude"},
	}
	result, err := Install(plan)
	if err != nil {
		t.Fatalf("Install() error = %v, want nil (non-fatal)", err)
	}
	if result.Warning == "" {
		t.Fatal("expected a warning for non-zero exit")
	}
	if !strings.Contains(result.Warning, "exit status 1") {
		t.Fatalf("Warning = %q, want exit status mention", result.Warning)
	}
}

func TestBuildArgs_ConstructsCorrectArgList(t *testing.T) {
	plan := &Plan{
		Binary:  "/usr/local/bin/agent-plugin",
		CLIs:    []string{"claude", "codex", "copilot"},
		Project: true,
	}
	got := buildArgs(plan, "--yes")
	want := []string{"add", Source, "--agent", "claude", "--agent", "codex", "--agent", "copilot", "--project", "--yes"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("buildArgs() mismatch (-want +got):\n%s", diff)
	}
}
