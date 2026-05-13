package native

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/codagent/agent-runner/internal/agentplugin"
	"github.com/codagent/agent-runner/internal/profilewrite"
	"github.com/codagent/agent-runner/internal/usersettings"
)

type fakePluginInstaller struct {
	resolveCLIs  []string
	resolveScope string
	resolveErr   error
	dryRunOutput string
	dryRunErr    error
	installOut   string
	installWarn  string
	installErr   error
	installed    bool
}

func (f *fakePluginInstaller) Resolve(clis []string, scope string) (*agentplugin.Plan, error) {
	f.resolveCLIs = clis
	f.resolveScope = scope
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &agentplugin.Plan{Binary: "/usr/local/bin/agent-plugin", CLIs: clis, Project: scope == "project"}, nil
}

func (f *fakePluginInstaller) DryRun(plan *agentplugin.Plan) (*agentplugin.Preview, error) {
	if f.dryRunErr != nil {
		return nil, f.dryRunErr
	}
	return &agentplugin.Preview{Output: f.dryRunOutput}, nil
}

func (f *fakePluginInstaller) Install(plan *agentplugin.Plan) (*agentplugin.Result, error) {
	f.installed = true
	if f.installErr != nil {
		return nil, f.installErr
	}
	return &agentplugin.Result{Output: f.installOut, Warning: f.installWarn}, nil
}

func pluginDeps(plugin *fakePluginInstaller) Deps {
	return Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings:   SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error { return nil }),
		Clock:      func() time.Time { return time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC) },
		HomeDir:    func() (string, error) { return "/home/me", nil },
		Cwd:        func() (string, error) { return "/work/project", nil },
		Plugin:     plugin,
		EnumCLIs:   func(string, string) ([]string, error) { return []string{"claude"}, nil },
	}
}

// CLI → default model → headless CLI → default model → scope → profile write →
// resolve → dry-run → preview stage → confirm (enter Install) → install → completed_at → demo prompt
func TestPluginInstallRunsBetweenProfileWriteAndCompletion(t *testing.T) {
	var saved []usersettings.Settings
	plugin := &fakePluginInstaller{dryRunOutput: "Would install skills for claude"}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := NewModel(&deps)

	// Walk to scope → write → plugin preview
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginPreview {
		t.Fatalf("stage = %v, want stagePluginPreview", m.stage)
	}

	// Confirm install
	sendKeys(t, m, "enter")

	// Should be at demo prompt now
	view := m.View()
	if !strings.Contains(view, "Continue") || !strings.Contains(view, "Not now") {
		t.Fatalf("expected demo prompt after plugin install, got:\n%s", view)
	}
	if !plugin.installed {
		t.Fatal("plugin was not installed")
	}

	wantSaved := []usersettings.Settings{{
		Setup: usersettings.SetupSettings{CompletedAt: "2026-05-13T12:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved settings mismatch (-want +got):\n%s", diff)
	}
}

func TestPluginMissingBinaryFailsSetup(t *testing.T) {
	plugin := &fakePluginInstaller{resolveErr: agentplugin.ErrBinaryMissing}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want ResultFailed", m.Result())
	}
	if !errors.Is(m.Err(), agentplugin.ErrBinaryMissing) {
		t.Fatalf("Err() = %v, want ErrBinaryMissing", m.Err())
	}
}

func TestPluginCancelAtPreviewDoesNotWriteCompletedAt(t *testing.T) {
	saved := false
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
		saved = true
		return nil
	})
	m := NewModel(&deps)

	// Walk to plugin preview
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginPreview {
		t.Fatalf("stage = %v, want stagePluginPreview", m.stage)
	}

	// Focus Cancel and press enter
	sendKeys(t, m, "down", "enter")

	if m.Result() != ResultCancelled {
		t.Fatalf("Result() = %v, want ResultCancelled", m.Result())
	}
	if saved {
		t.Fatal("settings were saved after plugin cancel")
	}
	if plugin.installed {
		t.Fatal("plugin was installed after cancel")
	}
}

func TestPluginInstallWarningStillWritesCompletedAt(t *testing.T) {
	var saved []usersettings.Settings
	plugin := &fakePluginInstaller{
		dryRunOutput: "preview",
		installWarn:  "copilot: permission denied",
		installOut:   "partial output",
	}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")
	sendKeys(t, m, "enter") // confirm install

	if m.Result() == ResultFailed {
		t.Fatalf("Result() = ResultFailed, want non-failure for per-CLI warning")
	}
	if len(saved) == 0 {
		t.Fatal("expected settings to be saved despite per-CLI warning")
	}
	lastSave := saved[len(saved)-1]
	if lastSave.Setup.CompletedAt == "" {
		t.Fatal("CompletedAt should be set despite per-CLI warning")
	}
}

func TestPluginScopeMatchesSetupScope(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	// Walk through: CLI → default model → headless → default model → scope (default is "global")
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if plugin.resolveScope != "global" {
		t.Fatalf("resolve scope = %q, want global", plugin.resolveScope)
	}
}

func TestPluginScopeProjectPassesProject(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	// Walk to scope, select "project" (second option)
	sendKeys(t, m, "enter", "enter", "enter", "enter", "down", "enter")

	if plugin.resolveScope != "project" {
		t.Fatalf("resolve scope = %q, want project", plugin.resolveScope)
	}
}

func TestPluginDryRunFailureFailsSetup(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunErr: errors.New("dry-run failed")}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want ResultFailed for dry-run failure", m.Result())
	}
}

func TestPluginPreviewShowsDryRunOutput(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "Would install agent-skills for claude, codex"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	view := m.View()
	if !strings.Contains(view, "Would install agent-skills") {
		t.Fatalf("expected dry-run preview in view:\n%s", view)
	}
	if !strings.Contains(view, "Install") || !strings.Contains(view, "Cancel") {
		t.Fatalf("expected Install/Cancel options in preview:\n%s", view)
	}
}

func TestPluginNilSkipsInstallStep(t *testing.T) {
	var saved []usersettings.Settings
	deps := Deps{
		Detector:   AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:     ModelDiscovererFunc(func(string) ([]string, error) { return nil, nil }),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
			saved = append(saved, mutator(usersettings.Settings{}))
			return nil
		}),
		Clock:   func() time.Time { return time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC) },
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
		Plugin:  nil,
	}
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	view := m.View()
	if !strings.Contains(view, "Continue") || !strings.Contains(view, "Not now") {
		t.Fatalf("expected demo prompt when Plugin is nil:\n%s", view)
	}
	if len(saved) != 1 || saved[0].Setup.CompletedAt == "" {
		t.Fatal("completed_at should be written when Plugin is nil")
	}
}

func TestPluginInstallErrorFailsSetup(t *testing.T) {
	saved := false
	plugin := &fakePluginInstaller{
		dryRunOutput: "preview",
		installErr:   errors.New("command execution failed"),
	}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
		saved = true
		return nil
	})
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")
	sendKeys(t, m, "enter") // confirm install

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want ResultFailed for install error", m.Result())
	}
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "command execution failed") {
		t.Fatalf("Err() = %v, want command execution failed", m.Err())
	}
	if saved {
		t.Fatal("settings should not be saved after install error")
	}
}

func TestPluginEnumCLIsPassedToResolve(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	deps.EnumCLIs = func(globalPath, projectPath string) ([]string, error) {
		return []string{"claude", "codex", "copilot"}, nil
	}
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	want := []string{"claude", "codex", "copilot"}
	if diff := cmp.Diff(want, plugin.resolveCLIs); diff != "" {
		t.Fatalf("resolve CLIs mismatch (-want +got):\n%s", diff)
	}
}
