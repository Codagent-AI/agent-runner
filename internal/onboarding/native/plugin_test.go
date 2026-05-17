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

// CLI → default model → headless CLI → default model → intro → scope →
// profile write → resolve → dry-run → preview stage → confirm (enter Install) → install → completed_at → demo prompt
func TestPluginInstallRunsBetweenProfileWriteAndCompletion(t *testing.T) {
	var saved []usersettings.Settings
	plugin := &fakePluginInstaller{dryRunOutput: "Would install skills for claude"}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := NewModel(&deps)

	// Walk to plugin intro.
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginIntro {
		t.Fatalf("stage = %v, want stagePluginIntro", m.stage)
	}

	// Continue to scope, then write and preview.
	sendKeys(t, m, "enter")
	sendKeys(t, m, "enter")

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
		AutonomousBackend: usersettings.BackendInteractiveClaude,
		Setup:             usersettings.SetupSettings{CompletedAt: "2026-05-13T12:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved settings mismatch (-want +got):\n%s", diff)
	}
}

func TestPluginInstallCompletionStartsDemoPromptTransition(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "Would install skills for claude"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginPreview {
		t.Fatalf("stage = %v, want stagePluginPreview", m.stage)
	}

	_, installCmd := m.Update(pluginInstallMsg{result: &agentplugin.Result{}})
	if m.stage != stageDemoPrompt {
		t.Fatalf("stage = %v, want stageDemoPrompt", m.stage)
	}
	if m.animDone {
		t.Fatal("demo prompt transition should start after plugin install")
	}
	if installCmd == nil {
		t.Fatal("plugin install completion should start the demo prompt transition timer")
	}

	settleAnimation(m)
	sendKey(t, m, "enter")

	if m.Result() != ResultDemo {
		t.Fatalf("Result() = %v, want ResultDemo after continuing from demo prompt", m.Result())
	}
}

func TestPluginIntroExplainsSkillsBeforeInstallPreview(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginIntro {
		t.Fatalf("stage = %v, want stagePluginIntro", m.stage)
	}
	view := m.View()
	for _, want := range []string{
		"Agent Runner uses skills from",
		"https://github.com/Codagent-AI/agent-skills",
		"focused workflows for spec-driven development",
		"Agent Validator quality",
		"gates and PR/CI follow-up skills",
		"Next",
		"you will select where Agent Runner installs these skills.",
		"Continue",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("intro view missing %q:\n%s", want, view)
		}
	}
}

func TestPluginMissingBinaryFailsSetup(t *testing.T) {
	plugin := &fakePluginInstaller{resolveErr: agentplugin.ErrBinaryMissing}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want ResultFailed", m.Result())
	}
	if !errors.Is(m.Err(), agentplugin.ErrBinaryMissing) {
		t.Fatalf("Err() = %v, want ErrBinaryMissing", m.Err())
	}
}

func TestPluginPreviewIsMandatoryAndDownEnterStillInstalls(t *testing.T) {
	var saved []usersettings.Settings
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	m := NewModel(&deps)

	// Walk to plugin intro, then scope, then preview.
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginPreview {
		t.Fatalf("stage = %v, want stagePluginPreview", m.stage)
	}
	if diff := cmp.Diff([]string{"Install"}, m.options); diff != "" {
		t.Fatalf("plugin preview options mismatch (-want +got):\n%s", diff)
	}

	// There is no cancel option; moving down should keep focus on Install.
	sendKeys(t, m, "down", "enter")

	if !plugin.installed {
		t.Fatal("plugin was not installed")
	}
	if len(saved) == 0 || saved[len(saved)-1].Setup.CompletedAt == "" {
		t.Fatal("completed_at should be written after mandatory plugin install")
	}
}

func TestPluginInstallShowsWaitingStateWhileCommandRuns(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.stage != stagePluginPreview {
		t.Fatalf("stage = %v, want stagePluginPreview", m.stage)
	}

	cmd := sendKeyRaw(t, m, "enter")
	if cmd == nil {
		t.Fatal("confirming install should start plugin install command")
	}

	view := m.View()
	if !strings.Contains(view, "Installing agent skills") {
		t.Fatalf("expected installing wait message while command runs:\n%s", view)
	}
	if !strings.Contains(view, "This can take a moment") {
		t.Fatalf("expected wait guidance while command runs:\n%s", view)
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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")
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
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if plugin.resolveScope != "global" {
		t.Fatalf("resolve scope = %q, want global", plugin.resolveScope)
	}
}

func TestPluginScopeProjectPassesProject(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "preview"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	// Walk to scope, select "project" (second option)
	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "down", "enter")

	if plugin.resolveScope != "project" {
		t.Fatalf("resolve scope = %q, want project", plugin.resolveScope)
	}
}

func TestPluginDryRunFailureFailsSetup(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunErr: errors.New("dry-run failed")}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	if m.Result() != ResultFailed {
		t.Fatalf("Result() = %v, want ResultFailed for dry-run failure", m.Result())
	}
}

func TestPluginPreviewShowsDryRunOutput(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "Would install agent-skills for claude, codex"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	view := m.View()
	if !strings.Contains(view, "Would install agent-skills") {
		t.Fatalf("expected dry-run preview in view:\n%s", view)
	}
	if !strings.Contains(view, "Install") {
		t.Fatalf("expected Install option in preview:\n%s", view)
	}
	if strings.Contains(view, "Cancel") {
		t.Fatalf("did not expect Cancel option in mandatory plugin preview:\n%s", view)
	}
}

func TestPluginPreviewRendersAfterAsyncDryRunWithoutSettledAnimation(t *testing.T) {
	plugin := &fakePluginInstaller{dryRunOutput: "Would install agent-skills for claude, codex"}
	deps := pluginDeps(plugin)
	m := NewModel(&deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter")
	cmd := sendKeyRaw(t, m, "enter")
	runTestCmd(t, m, cmd)

	view := m.View()
	if strings.Contains(view, "Config Scope") {
		t.Fatalf("expected plugin preview, still rendering scope screen:\n%s", view)
	}
	if !strings.Contains(view, "Would install agent-skills") {
		t.Fatalf("expected dry-run preview in view:\n%s", view)
	}
	if !strings.Contains(view, "Install") {
		t.Fatalf("expected Install option in preview:\n%s", view)
	}
	if strings.Contains(view, "Cancel") {
		t.Fatalf("did not expect Cancel option in mandatory plugin preview:\n%s", view)
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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter")

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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")
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

	sendKeys(t, m, "enter", "enter", "enter", "enter", "enter", "enter", "enter")

	want := []string{"claude", "codex", "copilot"}
	if diff := cmp.Diff(want, plugin.resolveCLIs); diff != "" {
		t.Fatalf("resolve CLIs mismatch (-want +got):\n%s", diff)
	}
}
