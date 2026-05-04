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

func TestWelcomeSurfaceHasNoSkipDismissOrNotNow(t *testing.T) {
	m := NewModel(&Deps{})
	view := strings.ToLower(m.View())
	for _, forbidden := range []string{"skip", "dismiss", "not now", "not-now"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("welcome view contains forbidden action %q:\n%s", forbidden, view)
		}
	}
}

func TestModelCompletesSetupAndRecordsTimestampAfterWrite(t *testing.T) {
	var wrote []profilewrite.Request
	var saved []usersettings.Settings
	deps := Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude", "codex"}, nil }),
		Models: ModelDiscovererFunc(func(adapter string) ([]string, error) {
			switch adapter {
			case "claude":
				return []string{"opus"}, nil
			case "codex":
				return nil, nil
			default:
				return nil, nil
			}
		}),
		Settings: SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
			saved = append(saved, mutator(usersettings.Settings{Onboarding: usersettings.OnboardingSettings{CompletedAt: "2026-05-01T00:00:00Z"}}))
			return nil
		}),
		Profiles: ProfileWriterFunc(func(req *profilewrite.Request) error {
			wrote = append(wrote, *req)
			return nil
		}),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Clock:      func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
	}
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultCompleted {
		t.Fatalf("Result() = %v, want completed; err=%v view=\n%s", m.Result(), m.Err(), m.View())
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
		Setup:      usersettings.SetupSettings{CompletedAt: "2026-05-04T12:00:00Z"},
		Onboarding: usersettings.OnboardingSettings{CompletedAt: "2026-05-01T00:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved settings mismatch (-want +got):\n%s", diff)
	}
}

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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "right", "enter")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want cancelled", m.Result())
	}
	if saved || wrote {
		t.Fatalf("saved=%v wrote=%v, want no side effects", saved, wrote)
	}
	if view := m.View(); !strings.Contains(view, "Setup Cancelled") {
		t.Fatalf("cancelled setup view missing Setup Cancelled:\n%s", view)
	}
}

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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "right", "enter")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want cancelled", m.Result())
	}
	if wrote {
		t.Fatal("profile writer was called after overwrite cancel")
	}
}

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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want failed", m.Result())
	}
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "disk full") {
		t.Fatalf("Err() = %v, want disk full", m.Err())
	}
	if saved {
		t.Fatal("settings were saved after profile write failure")
	}
}

func TestModelDiscoveryErrorUsesAdapterDefault(t *testing.T) {
	var wrote []profilewrite.Request
	var saved bool
	var discoveryCalls int
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models: ModelDiscovererFunc(func(string) ([]string, error) {
			discoveryCalls++
			return nil, errors.New("permission denied")
		}),
		Profiles: ProfileWriterFunc(func(req *profilewrite.Request) error {
			wrote = append(wrote, *req)
			return nil
		}),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			saved = true
			return nil
		}),
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	})

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

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
	if !saved {
		t.Fatal("settings were not saved after setup completion")
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

func sendKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, key := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		}
		next, _ := m.Update(msg)
		if updated, ok := next.(*Model); ok {
			m = updated
		}
	}
}
