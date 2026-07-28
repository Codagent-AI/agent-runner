package native

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/codagent/agent-runner/internal/profilerecommend"
	"github.com/codagent/agent-runner/internal/profilewrite"
	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestRecommendationDiscoveryHasDedicatedNonActionableLoadingState(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil }),
		Models:   ModelDiscovererFunc(func(string) ([]string, error) { return []string{"opus"}, nil }),
	})

	panel := tuistyle.Sanitize(m.renderPanel())
	for _, want := range []string{"Set Up Agent Runner", "Welcome", "four-role", "profile recommendation", "available CLIs", "models"} {
		if !strings.Contains(panel, want) {
			t.Errorf("loading panel missing %q:\n%s", want, panel)
		}
	}
	if len(m.options) != 0 {
		t.Fatalf("loading options = %v, want none", m.options)
	}
	if strings.Contains(panel, "Step ") {
		t.Fatalf("loading panel unexpectedly shows progress:\n%s", panel)
	}

	sendKey(t, m, "enter")
	if m.stage != stageLoading {
		t.Fatalf("Enter advanced loading state to %v", m.stage)
	}
}

func TestDiscoveryRunsConcurrentlyAndPublishesOneOrderedAggregate(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	var mu sync.Mutex
	completed := make([]string, 0, 3)
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) {
			return []string{"cursor", "claude", "codex"}, nil
		}),
		Models: ModelDiscovererFunc(func(adapter string) ([]string, error) {
			started <- adapter
			<-release
			mu.Lock()
			completed = append(completed, adapter)
			mu.Unlock()
			return []string{adapter + "-model-1", adapter + "-model-2"}, nil
		}),
	})

	msgCh := make(chan tea.Msg, 1)
	go func() { msgCh <- m.discoverRecommendation()() }()
	gotStarted := map[string]bool{}
	for range 3 {
		select {
		case adapter := <-started:
			gotStarted[adapter] = true
		case <-time.After(time.Second):
			t.Fatal("model queries did not all start concurrently")
		}
	}
	if len(gotStarted) != 3 {
		t.Fatalf("started adapters = %v", gotStarted)
	}
	if m.stage != stageLoading || len(m.options) != 0 {
		t.Fatalf("partial discovery became actionable: stage=%v options=%v", m.stage, m.options)
	}
	close(release)
	msg := <-msgCh
	if _, ok := msg.(discoveryLoadedMsg); !ok {
		t.Fatalf("Init command returned %T, want discoveryLoadedMsg", msg)
	}
	_, _ = m.Update(msg)

	got := m.snapshot.Discoveries()
	want := []profilerecommend.CLIDiscovery{
		{CLI: "cursor", Models: []string{"cursor-model-1", "cursor-model-2"}},
		{CLI: "claude", Models: []string{"claude-model-1", "claude-model-2"}},
		{CLI: "codex", Models: []string{"codex-model-1", "codex-model-2"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("aggregate ordering mismatch (-want +got):\n%s\ncompletion order=%v", diff, completed)
	}
}

func TestRecommendationSummaryIsFirstActionableSurface(t *testing.T) {
	m := startTestModel(t, baseDeps())
	panel := tuistyle.Sanitize(m.renderPanel())
	for _, want := range []string{
		"Recommended Agent Profile",
		"Lead", "claude", "opus",
		"Crosscheck", "codex", "gpt-5.6-sol",
		"Implementor", "codex", "gpt-5.6-terra",
		"Tester", "claude", "sonnet",
		"planning artifacts", "Agent Validator", "Step 1 of",
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("summary missing %q:\n%s", want, panel)
		}
	}
	wantOptions := []string{"Accept all recommendations", "Customize roles"}
	if diff := cmp.Diff(wantOptions, m.options); diff != "" {
		t.Fatalf("summary actions mismatch (-want +got):\n%s", diff)
	}
}

func TestAcceptAllProceedsDirectlyToHeadlessBackend(t *testing.T) {
	m := startTestModel(t, baseDeps())
	sendKey(t, m, "enter")

	if m.stage != stageAutonomousBackend {
		t.Fatalf("stage = %v, want Autonomous Backend", m.stage)
	}
	if got := m.options[m.focus]; got != string(usersettings.BackendHeadless) {
		t.Fatalf("focused backend = %q, want headless", got)
	}
	panel := tuistyle.Sanitize(m.renderPanel())
	for _, want := range []string{"Headless", "recommended", "Interactive", "Interactive for Claude"} {
		if !strings.Contains(panel, want) {
			t.Errorf("backend panel missing %q:\n%s", want, panel)
		}
	}
	for _, forbidden := range []string{"billing", "API rate", "credits"} {
		if strings.Contains(panel, forbidden) {
			t.Errorf("backend panel contains billing claim %q:\n%s", forbidden, panel)
		}
	}
}

func TestCustomizationVisitsFourRolesAndReusesImmutableDiscovery(t *testing.T) {
	var calls int
	deps := baseDeps()
	original := deps.Models
	deps.Models = ModelDiscovererFunc(func(adapter string) ([]string, error) {
		calls++
		return original.ModelsFor(adapter)
	})
	m := startTestModel(t, deps)
	discoveryCalls := calls

	sendKeys(t, m, "down", "enter")
	for i, role := range profilerecommend.Roles() {
		if m.stage != stageRoleCLI || m.currentRole() != role {
			t.Fatalf("role %d stage=%v role=%v, want CLI for %v", i, m.stage, m.currentRole(), role)
		}
		sendKey(t, m, "enter")
		if m.stage != stageRoleModel {
			t.Fatalf("%v model stage = %v, want stageRoleModel", role, m.stage)
		}
		sendKey(t, m, "enter")
	}
	if m.stage != stageAutonomousBackend {
		t.Fatalf("stage after Tester = %v, want backend", m.stage)
	}
	if calls != discoveryCalls {
		t.Fatalf("customization launched discovery again: before=%d after=%d", discoveryCalls, calls)
	}
}

func TestLeadAndImplementorChangesRecomputePairedRecommendations(t *testing.T) {
	deps := baseDeps()
	deps.Detector = AdapterDetectorFunc(func() ([]string, error) { return []string{"opencode"}, nil })
	deps.Models = ModelDiscovererFunc(func(string) ([]string, error) {
		return []string{
			"anthropic/claude-opus", "openai/gpt-5.7-sol",
			"openai/gpt-5.7-terra", "anthropic/claude-sonnet",
		}, nil
	})
	m := startTestModel(t, deps)

	sendKeys(t, m, "down", "enter", "enter") // customize, Lead CLI
	focusOption(t, m, "openai/gpt-5.7-sol")
	sendKey(t, m, "enter")
	if got := m.selections[roleIndex(profilerecommend.Crosscheck)].Model; got != "anthropic/claude-opus" {
		t.Fatalf("Crosscheck recommendation = %q, want Claude after GPT Lead", got)
	}
	if m.currentRole() != profilerecommend.Crosscheck {
		t.Fatalf("current role = %v, want Crosscheck", m.currentRole())
	}

	sendKeys(t, m, "enter", "enter") // Crosscheck CLI + recommended model
	sendKey(t, m, "enter")           // Implementor CLI
	focusOption(t, m, "anthropic/claude-sonnet")
	sendKey(t, m, "enter")
	if got := m.selections[roleIndex(profilerecommend.Tester)].Model; got != "openai/gpt-5.7-terra" {
		t.Fatalf("Tester recommendation = %q, want GPT after Claude Implementor", got)
	}
}

func TestSameFamilyEvaluatorOverrideIsAllowed(t *testing.T) {
	deps := baseDeps()
	deps.Detector = AdapterDetectorFunc(func() ([]string, error) { return []string{"opencode"}, nil })
	deps.Models = ModelDiscovererFunc(func(string) ([]string, error) {
		return []string{"anthropic/claude-opus", "openai/gpt-5.7-sol"}, nil
	})
	m := startTestModel(t, deps)
	sendKeys(t, m, "down", "enter", "enter", "enter") // customize + Lead CLI/model (Claude)
	sendKey(t, m, "enter")                            // Crosscheck CLI
	focusOption(t, m, "anthropic/claude-opus")
	sendKey(t, m, "enter")

	got := m.selections[roleIndex(profilerecommend.Crosscheck)]
	if got.Model != "anthropic/claude-opus" || got.Family != profilerecommend.Claude {
		t.Fatalf("same-family override was not retained: %#v", got)
	}
}

func TestCLIDefaultAndDiscoveryFailureSurfaces(t *testing.T) {
	tests := []struct {
		name      string
		models    []string
		err       error
		wantStage stage
		wantText  string
	}{
		{
			name:      "unclassified models retain explicit default option",
			models:    []string{"mystery-zeta", "mystery-alpha"},
			wantStage: stageRoleModel,
			wantText:  "Use CLI default",
		},
		{
			name:      "empty discovery uses default surface",
			wantStage: stageRoleModelDefault,
			wantText:  "leave the model field unset",
		},
		{
			name:      "failed discovery remains usable",
			err:       errors.New("query timed out"),
			wantStage: stageRoleModelDefault,
			wantText:  "query timed out",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := baseDeps()
			deps.Detector = AdapterDetectorFunc(func() ([]string, error) { return []string{"opencode"}, nil })
			deps.Models = ModelDiscovererFunc(func(string) ([]string, error) { return tt.models, tt.err })
			m := startTestModel(t, deps)
			sendKeys(t, m, "down", "enter", "enter")

			if m.stage != tt.wantStage {
				t.Fatalf("stage = %v, want %v", m.stage, tt.wantStage)
			}
			panel := tuistyle.Sanitize(m.renderPanel())
			if !strings.Contains(panel, tt.wantText) {
				t.Fatalf("panel missing %q:\n%s", tt.wantText, panel)
			}
			if tt.wantStage == stageRoleModel {
				if got := m.options[m.focus]; got != useCLIDefaultOption {
					t.Fatalf("focused option = %q, want CLI default", got)
				}
				if diff := cmp.Diff([]string{useCLIDefaultOption, "mystery-zeta", "mystery-alpha"}, m.options); diff != "" {
					t.Fatalf("model options mismatch (-want +got):\n%s", diff)
				}
			}
			sendKey(t, m, "enter")
			if got := m.selections[0].Model; got != "" {
				t.Fatalf("default selection model = %q, want empty", got)
			}
		})
	}
}

func TestRecommendationExplainsDiscoveryAndDiversityLimitations(t *testing.T) {
	deps := baseDeps()
	deps.Detector = AdapterDetectorFunc(func() ([]string, error) { return []string{"claude"}, nil })
	deps.Models = ModelDiscovererFunc(func(string) ([]string, error) {
		return nil, errors.New("model query timed out")
	})
	m := startTestModel(t, deps)
	panel := tuistyle.Sanitize(m.renderPanel())
	for _, want := range []string{
		"model query timed out", "CLI default",
		"Lead/Crosscheck", "Implementor/Tester", "could not establish known model-family", "diversity",
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("summary missing limitation %q:\n%s", want, panel)
		}
	}
}

func TestNoDetectedAdaptersFailsAfterLoading(t *testing.T) {
	m := NewModel(&Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) { return nil, nil }),
	})
	runTestCmd(t, m, m.Init())
	if m.Result() != ResultFailed || m.Err() == nil ||
		!strings.Contains(m.Err().Error(), "no supported CLI adapters were found on $PATH") {
		t.Fatalf("result=%v err=%v", m.Result(), m.Err())
	}
}

func TestSuccessfulAcceptAllWritesOneFourRoleRequestAndPersistsSettings(t *testing.T) {
	var writes []profilewrite.Request
	var saved []usersettings.Settings
	deps := baseDeps()
	deps.Profiles = ProfileWriterFunc(func(req *profilewrite.Request) error {
		writes = append(writes, *req)
		return nil
	})
	deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
		saved = append(saved, mutator(usersettings.Settings{}))
		return nil
	})
	deps.Clock = func() time.Time { return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC) }
	m := startTestModel(t, deps)

	sendKeys(t, m, "enter", "enter", "enter", "enter")

	wantWrite := profilewrite.Request{
		TargetPath: "/home/me/.agent-runner/config.yaml",
		LeadCLI:    "claude", LeadModel: "opus",
		CrosscheckCLI: "codex", CrosscheckModel: "gpt-5.6-sol",
		ImplementorCLI: "codex", ImplementorModel: "gpt-5.6-terra",
		TesterCLI: "claude", TesterModel: "sonnet",
	}
	if diff := cmp.Diff([]profilewrite.Request{wantWrite}, writes); diff != "" {
		t.Fatalf("profile writes mismatch (-want +got):\n%s", diff)
	}
	wantSaved := []usersettings.Settings{{
		AutonomousBackend:        usersettings.BackendHeadless,
		AutonomousPermissionMode: usersettings.PermissionModeConservative,
		Setup:                    usersettings.SetupSettings{CompletedAt: "2026-07-27T12:00:00Z"},
	}}
	if diff := cmp.Diff(wantSaved, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("settings mismatch (-want +got):\n%s", diff)
	}
	if m.stage != stageDemoPrompt {
		t.Fatalf("stage = %v, want demo prompt", m.stage)
	}
}

func TestCancellationAndOverwriteBoundariesHaveNoPartialPersistence(t *testing.T) {
	t.Run("cancel after backend selection", func(t *testing.T) {
		wrote, saved := false, false
		deps := baseDeps()
		deps.Profiles = ProfileWriterFunc(func(*profilewrite.Request) error { wrote = true; return nil })
		deps.Settings = SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			saved = true
			return nil
		})
		m := startTestModel(t, deps)
		sendKeys(t, m, "enter", "enter", "esc")
		if m.Result() != ResultCancelled || wrote || saved {
			t.Fatalf("result=%v wrote=%v saved=%v", m.Result(), wrote, saved)
		}
	})
	t.Run("legacy collisions are named and cancel prevents write", func(t *testing.T) {
		wrote := false
		deps := baseDeps()
		deps.Collisions = CollisionDetectorFunc(func(string) ([]string, error) {
			return []string{"planner", "reviewer", "tester"}, nil
		})
		deps.Profiles = ProfileWriterFunc(func(*profilewrite.Request) error { wrote = true; return nil })
		m := startTestModel(t, deps)
		sendKeys(t, m, "enter", "enter", "enter", "enter")
		if m.stage != stageOverwrite {
			t.Fatalf("stage = %v, want overwrite", m.stage)
		}
		panel := tuistyle.Sanitize(m.renderPanel())
		for _, collision := range []string{"planner", "reviewer", "tester"} {
			if !strings.Contains(panel, collision) {
				t.Errorf("overwrite panel missing %s:\n%s", collision, panel)
			}
		}
		sendKeys(t, m, "down", "enter")
		if m.Result() != ResultCancelled || wrote {
			t.Fatalf("result=%v wrote=%v", m.Result(), wrote)
		}
	})
}

func TestSemanticProgressTracksBranchRoleSubstatesAndOverwrite(t *testing.T) {
	m := startTestModel(t, baseDeps())
	_, acceptTotal, ok := m.setupProgress()
	if !ok {
		t.Fatal("summary has no progress")
	}
	sendKeys(t, m, "down", "enter")
	leadStep, customTotal, _ := m.setupProgress()
	if customTotal != acceptTotal+4 {
		t.Fatalf("custom total = %d, accept total = %d, want +4", customTotal, acceptTotal)
	}
	sendKey(t, m, "enter")
	modelStep, modelTotal, _ := m.setupProgress()
	if leadStep != modelStep || customTotal != modelTotal {
		t.Fatalf("Lead substates changed progress: CLI=%d/%d model=%d/%d", leadStep, customTotal, modelStep, modelTotal)
	}

	demo := NewDemoPromptModel(&Deps{})
	if _, _, ok := demo.setupProgress(); ok {
		t.Fatal("demo-only mode unexpectedly has progress")
	}

	deps := baseDeps()
	deps.Collisions = CollisionDetectorFunc(func(string) ([]string, error) { return []string{"lead"}, nil })
	overwrite := startTestModel(t, deps)
	sendKeys(t, overwrite, "enter", "enter", "enter", "enter")
	current, total, ok := overwrite.setupProgress()
	if !ok || total != acceptTotal+1 || current <= 1 {
		t.Fatalf("overwrite progress = %d/%d ok=%v, want one conditional step", current, total, ok)
	}
}

func TestRoleAndScopeScreensExplainTheirPurpose(t *testing.T) {
	m := startTestModel(t, baseDeps())
	sendKeys(t, m, "down", "enter")
	panel := tuistyle.Sanitize(m.renderPanel())
	for _, want := range []string{"Lead", "plans", "CLI", "controls"} {
		if !strings.Contains(panel, want) {
			t.Errorf("Lead customization missing %q:\n%s", want, panel)
		}
	}

	accept := startTestModel(t, baseDeps())
	sendKeys(t, accept, "enter", "enter", "enter")
	scope := tuistyle.Sanitize(accept.renderPanel())
	for _, want := range []string{"Global", "Project", "~/.agent-runner/config.yaml", ".agent-runner/config.yaml"} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope screen missing %q:\n%s", want, scope)
		}
	}
}

func TestNativeSetupUsesSharedWriterInsteadOfShellSideYAML(t *testing.T) {
	body, err := os.ReadFile("native.go")
	if err != nil {
		t.Fatalf("read native.go: %v", err)
	}
	source := string(body)
	if !strings.Contains(source, "pendingProfile = &profilewrite.Request{") ||
		!strings.Contains(source, "Profiles.WriteProfile(m.pendingProfile)") {
		t.Fatal("native setup does not call shared profile writer")
	}
	if strings.Contains(source, `"gopkg.in/yaml.v3"`) || strings.Contains(source, "yaml.Marshal") {
		t.Fatal("native setup constructs YAML instead of using shared writer")
	}
}

func TestDemoOnlyModeExplainsDemoWithoutProgress(t *testing.T) {
	m := NewDemoPromptModel(&Deps{})
	panel := tuistyle.Sanitize(m.renderPanel())
	for _, want := range []string{"short interactive demo", "about two minutes", "real workflow steps"} {
		if !strings.Contains(panel, want) {
			t.Errorf("demo prompt missing %q:\n%s", want, panel)
		}
	}
	if strings.Contains(panel, "Step ") {
		t.Fatalf("demo-only panel unexpectedly shows progress:\n%s", panel)
	}
}

func TestOptionWindow(t *testing.T) {
	if diff := cmp.Diff([]int{5, 9}, func() []int {
		start, end := optionWindow(7, 20, 4)
		return []int{start, end}
	}()); diff != "" {
		t.Fatalf("optionWindow mismatch (-want +got):\n%s", diff)
	}
}

func TestWrapTextLineAvoidsShortOrphan(t *testing.T) {
	got := wrapTextLine("Choose a model for this role now", 24)
	if len(got) < 2 || strings.TrimSpace(got[len(got)-1]) == "now" {
		t.Fatalf("wrapTextLine() produced orphan: %v", got)
	}
}

func baseDeps() *Deps {
	return &Deps{
		Detector: AdapterDetectorFunc(func() ([]string, error) {
			return []string{"claude", "codex"}, nil
		}),
		Models: ModelDiscovererFunc(func(adapter string) ([]string, error) {
			switch adapter {
			case "claude":
				return []string{"sonnet", "opus"}, nil
			case "codex":
				return []string{"gpt-5.6-terra", "gpt-5.6-sol"}, nil
			default:
				return nil, nil
			}
		}),
		Profiles:   ProfileWriterFunc(func(*profilewrite.Request) error { return nil }),
		Collisions: CollisionDetectorFunc(func(string) ([]string, error) { return nil, nil }),
		Settings: SettingsStoreFunc(func(func(usersettings.Settings) usersettings.Settings) error {
			return nil
		}),
		HomeDir: func() (string, error) { return "/home/me", nil },
		Cwd:     func() (string, error) { return "/work/project", nil },
	}
}

func startTestModel(t *testing.T, deps *Deps) *Model {
	t.Helper()
	m := NewModel(deps)
	runTestCmd(t, m, m.Init())
	settleAnimation(m)
	if m.stage != stageRecommendation && !m.terminal {
		t.Fatalf("stage after discovery = %v, want recommendation; view:\n%s", m.stage, m.View())
	}
	return m
}

func focusOption(t *testing.T, m *Model, want string) {
	t.Helper()
	for i, option := range m.options {
		if option == want {
			m.focus = i
			return
		}
	}
	t.Fatalf("option %q not found in %v", want, m.options)
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
	_, cmd := m.Update(msg)
	return cmd
}

func runTestCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil, animTick, loadingTick:
		return
	case tea.BatchMsg:
		for _, batched := range msg {
			runTestCmd(t, m, batched)
		}
	default:
		_, nextCmd := m.Update(msg)
		runTestCmd(t, m, nextCmd)
	}
}

func settleAnimation(m *Model) {
	m.animFrame = 0
	m.animDone = true
	m.prevView = ""
}
