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

	"github.com/codagent/agent-runner/internal/profilewrite"
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
// Enter 3: headless CLI (claude) → model screen
// Enter 4: headless model (opus) → scope
// Enter 5: scope (global) → write → demo prompt
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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

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
		HeadlessCLI:      "claude",
		HeadlessModel:    "opus",
	}
	if diff := cmp.Diff(wantWrite, wrote[0]); diff != "" {
		t.Fatalf("write request mismatch (-want +got):\n%s", diff)
	}

	wantSaved := []usersettings.Settings{{
		Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T12:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved settings mismatch (-want +got):\n%s", diff)
	}
}

// With model discovery returning nil (skip both models):
// Enter 1: interactive CLI → skip model → headless CLI
// Enter 2: headless CLI → skip model → scope
// Enter 3: scope → write → demo prompt
// Enter 4: Continue
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

	sendKeys(t, m, "enter", "enter", "enter", "enter")

	if m.Result() != ResultDemo {
		t.Fatalf("Result() = %v, want ResultDemo; view=\n%s", m.Result(), m.View())
	}
}

// Enter 1: CLI → skip model → headless CLI
// Enter 2: headless CLI → skip model → scope
// Enter 3: scope → write → demo prompt
// Down: focus "Not now"
// Enter 4: Not now
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

	sendKeys(t, m, "enter", "enter", "enter", "down", "enter")

	if m.Result() != ResultCompleted {
		t.Fatalf("Result() = %v, want ResultCompleted", m.Result())
	}
}

// Enter 1-3: get to demo prompt
// Down twice: focus "Dismiss"
// Enter 4: Dismiss
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

	sendKeys(t, m, "enter", "enter", "enter", "down", "down", "enter")

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

	// CLI → skip model → headless CLI → skip model → scope → write → done (no demo prompt)
	sendKeys(t, m, "enter", "enter", "enter")

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

// Enter 1: CLI → skip model → headless CLI
// Enter 2: headless CLI → skip model → scope
// Enter 3: scope → collision check → overwrite screen
// Down: focus "Cancel"
// Enter 4: Cancel
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

	sendKeys(t, m, "enter", "enter", "enter", "down", "enter")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want cancelled; view=\n%s", m.Result(), m.View())
	}
	if wrote {
		t.Fatal("profile writer was called after overwrite cancel")
	}
}

// Enter 1: CLI → skip model → headless CLI
// Enter 2: headless CLI → skip model → scope
// Enter 3: scope → write fails
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

	sendKeys(t, m, "enter", "enter", "enter")

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

// Enter 1: CLI → skip model (error) → headless CLI
// Enter 2: headless CLI → skip model (error) → scope
// Enter 3: scope → write → demo prompt
// Down: Not now
// Enter 4: Not now
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

	sendKeys(t, m, "enter", "enter", "enter", "down", "enter")

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

	_, err := SubprocessModels{}.ModelsFor("claude")
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

func sendKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, key := range keys {
		sendKey(t, m, key)
	}
}

func sendKey(t *testing.T, m *Model, key string) {
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
	}
	next, _ := m.Update(msg)
	if updated, ok := next.(*Model); ok {
		_ = updated
	}
}
