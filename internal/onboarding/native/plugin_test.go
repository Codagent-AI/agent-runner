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

func (f *fakePluginInstaller) DryRun(*agentplugin.Plan) (*agentplugin.Preview, error) {
	if f.dryRunErr != nil {
		return nil, f.dryRunErr
	}
	return &agentplugin.Preview{Output: f.dryRunOutput}, nil
}

func (f *fakePluginInstaller) Install(*agentplugin.Plan) (*agentplugin.Result, error) {
	f.installed = true
	if f.installErr != nil {
		return nil, f.installErr
	}
	return &agentplugin.Result{Output: f.installOut, Warning: f.installWarn}, nil
}

func pluginDeps(plugin *fakePluginInstaller) *Deps {
	deps := baseDeps()
	deps.Plugin = plugin
	deps.Clock = func() time.Time { return time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC) }
	deps.EnumCLIs = func(string, string) ([]string, error) { return []string{"claude"}, nil }
	return deps
}

func TestPluginInstallCompletesBeforeProfileAndSettingsPersistence(t *testing.T) {
	var writes int
	var saved []usersettings.Settings
	plugin := &fakePluginInstaller{dryRunOutput: "Would install skills for claude"}
	deps := pluginDeps(plugin)
	deps.Profiles = ProfileWriterFunc(func(*profilewrite.Request) error { writes++; return nil })
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := startTestModel(t, deps)

	reachPluginPreview(t, m, false)
	if writes != 0 || len(saved) != 0 {
		t.Fatalf("before install writes=%d settings saves=%d, want 0 and 0", writes, len(saved))
	}
	sendKey(t, m, "enter")

	if !plugin.installed || m.stage != stageDemoPrompt {
		t.Fatalf("installed=%v stage=%v, want installed demo prompt", plugin.installed, m.stage)
	}
	if writes != 1 {
		t.Fatalf("profile writes after install = %d, want 1", writes)
	}
	wantSaved := []usersettings.Settings{{
		AutonomousBackend:        usersettings.BackendHeadless,
		AutonomousPermissionMode: usersettings.PermissionModeConservative,
		Setup:                    usersettings.SetupSettings{CompletedAt: "2026-05-13T12:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved settings mismatch (-want +got):\n%s", diff)
	}
}

func TestPluginPreviewCancellationDoesNotPersistProfileOrSettings(t *testing.T) {
	writes, saves := 0, 0
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	deps.Profiles = ProfileWriterFunc(func(*profilewrite.Request) error { writes++; return nil })
	deps.Settings = SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
		saves++
		return nil
	})
	m := startTestModel(t, deps)
	reachPluginPreview(t, m, false)

	sendKey(t, m, "esc")

	if m.Result() != ResultCancelled || writes != 0 || saves != 0 {
		t.Fatalf("result=%v profile writes=%d settings saves=%d", m.Result(), writes, saves)
	}
}

func TestPluginIntroExplainsSkillsBeforeScopeAndPreview(t *testing.T) {
	m := startTestModel(t, pluginDeps(&fakePluginInstaller{}))
	sendKeys(t, m, "enter", "enter", "enter")
	if m.stage != stagePluginIntro {
		t.Fatalf("stage = %v, want plugin intro", m.stage)
	}
	view := m.View()
	for _, want := range []string{
		"Agent Runner uses skills from",
		"https://github.com/Codagent-AI/agent-skills",
		"spec", "TDD", "validation", "PR", "CI",
		"profile scope", "Continue",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("plugin intro missing %q:\n%s", want, view)
		}
	}
}

func TestPluginPreviewIsMandatoryAndShowsDryRunOutput(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "Would install agent-skills for claude, codex"}
	m := startTestModel(t, pluginDeps(plugin))
	reachPluginPreview(t, m, false)

	if diff := cmp.Diff([]string{"Install"}, m.options); diff != "" {
		t.Fatalf("preview options mismatch (-want +got):\n%s", diff)
	}
	view := m.View()
	if !strings.Contains(view, plugin.dryRunOutput) || strings.Contains(view, "Cancel") {
		t.Fatalf("unexpected preview:\n%s", view)
	}
	sendKeys(t, m, "down", "enter")
	if !plugin.installed {
		t.Fatal("mandatory Install action did not install")
	}
}

func TestPluginInstallShowsWaitingStateWhileCommandRuns(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	m := startTestModel(t, pluginDeps(plugin))
	reachPluginPreview(t, m, false)

	cmd := sendKeyRaw(t, m, "enter")
	if cmd == nil {
		t.Fatal("Install did not start a command")
	}
	view := m.View()
	for _, want := range []string{"Installing agent skills", "This can take a moment"} {
		if !strings.Contains(view, want) {
			t.Errorf("install wait state missing %q:\n%s", want, view)
		}
	}
}

func TestPluginInstallCompletionContinuesDemoTransition(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	m := startTestModel(t, pluginDeps(plugin))
	reachPluginPreview(t, m, false)

	_, cmd := m.Update(pluginInstallMsg{result: &agentplugin.Result{}})
	if m.stage != stageDemoPrompt || m.animDone {
		t.Fatalf("stage=%v animDone=%v, want animated demo transition", m.stage, m.animDone)
	}
	if cmd == nil {
		t.Fatal("plugin completion did not schedule the transition timer")
	}
}

func TestPluginErrorsPreserveCompletionBoundary(t *testing.T) {
	tests := []struct {
		name      string
		plugin    *fakePluginInstaller
		reachOnly bool
	}{
		{"missing binary", &fakePluginInstaller{resolveErr: agentplugin.ErrBinaryMissing}, true},
		{"dry run", &fakePluginInstaller{dryRunErr: errors.New("dry-run failed")}, true},
		{"install", &fakePluginInstaller{dryRunOutput: "preview", installErr: errors.New("install failed")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := false
			wrote := false
			deps := pluginDeps(tt.plugin)
			deps.Profiles = ProfileWriterFunc(func(*profilewrite.Request) error {
				wrote = true
				return nil
			})
			deps.Settings = SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
				saved = true
				return nil
			})
			m := startTestModel(t, deps)
			reachPluginPreview(t, m, false)
			if !tt.reachOnly && m.Result() != ResultFailed {
				sendKey(t, m, "enter")
			}
			if m.Result() != ResultFailed || wrote || saved {
				t.Fatalf("result=%v wrote=%v saved=%v err=%v", m.Result(), wrote, saved, m.Err())
			}
		})
	}
}

func TestPluginWarningStillCompletesSetup(t *testing.T) {
	var saved []usersettings.Settings
	plugin := &fakePluginInstaller{
		dryRunOutput: "preview",
		installWarn:  "copilot: permission denied",
	}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := startTestModel(t, deps)
	reachPluginPreview(t, m, false)
	sendKey(t, m, "enter")
	if m.Result() == ResultFailed || len(saved) != 1 || saved[0].Setup.CompletedAt == "" {
		t.Fatalf("result=%v saved=%#v", m.Result(), saved)
	}
}

func TestPluginScopeAndConfiguredCLIsReachResolver(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	deps.EnumCLIs = func(string, string) ([]string, error) {
		return []string{"claude", "codex", "copilot"}, nil
	}
	m := startTestModel(t, deps)
	reachPluginPreview(t, m, true)

	if plugin.resolveScope != "project" {
		t.Fatalf("resolve scope = %q, want project", plugin.resolveScope)
	}
	if diff := cmp.Diff([]string{"claude", "codex", "copilot"}, plugin.resolveCLIs); diff != "" {
		t.Fatalf("resolve CLIs mismatch (-want +got):\n%s", diff)
	}
}

func TestDeferredProfileSelectionsReachPluginResolver(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	deps.EnumCLIs = func(string, string) ([]string, error) { return nil, nil }
	m := startTestModel(t, deps)
	reachPluginPreview(t, m, false)

	if diff := cmp.Diff([]string{"claude", "codex"}, plugin.resolveCLIs); diff != "" {
		t.Fatalf("resolve CLIs mismatch (-want +got):\n%s", diff)
	}
}

func TestNilPluginSkipsInstallationAndCompletes(t *testing.T) {
	var saved []usersettings.Settings
	deps := baseDeps()
	deps.Plugin = nil
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := startTestModel(t, deps)
	sendKeys(t, m, "enter", "enter", "enter", "enter")

	if m.stage != stageDemoPrompt || len(saved) != 1 || saved[0].Setup.CompletedAt == "" {
		t.Fatalf("stage=%v saved=%#v", m.stage, saved)
	}
}

func reachPluginPreview(t *testing.T, m *Model, project bool) {
	t.Helper()
	sendKeys(t, m, "enter", "enter", "enter") // recommendation, backend, permission
	if m.stage != stagePluginIntro {
		t.Fatalf("stage = %v, want plugin intro", m.stage)
	}
	sendKey(t, m, "enter")
	if project {
		sendKey(t, m, "down")
	}
	sendKey(t, m, "enter")
	if m.Result() != ResultFailed && m.stage != stagePluginPreview {
		t.Fatalf("stage = %v, want plugin preview", m.stage)
	}
}
