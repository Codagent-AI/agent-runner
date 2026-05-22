package native

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/profilewrite"
	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestFirstSurfaceIsCLISelectionNotWelcome(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})
	view := m.View()
	if !strings.Contains(view, "claude") {
		t.Fatalf("first surface should show detected adapters:\n%s", view)
	}
}

func TestNoSkipDismissOnSelectionScreens(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})
	view := strings.ToLower(m.View())
	for _, forbidden := range []string{"skip", "dismiss", "not now", "not-now"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("selection view contains forbidden action %q:\n%s", forbidden, view)
		}
	}
}

func TestAdapterDetectionFailureIsFirstSurface(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return nil, nil }),
	})
	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want failed", m.Result())
	}
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "no supported CLI") {
		t.Fatalf("Err() = %v, want no supported CLI error", m.Err())
	}
}

// With models for claude returning ["opus"] and codex returning nil:
// Enter 1: interactive CLI (claude) → model screen
// Enter 2: interactive model (opus) → headless CLI
// Enter 3: headless CLI (codex) → autonomous backend
// Enter 4: autonomous backend (interactive-claude) → default headless model notice
// Enter 5: continue → scope
// Enter 6: scope (global) → write → demo prompt
func TestSetupCompletesAndShowsDemoPrompt(t *testing.T) {
	var wrote []profilewrite.Request
	var saved []usersettings.Settings
	deps := Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex"}, nil }),
		Models: ModelDiscovererFunc(func(adapter string) ([]string, error) {
			switch adapter {
			case "claude":
				return []string{"opus"}, nil
			default:
				return nil, nil
			}
		}),
		Settings: SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
			saved = append(saved, mutator(usersettings.Settings{}))
			return nil
		}),
		Profiles:   ProfileWriterFunc(func(req *profilewrite.Request) error { wrote = append(wrote, *req); return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Clock:      func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
	}
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter")

	view := m.View()
	if !strings.Contains(view, "Continue") || !strings.Contains(view, "Not now") || !strings.Contains(view, "Dismiss") {
		t.Fatalf("expected demo prompt with Continue/Not now/Dismiss:\n%s", view)
	}

	if len(wrote) != 1 {
		t.Fatalf("profile writes = %d, want 1", len(wrote))
	}
	wantWrite := profilewrite.Request{
		TargetPath:       "/home/me/.agent-runner/config.yaml",
		InteractiveCLI:   "claude",
		InteractiveModel: "opus",
		HeadlessCLI:      "codex",
	}
	if diff := cmp.Diff(wantWrite, wrote[0]); diff != "" {
		t.Fatalf("write request mismatch (-want +got):\n%s", diff)
	}

	wantSaved := []usersettings.Settings{{
		AutonomousBackend: usersettings.BackendInteractiveClaude,
		Setup:             usersettings.SetupSettings{CompletedAt: "2026-05-04T12:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved settings mismatch (-want +got):\n%s", diff)
	}
}

func TestAutonomousBackendScreenAppearsAfterImplementorCLI(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			return nil
		}),
	})

	sendKeys(t, m, "enter", "enter", "enter")

	view := m.View()
	for _, want := range []string{
		"Autonomous Backend",
		"Headless",
		"Interactive",
		"Interactive for Claude",
		"non-interactive print mode",
		"interactive session with autonomy instructions",
		"Claude only",
		"Interactive for Claude -",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("autonomous backend screen missing %q:\n%s", want, view)
		}
	}
}

func TestAutonomousBackendSelectionPersistsOnSetupCompletion(t *testing.T) {
	var saved []usersettings.Settings
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
			saved = append(saved, mutator(usersettings.Settings{}))
			return nil
		}),
		Clock:   func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "up", "enter", "enter", "enter")

	if len(saved) != 1 {
		t.Fatalf("settings saves = %d, want 1", len(saved))
	}
	if saved[0].AutonomousBackend != usersettings.BackendInteractive {
		t.Fatalf("AutonomousBackend = %q, want interactive", saved[0].AutonomousBackend)
	}
	if saved[0].Setup.CompletedAt != "2026-05-04T12:00:00Z" {
		t.Fatalf("Setup.CompletedAt = %q, want timestamp", saved[0].Setup.CompletedAt)
	}
}

func TestCancelledSetupDoesNotPersistAutonomousBackend(t *testing.T) {
	saved := false
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			saved = true
			return nil
		}),
	})

	sendKeys(t, m, "enter", "enter", "enter", "esc")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want cancelled", m.Result())
	}
	if saved {
		t.Fatal("settings were saved after setup cancellation")
	}
}

// With model discovery returning nil:
// Enter 1: interactive CLI → default planner model notice
// Enter 2: Continue → headless CLI
// Enter 3: headless CLI → default implementor model notice
// Enter 4: Continue → scope
// Enter 5: scope → write → demo prompt
// Enter 6: Continue
func TestDemoPromptContinueReturnsResultDemo(t *testing.T) {
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings:   SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultDemo {
		t.Fatalf("Result() = %v, want ResultDemo; view=\n%s", m.Result(), m.View())
	}
}

// Enter 1-5: CLI/default/headless/default/scope → demo prompt
// Down: focus "Not now"
// Enter 6: Not now
func TestDemoPromptNotNowReturnsCompleted(t *testing.T) {
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings:   SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "down", "enter")

	if m.Result() != ResultCompleted {
		t.Fatalf("Result() = %v, want ResultCompleted", m.Result())
	}
}

// Enter 1-5: get to demo prompt
// Down twice: focus "Dismiss"
// Enter 6: Dismiss
func TestDemoPromptDismissWritesDismissedSetting(t *testing.T) {
	var saved []usersettings.Settings
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
			saved = append(saved, mutator(usersettings.Settings{}))
			return nil
		}),
		Clock:   func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "down", "down", "enter")

	if m.Result() != ResultCompleted {
		t.Fatalf("Result() = %v, want ResultCompleted", m.Result())
	}
	if len(saved) < 2 {
		t.Fatalf("expected at least 2 saves (setup + dismiss), got %d", len(saved))
	}
	dismissSave := saved[len(saved)-1]
	if dismissSave.Onboarding.Dismissed != "2026-05-04T12:00:00Z" {
		t.Fatalf("Onboarding.Dismissed = %q, want 2026-05-04T12:00:00Z", dismissSave.Onboarding.Dismissed)
	}
}

func TestDemoPromptSkippedWhenOnboardingAlreadyCompleted(t *testing.T) {
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			return nil
		}),
		HomeDir:             func() (string, error) { return "/home/me", nil },
		Cwd:                 func() (string, error) { return "/work/project", nil },
		OnboardingCompleted: true,
	})

	// CLI → default model → headless CLI → default model → scope → write → done (no demo prompt)
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultCompleted {
		t.Fatalf("Result() = %v, want ResultCompleted; view=\n%s", m.Result(), m.View())
	}
}

// Enter 1: CLI → skip model → headless CLI
// Esc: cancel
func TestCancelBeforeWriteLeavesSetupIncomplete(t *testing.T) {
	saved := false
	wrote := false
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			saved = true
			return nil
		}),
		Profiles: ProfileWriterFunc(func(*profilewrite.Request) error {
			wrote = true
			return nil
		}),
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter")
	sendKey(t, m, "esc")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want cancelled", m.Result())
	}
	if saved || wrote {
		t.Fatalf("saved=%v wrote=%v, want no side effects", saved, wrote)
	}
}

func TestCtrlCBeforeWriteRequestsExit(t *testing.T) {
	saved := false
	wrote := false
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			saved = true
			return nil
		}),
		Profiles: ProfileWriterFunc(func(*profilewrite.Request) error {
			wrote = true
			return nil
		}),
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	})

	cmd := sendKeyRaw(t, m, "ctrl+c")

	if m.Result() != ResultExitRequested {
		t.Fatalf("Result() = %v, want exit requested", m.Result())
	}
	if cmd == nil {
		t.Fatal("ctrl+c should quit setup")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	if saved || wrote {
		t.Fatalf("saved=%v wrote=%v, want no side effects", saved, wrote)
	}
}

// Enter 1-5: CLI/default/headless/default/scope → overwrite screen
// Down: focus "Cancel"
// Enter 6: Cancel
func TestOverwriteConfirmationCanCancelBeforeWrite(t *testing.T) {
	wrote := false
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return []string{"planner"}, nil }),
		Profiles: ProfileWriterFunc(func(*profilewrite.Request) error {
			wrote = true
			return nil
		}),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
		HomeDir:  func() (string, error) { return "/home/me", nil },
		Cwd:      func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "down", "enter")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want cancelled; view=\n%s", m.Result(), m.View())
	}
	if wrote {
		t.Fatal("profile writer was called after overwrite cancel")
	}
}

// Enter 1-5: CLI/default/headless/default/scope → write fails
func TestWriteFailureReturnsFailedWithoutRecordingSetup(t *testing.T) {
	saved := false
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles: ProfileWriterFunc(func(*profilewrite.Request) error {
			return errors.New("disk full")
		}),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			saved = true
			return nil
		}),
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want failed; view=\n%s", m.Result(), m.View())
	}
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "disk full") {
		t.Fatalf("Err() = %v, want disk full", m.Err())
	}
	if saved {
		t.Fatal("settings were saved after profile write failure")
	}
}

// Enter 1-5: CLI/default/headless/default/scope → demo prompt
// Down: Not now
// Enter 6: Not now
func TestModelDiscoveryErrorUsesAdapterDefault(t *testing.T) {
	var wrote []profilewrite.Request
	var discoveryCalls int
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models: ModelDiscovererFunc(func(string) ([]string, error) {
			discoveryCalls++
			return nil, errors.New("permission denied")
		}),
		Profiles:   ProfileWriterFunc(func(req *profilewrite.Request) error { wrote = append(wrote, *req); return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings:   SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "down", "enter")

	if m.Result() != ResultCompleted {
		t.Fatalf("Result() = %v, want completed; err=%v view=\n%s", m.Result(), m.Err(), m.View())
	}
	if discoveryCalls != 2 {
		t.Fatalf("discoveryCalls = %d, want 2", discoveryCalls)
	}
	if len(wrote) != 1 {
		t.Fatalf("profile writes = %d, want 1", len(wrote))
	}
	if wrote[0].InteractiveModel != "" || wrote[0].HeadlessModel != "" {
		t.Fatalf("write request models = interactive %q headless %q, want adapter defaults", wrote[0].InteractiveModel, wrote[0].HeadlessModel)
	}
}

func TestEmptyModelDiscoveryRequiresContinue(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})

	sendKey(t, m, "enter")

	view := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(view, "No selectable models were found for claude") {
		t.Fatalf("expected default-model notice after empty discovery:\n%s", view)
	}
	if !strings.Contains(view, "▶ Continue") {
		t.Fatalf("expected explicit Continue action after empty discovery:\n%s", view)
	}
	if m.stage != stageInteractiveModelDefault {
		t.Fatalf("stage = %v, want stageInteractiveModelDefault", m.stage)
	}
}

func TestDemoOnlyModeShowsDemoPromptDirectly(t *testing.T) {
	m := NewDemoPromptModel(&Deps{
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
	})

	view := m.View()
	if !strings.Contains(view, "Continue") || !strings.Contains(view, "Not now") || !strings.Contains(view, "Dismiss") {
		t.Fatalf("demo-only model should show Continue/Not now/Dismiss:\n%s", view)
	}

	sendKeys(t, m, "enter")
	if m.Result() != ResultDemo {
		t.Fatalf("Result() = %v, want ResultDemo", m.Result())
	}
}

func TestParseModelOutputMatchesSupportedAdapters(t *testing.T) {
	claude := parseModelOutput("claude", "Available models:\n* claude-opus-4-1\n* claude-sonnet-4-5\n")
	if len(claude) != 2 || claude[0] != "claude-opus-4-1" || claude[1] != "claude-sonnet-4-5" {
		t.Fatalf("claude models = %v", claude)
	}

	codex := parseModelOutput("codex", `{"slug":"gpt-5.4","visibility":"list"},{"slug":"hidden","visibility":"internal"}`)
	if len(codex) != 1 || codex[0] != "gpt-5.4" {
		t.Fatalf("codex models = %v", codex)
	}

	opencode := parseModelOutput("opencode", "anthropic/claude-sonnet-4-5\nopenai/gpt-5.2\nanthropic/claude-sonnet-4-5\n")
	if len(opencode) != 2 || opencode[0] != "anthropic/claude-sonnet-4-5" || opencode[1] != "openai/gpt-5.2" {
		t.Fatalf("opencode models = %v", opencode)
	}
}

func TestParseCodexModelsHandlesEnvelopeFormat(t *testing.T) {
	envelope := `{"models":[{"slug":"gpt-5.5","visibility":"list"},{"slug":"internal","visibility":"internal"}]}`
	models := parseCodexModels(envelope)
	if len(models) != 1 || models[0] != "gpt-5.5" {
		t.Fatalf("codex envelope models = %v, want [gpt-5.5]", models)
	}
}

func TestSubprocessModelsReturnsClaudeAliasesWithoutSubprocess(t *testing.T) {
	origCommandContext := modelCommandContext
	modelCommandContext = func(context.Context, string, ...string) *exec.Cmd {
		t.Fatal("claude model discovery should not spawn a subprocess")
		return nil
	}
	t.Cleanup(func() {
		modelCommandContext = origCommandContext
	})

	models, err := SubprocessModels{}.ModelsFor("claude")
	if err != nil {
		t.Fatalf("ModelsFor() error = %v", err)
	}
	want := []string{"opus", "sonnet"}
	if diff := cmp.Diff(want, models); diff != "" {
		t.Fatalf("claude models mismatch (-want +got):\n%s", diff)
	}
}

func TestSubprocessModelsTimeoutReturnsError(t *testing.T) {
	origTimeout := subprocessModelTimeout
	origCommandContext := modelCommandContext
	subprocessModelTimeout = 20 * time.Millisecond
	modelCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "sleep 10")
	}
	t.Cleanup(func() {
		subprocessModelTimeout = origTimeout
		modelCommandContext = origCommandContext
	})

	_, err := SubprocessModels{}.ModelsFor("opencode")
	if err == nil {
		t.Fatal("ModelsFor() error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("ModelsFor() error = %v, want context deadline exceeded", err)
	}
}

func TestViewCentersContentInLargeTerminal(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) < 10 {
		t.Fatalf("centered view should have reasonable content, got %d lines", len(lines))
	}
}

func TestViewFallsBackToTopLeftInSmallTerminal(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		first := 0
		for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
			first++
		}
		if first > 2 {
			t.Fatalf("small terminal should not center: %d leading blank lines", first)
		}
	}
}

func TestSetupPanelUsesReadableWidthInLargeTerminal(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex", "copilot"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	panel := tuistyle.Sanitize(m.renderPanel())
	if got, want := maxLineWidth(panel), setupPanelWidth(120); got > want {
		t.Fatalf("panel width = %d, want <= %d:\n%s", got, want, panel)
	}
	if strings.Contains(panel, "Welcome. Agent Runner uses a planner for interactive work and an implementor for unattended implementation tasks.") {
		t.Fatalf("first paragraph should wrap inside the setup panel:\n%s", panel)
	}

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got, want := maxLineWidth(tuistyle.Sanitize(m.renderPanel())), 80; got > want {
		t.Fatalf("80-column panel width = %d, want <= %d", got, want)
	}
}

func TestWrappedCopyAvoidsShortOrphanWords(t *testing.T) {
	m := NewDemoPromptModel(&Deps{
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
	})

	panel := tuistyle.Sanitize(m.renderPanel())
	for _, line := range strings.Split(panel, "\n") {
		switch strings.TrimSpace(line) {
		case "UI", "data":
			t.Fatalf("wrapped copy should not orphan short words on their own line:\n%s", panel)
		}
	}
	if !strings.Contains(panel, "through UI") {
		t.Fatalf("wrapped copy should keep short word UI with neighboring text:\n%s", panel)
	}
}

func TestDemoPromptRendersHorizontalButtons(t *testing.T) {
	m := NewDemoPromptModel(&Deps{
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
	})

	panel := tuistyle.Sanitize(m.renderPanel())
	buttonLine := lineContaining(strings.Split(panel, "\n"), "[ Continue ]")
	if buttonLine < 0 {
		t.Fatalf("demo prompt should render Continue as a button:\n%s", panel)
	}
	line := strings.Split(panel, "\n")[buttonLine]
	for _, want := range []string{"[ Continue ]", "[ Not now ]", "[ Dismiss ]"} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected all demo actions on one button row, missing %q:\n%s", want, panel)
		}
	}
	if strings.Contains(panel, "▶ Continue") || strings.Contains(panel, "\n  Not now") || strings.Contains(panel, "\n  Dismiss") {
		t.Fatalf("demo prompt should not render the actions as a vertical option list:\n%s", panel)
	}
}

func TestNativeSetupShowsWizardProgress(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})

	panel := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "Step 1 of 7") {
		t.Fatalf("first setup screen should show wizard progress:\n%s", panel)
	}
	lines := strings.Split(panel, "\n")
	progressLine := lineContaining(lines, "Step 1 of 7")
	titleLine := lineContaining(lines, "Set Up Agent Runner")
	if progressLine < 0 || titleLine < 0 || progressLine >= titleLine {
		t.Fatalf("wizard progress should appear above the screen heading:\n%s", panel)
	}
	if leadingColumns(lines[progressLine], "Step 1 of 7") <= leadingColumns(lines[titleLine], "Set Up Agent Runner") {
		t.Fatalf("wizard progress should be centered above the left-aligned heading:\n%s", panel)
	}

	sendKey(t, m, "enter")
	panel = tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "Step 2 of 7") {
		t.Fatalf("model default screen should be setup step 2:\n%s", panel)
	}
}

func TestOverwriteScreenAddsWizardProgressStep(t *testing.T) {
	m := NewModel(&Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return []string{"planner"}, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Settings:   SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter")
	panel := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "Step 7 of 8") {
		t.Fatalf("overwrite screen should add a wizard progress step:\n%s", panel)
	}

	sendKey(t, m, "enter")
	panel = tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "Step 8 of 8") {
		t.Fatalf("demo prompt after overwrite should be final wizard step:\n%s", panel)
	}
}

func TestDemoOnlyPromptDoesNotShowSetupProgress(t *testing.T) {
	m := NewDemoPromptModel(&Deps{
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
	})

	panel := tuistyle.Sanitize(m.renderPanel())
	if strings.Contains(panel, "Step ") {
		t.Fatalf("demo-only prompt should not show native setup progress:\n%s", panel)
	}
}

func TestDemoPromptLeftRightChangesSelection(t *testing.T) {
	m := NewDemoPromptModel(&Deps{
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
	})

	if got := m.options[m.focus]; got != "Continue" {
		t.Fatalf("initial demo prompt focus = %q, want Continue", got)
	}
	sendKey(t, m, "right")
	if got := m.options[m.focus]; got != "Not now" {
		t.Fatalf("focus after right = %q, want Not now", got)
	}
	sendKey(t, m, "right")
	if got := m.options[m.focus]; got != "Dismiss" {
		t.Fatalf("focus after second right = %q, want Dismiss", got)
	}
	sendKey(t, m, "left")
	if got := m.options[m.focus]; got != "Not now" {
		t.Fatalf("focus after left = %q, want Not now", got)
	}
}

func TestSetupPanelKeepsOptionsNearPrompt(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})

	lines := strings.Split(tuistyle.Sanitize(m.renderPanel()), "\n")
	promptLine := lineContaining(lines, "Choose the CLI for the planner agent.")
	optionLine := lineContaining(lines, "claude")
	if promptLine < 0 || optionLine < 0 {
		t.Fatalf("promptLine=%d optionLine=%d lines:\n%s", promptLine, optionLine, strings.Join(lines, "\n"))
	}
	if optionLine-promptLine > 3 {
		t.Fatalf("first option is too far from prompt: prompt line %d option line %d\n%s", promptLine, optionLine, strings.Join(lines, "\n"))
	}
}

func TestPlannerCLIRecommendsAndDefaultsToClaude(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"codex", "claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})

	panel := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "▶ claude (recommended)") {
		t.Fatalf("planner CLI should default to recommended claude:\n%s", panel)
	}
	if got := m.options[m.focus]; got != "claude" {
		t.Fatalf("focused planner CLI = %q, want claude", got)
	}
}

func TestImplementorCLIRecommendsAndDefaultsToCodex(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return []string{"opus"}, nil }),
	})

	sendKeys(t, m, "enter", "enter")

	panel := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "▶ codex (recommended)") {
		t.Fatalf("implementor CLI should default to recommended codex:\n%s", panel)
	}
	if got := m.options[m.focus]; got != "codex" {
		t.Fatalf("focused implementor CLI = %q, want codex", got)
	}
}

func TestImplementorCLIShowsClaudeProgrammaticBillingDisclaimer(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return []string{"opus"}, nil }),
	})

	sendKeys(t, m, "enter", "enter")

	panel := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "claude (programmatic use may be billed at API rates)") {
		t.Fatalf("implementor CLI should warn about Claude programmatic billing:\n%s", panel)
	}
}

func TestPlannerModelScreenDoesNotRepeatPlannerAgentLabel(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return []string{"gpt-5.5"}, nil }),
	})

	sendKey(t, m, "enter")

	panel := tuistyle.Sanitize(m.renderPanel())
	if strings.Contains(panel, "Planner agent") {
		t.Fatalf("planner model screen should not repeat planner agent label:\n%s", panel)
	}
}

func TestTransitionDimsInIncomingPanelWithoutOutgoingScroll(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return []string{"gpt-5.5"}, nil }),
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	sendKeyRaw(t, m, "enter")
	m.animFrame = 1

	view := tuistyle.Sanitize(m.View())
	if strings.Contains(view, "Welcome. Agent Runner uses a planner") {
		t.Fatalf("transition should not keep outgoing panel visible:\n%s", view)
	}
	if !strings.Contains(view, "Planner Model") {
		t.Fatalf("transition should show incoming panel immediately:\n%s", view)
	}
	if !strings.Contains(view, "Preparing next step") {
		t.Fatalf("transition should show a wait indicator:\n%s", view)
	}
}

func TestModelDiscoveryLoadingUsesModelScreen(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"codex"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return []string{"gpt-5.5"}, nil }),
	})

	_ = sendKeyRaw(t, m, "enter")

	panel := tuistyle.Sanitize(m.renderPanel())
	if !strings.Contains(panel, "Planner Model") {
		t.Fatalf("loading should render the planner model screen:\n%s", panel)
	}
	if !strings.Contains(panel, "Checking available models for codex.") {
		t.Fatalf("loading should show model discovery status:\n%s", panel)
	}
	if strings.Contains(panel, "Discovering Planner Models") {
		t.Fatalf("loading should not use a separate discovering screen:\n%s", panel)
	}
}

func TestFirstScreenIncludesWelcomeLanguage(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
	})
	view := strings.ToLower(m.View())
	if !strings.Contains(view, "welcome") && !strings.Contains(view, "set up") {
		t.Fatalf("first screen should include welcoming language:\n%s", view)
	}
}

func maxLineWidth(s string) int {
	maxWidth := 0
	for _, line := range strings.Split(s, "\n") {
		maxWidth = max(maxWidth, runewidth.StringWidth(line))
	}
	return maxWidth
}

func lineContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func leadingColumns(line, needle string) int {
	idx := strings.Index(line, needle)
	if idx < 0 {
		return -1
	}
	return runewidth.StringWidth(line[:idx])
}

func sendKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, key := range keys {
		sendKey(t, m, key)
	}
}

func sendKey(t *testing.T, m *Model, key string) {
	t.Helper()
	cmd := sendKeyRaw(t, m, key)
	runTestCmd(t, m, cmd)
	settleAnimation(m)
}

func sendKeyRaw(t *testing.T, m *Model, key string) tea.Cmd {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEscape}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	next, cmd := m.Update(msg)
	if updated, ok := next.(*Model); ok {
		_ = updated
	}
	return cmd
}

func runTestCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil:
		return
	case animTick:
		return
	case loadingTick:
		return
	case tea.BatchMsg:
		for _, batched := range msg {
			runTestCmd(t, m, batched)
		}
	default:
		next, nextCmd := m.Update(msg)
		if updated, ok := next.(*Model); ok {
			_ = updated
		}
		runTestCmd(t, m, nextCmd)
	}
}

func settleAnimation(m *Model) {
	m.animFrame = 0
	m.animDone = true
	m.prevView = ""
	if m.pending != nil {
		pending := *m.pending
		m.pending = nil
		_ = m.applyModelsLoaded(pending)
		m.animFrame = 0
		m.animDone = true
		m.prevView = ""
	}
}
