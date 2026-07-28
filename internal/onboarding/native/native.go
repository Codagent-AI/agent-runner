// Package native implements the mandatory first-run setup flow.
package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/agentplugin"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/profilerecommend"
	"github.com/codagent/agent-runner/internal/profilewrite"
	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
)

type Result int

const (
	ResultCompleted Result = iota
	ResultCancelled
	ResultFailed
	ResultDemo
	ResultExitRequested
)

type AdapterDetector interface {
	DetectAdapters() ([]string, error)
}

type AdapterDetectorFunc func() ([]string, error)

func (f AdapterDetectorFunc) DetectAdapters() ([]string, error) { return f() }

type ModelDiscoverer interface {
	ModelsFor(adapter string) ([]string, error)
}

type ModelDiscovererFunc func(string) ([]string, error)

func (f ModelDiscovererFunc) ModelsFor(adapter string) ([]string, error) { return f(adapter) }

type ProfileWriter interface {
	StageProfile(*profilewrite.Request) (profilewrite.Staged, error)
}

type sharedProfileWriter struct{}

func (sharedProfileWriter) StageProfile(req *profilewrite.Request) (profilewrite.Staged, error) {
	return profilewrite.Stage(req)
}

type CollisionDetector interface {
	Collisions(path string) ([]string, error)
}

type CollisionDetectorFunc func(string) ([]string, error)

func (f CollisionDetectorFunc) Collisions(path string) ([]string, error) { return f(path) }

type SettingsStore interface {
	Update(func(usersettings.Settings) usersettings.Settings) error
}

type SettingsStoreFunc func(func(usersettings.Settings) usersettings.Settings) error

func (f SettingsStoreFunc) Update(mutator func(usersettings.Settings) usersettings.Settings) error {
	return f(mutator)
}

type PluginInstaller interface {
	Resolve(clis []string, scope string) (*agentplugin.Plan, error)
	DryRun(plan *agentplugin.Plan) (*agentplugin.Preview, error)
	Install(plan *agentplugin.Plan) (*agentplugin.Result, error)
}

type Deps struct {
	Detector            AdapterDetector
	Models              ModelDiscoverer
	Profiles            ProfileWriter
	Collisions          CollisionDetector
	Settings            SettingsStore
	Plugin              PluginInstaller
	EnumCLIs            func(globalPath, projectPath string) ([]string, error)
	Clock               func() time.Time
	HomeDir             func() (string, error)
	Cwd                 func() (string, error)
	OnboardingCompleted bool
}

func Run(deps *Deps) (Result, error) {
	m := NewModel(deps)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return ResultFailed, err
	}
	fm, ok := final.(*Model)
	if !ok {
		return ResultFailed, fmt.Errorf("unexpected setup model %T", final)
	}
	return fm.Result(), fm.Err()
}

func RunDemoPrompt(deps *Deps) (Result, error) {
	m := NewDemoPromptModel(deps)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return ResultFailed, err
	}
	fm, ok := final.(*Model)
	if !ok {
		return ResultFailed, fmt.Errorf("unexpected setup model %T", final)
	}
	return fm.Result(), fm.Err()
}

type stage int

const (
	stageLoading stage = iota
	stageRecommendation
	stageRoleCLI
	stageRoleModelDefault
	stageRoleModel
	stageAutonomousBackend
	stageAutonomousPermissionMode
	stagePluginIntro
	stageScope
	stageOverwrite
	stagePluginPreview
	stageDemoPrompt
	stageDone
)

const useCLIDefaultOption = "Use CLI default"

var (
	recommendationOptions = []string{"Accept all recommendations", "Customize roles"}
	scopeOptions          = []string{"global", "project"}
	overwriteOptions      = []string{"Overwrite", "Cancel"}
	pluginConfirmOptions  = []string{"Install"}
	demoPromptOptions     = []string{"Continue", "Not now", "Dismiss"}
	continueOptions       = []string{"Continue"}
)

func autonomousBackendOptions() []string {
	return []string{
		string(usersettings.BackendHeadless),
		string(usersettings.BackendInteractive),
		string(usersettings.BackendInteractiveClaude),
	}
}

func autonomousPermissionModeOptions() []string {
	return []string{
		string(usersettings.PermissionModeConservative),
		string(usersettings.PermissionModeYOLO),
	}
}

const (
	minCenterWidth  = 80
	minCenterHeight = 24
	maxPanelWidth   = 76
	minPanelWidth   = 44
	panelFrameWidth = 6
	textWrapInset   = 4
	animFrames      = 6
	animFrameTime   = time.Second / 60
)

var (
	setupBodyStyle             = lipgloss.NewStyle()
	setupTitleStyle            = tuistyle.LabelStyle.Bold(true)
	setupOptionStyle           = lipgloss.NewStyle()
	setupFocusedOptionStyle    = tuistyle.LabelStyle.Bold(true)
	setupTransitionStyle       = lipgloss.NewStyle().Faint(true)
	setupTransitionStatusStyle = tuistyle.DimStyle
)

type animTick struct{}
type loadingTick struct{}

type discoveryLoadedMsg struct {
	discoveries []profilerecommend.CLIDiscovery
	err         error
}

type pluginDryRunMsg struct {
	preview *agentplugin.Preview
	err     error
}

type pluginInstallMsg struct {
	result *agentplugin.Result
	err    error
}

type Model struct {
	deps Deps

	stage          stage
	focus          int
	options        []string
	snapshot       profilerecommend.Snapshot
	recommendation profilerecommend.Recommendation
	selections     [4]profilerecommend.Selection
	pairStatuses   [2]profilerecommend.PairStatus
	roleCursor     int
	customizing    bool

	autonomousBackend        usersettings.AutonomousBackend
	autonomousPermissionMode usersettings.AutonomousPermissionMode
	scope                    string
	targetPath               string
	collisions               []string
	pendingProfile           profilewrite.Staged

	width    int
	height   int
	result   Result
	err      error
	terminal bool
	demoOnly bool

	pluginPlan       *agentplugin.Plan
	pluginPreview    *agentplugin.Preview
	pluginResult     *agentplugin.Result
	pluginInstalling bool

	discoveryLoading bool
	loadingPhase     float64
	animDone         bool
	animFrame        int
	prevView         string
}

func NewModel(deps *Deps) *Model {
	deps = fillDefaults(deps)
	return &Model{
		deps:             *deps,
		stage:            stageLoading,
		width:            80,
		height:           24,
		discoveryLoading: true,
		animDone:         true,
	}
}

func NewDemoPromptModel(deps *Deps) *Model {
	deps = fillDefaults(deps)
	m := &Model{
		deps:     *deps,
		width:    80,
		height:   24,
		demoOnly: true,
		animDone: true,
	}
	m.setStage(stageDemoPrompt, demoPromptOptions)
	return m
}

func fillDefaults(deps *Deps) *Deps {
	if deps == nil {
		deps = &Deps{}
	}
	if deps.Detector == nil {
		deps.Detector = PathDetector{}
	}
	if deps.Models == nil {
		deps.Models = SubprocessModels{}
	}
	if deps.Profiles == nil {
		deps.Profiles = sharedProfileWriter{}
	}
	if deps.Collisions == nil {
		deps.Collisions = CollisionDetectorFunc(profilewrite.Collisions)
	}
	if deps.Settings == nil {
		deps.Settings = SettingsStoreFunc(func(mutator func(usersettings.Settings) usersettings.Settings) error {
			settings, err := usersettings.Load()
			if err != nil {
				return err
			}
			return usersettings.Save(mutator(settings))
		})
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.HomeDir == nil {
		deps.HomeDir = os.UserHomeDir
	}
	if deps.Cwd == nil {
		deps.Cwd = os.Getwd
	}
	if deps.EnumCLIs == nil {
		deps.EnumCLIs = config.EnumerateCLIs
	}
	return deps
}

func (m *Model) Init() tea.Cmd {
	if m.demoOnly {
		return nil
	}
	return tea.Batch(m.tickLoading(), m.discoverRecommendation())
}

func (m *Model) discoverRecommendation() tea.Cmd {
	return func() tea.Msg {
		adapters, err := m.deps.Detector.DetectAdapters()
		if err != nil {
			return discoveryLoadedMsg{err: err}
		}
		adapters = supportedAdapters(adapters)
		if len(adapters) == 0 {
			return discoveryLoadedMsg{err: fmt.Errorf("no supported CLI adapters were found on $PATH")}
		}

		discoveries := make([]profilerecommend.CLIDiscovery, len(adapters))
		var wg sync.WaitGroup
		wg.Add(len(adapters))
		for i, adapter := range adapters {
			go func() {
				defer wg.Done()
				models, discoveryErr := m.deps.Models.ModelsFor(adapter)
				discoveries[i] = profilerecommend.CLIDiscovery{
					CLI:    adapter,
					Models: slices.Clone(models),
				}
				if discoveryErr != nil {
					discoveries[i].DiscoveryError = discoveryErr.Error()
				}
			}()
		}
		wg.Wait()
		return discoveryLoadedMsg{discoveries: discoveries}
	}
}

func supportedAdapters(adapters []string) []string {
	var supported []string
	for _, adapter := range adapters {
		if !slices.Contains([]string{"claude", "codex", "copilot", "cursor", "opencode"}, adapter) ||
			slices.Contains(supported, adapter) {
			continue
		}
		supported = append(supported, adapter)
	}
	return supported
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case animTick:
		if m.animDone {
			return m, nil
		}
		m.animFrame++
		if m.animFrame >= animFrames {
			m.clearAnimation()
			return m, nil
		}
		cmd := m.tickAnim()
		return m, cmd
	case loadingTick:
		if !m.discoveryLoading && !m.pluginInstalling {
			return m, nil
		}
		m.loadingPhase++
		cmd := m.tickLoading()
		return m, cmd
	case discoveryLoadedMsg:
		m.discoveryLoading = false
		m.loadingPhase = 0
		if msg.err != nil {
			m.fail(msg.err)
			return m, tea.Quit
		}
		m.snapshot = profilerecommend.NewSnapshot(msg.discoveries)
		m.recommendation = profilerecommend.Recommend(m.snapshot)
		m.selections = m.recommendation.Selections()
		m.pairStatuses = m.recommendation.PairStatuses()
		m.setStage(stageRecommendation, recommendationOptions)
	case pluginDryRunMsg:
		if msg.err != nil {
			m.fail(msg.err)
			return m, tea.Quit
		}
		m.clearAnimation()
		m.pluginPreview = msg.preview
		m.setStage(stagePluginPreview, pluginConfirmOptions)
	case pluginInstallMsg:
		m.pluginInstalling = false
		m.loadingPhase = 0
		if msg.err != nil {
			m.fail(msg.err)
			return m, tea.Quit
		}
		m.pluginResult = msg.result
		done, cmd := m.complete()
		if done {
			return m, tea.Quit
		}
		if cmd != nil {
			return m, cmd
		}
		if !m.animDone {
			transitionCmd := m.tickAnim()
			return m, transitionCmd
		}
		return m, cmd
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}
	return m, nil
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if (!m.animDone || m.discoveryLoading || m.pluginInstalling) && key != "ctrl+c" && key != "esc" {
		return m, nil
	}
	switch key {
	case "ctrl+c":
		m.exitRequested()
		return m, tea.Quit
	case "esc":
		m.cancel()
		return m, tea.Quit
	case "up", "k", "left", "h":
		m.move(-1)
	case "down", "j", "tab", "right", "l":
		m.move(1)
	case "enter":
		done, cmd := m.enter()
		if done {
			return m, tea.Quit
		}
		if cmd != nil {
			return m, cmd
		}
		if !m.animDone {
			cmd := m.tickAnim()
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) tickAnim() tea.Cmd {
	return tea.Tick(animFrameTime, func(time.Time) tea.Msg { return animTick{} })
}

func (m *Model) tickLoading() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return loadingTick{} })
}

func (m *Model) startAnim() {
	m.animFrame = 0
	m.animDone = false
}

func (m *Model) clearAnimation() {
	m.animFrame = 0
	m.animDone = true
	m.prevView = ""
}

func (m *Model) Result() Result { return m.result }
func (m *Model) Err() error     { return m.err }
func (m *Model) Done() bool     { return m.terminal }

func (m *Model) move(delta int) {
	if len(m.options) <= 1 {
		return
	}
	m.focus = (m.focus + delta + len(m.options)) % len(m.options)
}

func (m *Model) enter() (bool, tea.Cmd) {
	if m.terminal || m.stage == stageLoading {
		return m.terminal, nil
	}
	selected := ""
	if len(m.options) > 0 {
		selected = m.options[m.focus]
	}
	switch m.stage {
	case stageRecommendation:
		if selected == recommendationOptions[1] {
			m.customizing = true
			m.roleCursor = 0
			m.openRoleCLI()
		} else {
			m.setStageAnimated(stageAutonomousBackend, autonomousBackendOptions())
		}
	case stageRoleCLI:
		m.prepareRoleModel(selected)
	case stageRoleModel:
		if selected == useCLIDefaultOption {
			m.selections[m.roleCursor] = m.defaultSelectionForCurrentCLI()
		} else {
			m.selections[m.roleCursor] = selectionForModel(m.currentRole(), m.currentCLI(), selected)
		}
		m.finalizeRole()
	case stageRoleModelDefault:
		m.selections[m.roleCursor] = m.defaultSelectionForCurrentCLI()
		m.finalizeRole()
	case stageAutonomousBackend:
		m.autonomousBackend = usersettings.AutonomousBackend(selected)
		m.setStageAnimated(stageAutonomousPermissionMode, autonomousPermissionModeOptions())
	case stageAutonomousPermissionMode:
		m.autonomousPermissionMode = usersettings.AutonomousPermissionMode(selected)
		if m.deps.Plugin != nil {
			m.setStageAnimated(stagePluginIntro, continueOptions)
		} else {
			m.setStageAnimated(stageScope, scopeOptions)
		}
	case stagePluginIntro:
		m.setStageAnimated(stageScope, scopeOptions)
	case stageScope:
		m.scope = selected
		if err := m.resolveTarget(); err != nil {
			return m.fail(err), nil
		}
		collisions, err := m.deps.Collisions.Collisions(m.targetPath)
		if err != nil {
			return m.fail(err), nil
		}
		m.collisions = collisions
		if len(collisions) > 0 {
			m.setStageAnimated(stageOverwrite, overwriteOptions)
			return false, nil
		}
		return m.prepareCompletion()
	case stageOverwrite:
		if selected == "Cancel" {
			m.cancel()
			return true, nil
		}
		return m.prepareCompletion()
	case stagePluginPreview:
		m.startPluginInstallLoading()
		return false, tea.Batch(m.tickLoading(), m.runPluginInstall())
	case stageDemoPrompt:
		return m.handleDemoPrompt(selected), nil
	}
	return false, nil
}

func (m *Model) openRoleCLI() {
	discoveries := m.snapshot.Discoveries()
	options := make([]string, len(discoveries))
	for i := range discoveries {
		options[i] = discoveries[i].CLI
	}
	m.setStageAnimated(stageRoleCLI, options)
	m.focusOption(m.selections[m.roleCursor].CLI)
}

func (m *Model) prepareRoleModel(cli string) {
	selection := m.recommendForCLI(m.currentRole(), cli)
	m.selections[m.roleCursor] = selection
	discovery, _ := m.snapshot.Discovery(cli)
	if discovery.DiscoveryError != "" || len(discovery.Models) == 0 {
		m.setStageAnimated(stageRoleModelDefault, continueOptions)
		return
	}

	options := slices.Clone(discovery.Models)
	if selection.Model == "" {
		options = append([]string{useCLIDefaultOption}, options...)
	} else {
		options = moveFirst(options, selection.Model)
	}
	m.setStageAnimated(stageRoleModel, options)
	m.focusOption(selection.Model)
	if selection.Model == "" {
		m.focusOption(useCLIDefaultOption)
	}
}

func moveFirst(options []string, value string) []string {
	index := slices.Index(options, value)
	if index <= 0 {
		return options
	}
	return append([]string{value}, append(options[:index], options[index+1:]...)...)
}

func (m *Model) recommendForCLI(role profilerecommend.Role, cli string) profilerecommend.Selection {
	discovery, ok := m.snapshot.Discovery(cli)
	if !ok {
		return profilerecommend.Selection{Role: role, CLI: cli}
	}
	snapshot := profilerecommend.NewSnapshot([]profilerecommend.CLIDiscovery{discovery})
	switch role {
	case profilerecommend.Crosscheck:
		selection, _ := profilerecommend.RecommendEvaluator(
			snapshot, role, &m.selections[roleIndex(profilerecommend.Lead)],
		)
		return selection
	case profilerecommend.Tester:
		selection, _ := profilerecommend.RecommendEvaluator(
			snapshot, role, &m.selections[roleIndex(profilerecommend.Implementor)],
		)
		return selection
	default:
		return profilerecommend.RecommendRole(snapshot, role)
	}
}

func selectionForModel(role profilerecommend.Role, cli, model string) profilerecommend.Selection {
	snapshot := profilerecommend.NewSnapshot([]profilerecommend.CLIDiscovery{{CLI: cli, Models: []string{model}}})
	selection := profilerecommend.RecommendRole(snapshot, role)
	if selection.Model == model {
		return selection
	}
	return profilerecommend.Selection{
		Role: role, CLI: cli, Model: model, Family: selection.Family,
	}
}

func (m *Model) defaultSelectionForCurrentCLI() profilerecommend.Selection {
	selection := m.recommendForCLI(m.currentRole(), m.currentCLI())
	selection.Model = ""
	selection.Tier = profilerecommend.UnrecognizedTier
	return selection
}

func (m *Model) finalizeRole() {
	role := m.currentRole()
	switch role {
	case profilerecommend.Lead:
		selection, status := profilerecommend.RecommendEvaluator(
			m.snapshot, profilerecommend.Crosscheck, &m.selections[m.roleCursor],
		)
		m.selections[roleIndex(profilerecommend.Crosscheck)] = selection
		m.pairStatuses[0] = status
	case profilerecommend.Implementor:
		selection, status := profilerecommend.RecommendEvaluator(
			m.snapshot, profilerecommend.Tester, &m.selections[m.roleCursor],
		)
		m.selections[roleIndex(profilerecommend.Tester)] = selection
		m.pairStatuses[1] = status
	}
	if m.roleCursor == len(profilerecommend.Roles())-1 {
		m.setStageAnimated(stageAutonomousBackend, autonomousBackendOptions())
		return
	}
	m.roleCursor++
	m.openRoleCLI()
}

func (m *Model) currentRole() profilerecommend.Role {
	roles := profilerecommend.Roles()
	if m.roleCursor < 0 || m.roleCursor >= len(roles) {
		return ""
	}
	return roles[m.roleCursor]
}

func roleIndex(role profilerecommend.Role) int {
	for i, candidate := range profilerecommend.Roles() {
		if candidate == role {
			return i
		}
	}
	return -1
}

func (m *Model) currentCLI() string {
	return m.selections[m.roleCursor].CLI
}

func (m *Model) focusOption(value string) {
	for i, option := range m.options {
		if option == value {
			m.focus = i
			return
		}
	}
}

func (m *Model) handleDemoPrompt(selected string) bool {
	switch selected {
	case "Continue":
		m.result = ResultDemo
	case "Not now":
		m.result = ResultCompleted
	case "Dismiss":
		stamp := m.deps.Clock().UTC().Format(time.RFC3339)
		if err := m.deps.Settings.Update(func(settings usersettings.Settings) usersettings.Settings {
			settings.Onboarding.Dismissed = stamp
			return settings
		}); err != nil {
			return m.fail(err)
		}
		m.result = ResultCompleted
	default:
		return false
	}
	m.terminal = true
	m.stage = stageDone
	return true
}

func (m *Model) resolveTarget() error {
	switch m.scope {
	case "global":
		home, err := m.deps.HomeDir()
		if err != nil {
			return err
		}
		m.targetPath = filepath.Join(home, ".agent-runner", "config.yaml")
	case "project":
		cwd, err := m.deps.Cwd()
		if err != nil {
			return err
		}
		m.targetPath = filepath.Join(cwd, ".agent-runner", "config.yaml")
	default:
		return fmt.Errorf("unsupported setup scope %q", m.scope)
	}
	return nil
}

func (m *Model) prepareCompletion() (bool, tea.Cmd) {
	req := &profilewrite.Request{
		TargetPath:       m.targetPath,
		LeadCLI:          m.selections[0].CLI,
		LeadModel:        m.selections[0].Model,
		CrosscheckCLI:    m.selections[1].CLI,
		CrosscheckModel:  m.selections[1].Model,
		ImplementorCLI:   m.selections[2].CLI,
		ImplementorModel: m.selections[2].Model,
		TesterCLI:        m.selections[3].CLI,
		TesterModel:      m.selections[3].Model,
	}
	staged, err := m.deps.Profiles.StageProfile(req)
	if err != nil {
		return m.fail(err), nil
	}
	m.pendingProfile = staged
	if m.deps.Plugin == nil {
		return m.complete()
	}

	clis, err := m.enumerateCLIs()
	if err != nil {
		return m.fail(err), nil
	}
	plan, err := m.deps.Plugin.Resolve(clis, m.scope)
	if err != nil {
		return m.fail(err), nil
	}
	if plan == nil {
		return m.complete()
	}
	m.pluginPlan = plan
	m.setStageAnimated(stagePluginPreview, nil)
	return false, m.runPluginDryRun()
}

func (m *Model) complete() (bool, tea.Cmd) {
	if m.pendingProfile == nil {
		return m.fail(fmt.Errorf("setup profile request is unavailable")), nil
	}
	if err := m.pendingProfile.Commit(); err != nil {
		return m.fail(err), nil
	}
	m.pendingProfile = nil

	stamp := m.deps.Clock().UTC().Format(time.RFC3339)
	if err := m.deps.Settings.Update(func(settings usersettings.Settings) usersettings.Settings {
		settings.AutonomousBackend = m.autonomousBackend
		settings.AutonomousPermissionMode = usersettings.EffectiveAutonomousPermissionMode(m.autonomousPermissionMode)
		settings.Setup.CompletedAt = stamp
		return settings
	}); err != nil {
		return m.fail(err), nil
	}
	if m.deps.OnboardingCompleted {
		m.result = ResultCompleted
		m.terminal = true
		m.stage = stageDone
		return true, nil
	}
	m.setStageAnimated(stageDemoPrompt, demoPromptOptions)
	return false, nil
}

func (m *Model) enumerateCLIs() ([]string, error) {
	home, err := m.deps.HomeDir()
	if err != nil {
		return nil, err
	}
	cwd, err := m.deps.Cwd()
	if err != nil {
		return nil, err
	}
	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	projectPath := filepath.Join(cwd, ".agent-runner", "config.yaml")
	switch m.scope {
	case "global":
		globalPath = m.pendingProfile.PreviewPath()
	case "project":
		projectPath = m.pendingProfile.PreviewPath()
	}
	clis, err := m.deps.EnumCLIs(globalPath, projectPath)
	if err != nil {
		return nil, err
	}
	for _, selection := range m.selections {
		if selection.CLI != "" && !slices.Contains(clis, selection.CLI) {
			clis = append(clis, selection.CLI)
		}
	}
	return clis, nil
}

func (m *Model) runPluginDryRun() tea.Cmd {
	return func() tea.Msg {
		preview, err := m.deps.Plugin.DryRun(m.pluginPlan)
		return pluginDryRunMsg{preview: preview, err: err}
	}
}

func (m *Model) runPluginInstall() tea.Cmd {
	return func() tea.Msg {
		result, err := m.deps.Plugin.Install(m.pluginPlan)
		return pluginInstallMsg{result: result, err: err}
	}
}

func (m *Model) startPluginInstallLoading() {
	m.pluginInstalling = true
	m.loadingPhase = 0
	m.options = nil
	m.focus = 0
}

func (m *Model) discardPendingProfile() {
	if m.pendingProfile == nil {
		return
	}
	_ = m.pendingProfile.Discard()
	m.pendingProfile = nil
}

func (m *Model) setStage(next stage, options []string) {
	m.stage = next
	m.options = slices.Clone(options)
	m.focus = m.defaultFocus(next, m.options)
}

func (m *Model) setStageAnimated(next stage, options []string) {
	m.setStage(next, options)
	m.startAnim()
}

func (m *Model) cancel() {
	m.discardPendingProfile()
	m.result = ResultCancelled
	m.terminal = true
	m.stage = stageDone
}

func (m *Model) exitRequested() {
	m.discardPendingProfile()
	m.result = ResultExitRequested
	m.terminal = true
	m.stage = stageDone
}

func (m *Model) fail(err error) bool {
	m.discardPendingProfile()
	m.err = err
	m.result = ResultFailed
	m.terminal = true
	m.stage = stageDone
	m.options = nil
	return true
}

func (m *Model) View() string {
	content := m.renderPanel()
	if m.width >= minCenterWidth && m.height >= minCenterHeight {
		return renderCenteredSetup(content, m.width, m.height, m.animDone, m.animFrame)
	}
	return content
}

func (m *Model) renderPanel() string {
	contentWidth := setupContentWidth(m.width)
	textWidth := setupTextWidth(contentWidth)
	title, body, prompt := m.screenContent()

	var b strings.Builder
	if progress := m.renderProgress(contentWidth); progress != "" {
		b.WriteString(lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, progress))
		b.WriteString("\n\n")
	}
	b.WriteString(setupTitleStyle.Render(title))
	b.WriteString("\n\n")
	if body != "" {
		b.WriteString(renderWrapped(body, textWidth, setupBodyStyle.Render))
		b.WriteString("\n\n")
	}
	if prompt != "" {
		b.WriteString(renderWrapped(prompt, textWidth, tuistyle.HeaderStyle.Render))
		b.WriteString("\n")
	}
	if len(m.options) > 0 {
		if prompt != "" {
			b.WriteString("\n")
		}
		b.WriteString(m.renderOptions(contentWidth, textWidth))
	}
	return lipgloss.NewStyle().
		Width(contentWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuistyle.DimText).
		Padding(1, 2).
		Render(b.String())
}

func renderCenteredSetup(content string, width, height int, animDone bool, frame int) string {
	renderedContent := content
	if !animDone && frame <= animFrames/2 {
		renderedContent = setupTransitionStyle.Render(renderedContent)
	}
	rendered := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, renderedContent)
	if animDone {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	panelHeight := lipgloss.Height(renderedContent)
	panelWidth := lipgloss.Width(renderedContent)
	statusRow := max((height-panelHeight)/2, 0) + panelHeight
	if statusRow >= len(lines) {
		return rendered
	}
	lines[statusRow] = strings.Repeat(" ", max((width-panelWidth)/2, 0)) + renderSetupTransitionStatus(frame)
	return strings.Join(lines, "\n")
}

func renderSetupTransitionStatus(frame int) string {
	return setupTransitionStatusStyle.Render(tuistyle.SpinnerGlyph(float64(frame)) + " Preparing next step...")
}

func setupPanelWidth(termWidth int) int {
	if termWidth <= 0 {
		termWidth = 80
	}
	available := termWidth - 4
	if termWidth < minCenterWidth {
		available = termWidth - 2
	}
	if available < minPanelWidth {
		return max(20, available)
	}
	return min(maxPanelWidth, available)
}

func setupContentWidth(termWidth int) int {
	return max(10, setupPanelWidth(termWidth)-panelFrameWidth)
}

func setupTextWidth(contentWidth int) int {
	return max(10, contentWidth-textWrapInset)
}

func (m *Model) renderOptions(width, textWidth int) string {
	if m.stage == stageDemoPrompt {
		return tuistyle.RenderButtonRow(m.options, m.focus, width)
	}
	if m.stage == stageRoleModel {
		return m.renderWindowedOptions(textWidth)
	}
	var b strings.Builder
	for i, option := range m.options {
		label := m.optionLabel(option)
		prefix, style := m.optionPresentation(i)
		for _, line := range wrapTextLine(prefix+label, textWidth) {
			b.WriteString(style.Render(line))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderWindowedOptions(textWidth int) string {
	start, end := optionWindow(m.focus, len(m.options), m.maxVisibleOptions())
	var b strings.Builder
	for i := start; i < end; i++ {
		prefix, style := m.optionPresentation(i)
		b.WriteString(style.Render(runewidth.Truncate(prefix+m.optionLabel(m.options[i]), textWidth, "...")))
		b.WriteByte('\n')
	}
	if start > 0 || end < len(m.options) {
		b.WriteString(tuistyle.DimStyle.Render(fmt.Sprintf(
			"Showing %d-%d of %d. Use up/down to choose.", start+1, end, len(m.options),
		)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) optionPresentation(index int) (string, lipgloss.Style) {
	if index == m.focus {
		return tuistyle.FocusedSelectorPrefix + " ", setupFocusedOptionStyle
	}
	return "  ", setupOptionStyle
}

func (m *Model) maxVisibleOptions() int {
	height := m.height
	if height <= 0 {
		height = 24
	}
	return max(4, min(12, height-13))
}

func optionWindow(focus, total, maxVisible int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	if maxVisible <= 0 || total <= maxVisible {
		return 0, total
	}
	start = focus - maxVisible/2
	if start < 0 {
		start = 0
	}
	if start+maxVisible > total {
		start = total - maxVisible
	}
	return start, start + maxVisible
}

func (m *Model) renderProgress(width int) string {
	current, total, ok := m.setupProgress()
	if !ok {
		return ""
	}
	return tuistyle.RenderStepIndicator(current, total, width)
}

type semanticStep string

const (
	stepRecommendation semanticStep = "recommendation"
	stepBackend        semanticStep = "backend"
	stepPermission     semanticStep = "permission"
	stepPluginIntro    semanticStep = "plugin-intro"
	stepScope          semanticStep = "scope"
	stepOverwrite      semanticStep = "overwrite"
	stepPluginPreview  semanticStep = "plugin-preview"
	stepDemo           semanticStep = "demo"
)

func roleStep(role profilerecommend.Role) semanticStep {
	return semanticStep("role-" + role)
}

func (m *Model) stepPlan() []semanticStep {
	plan := []semanticStep{stepRecommendation}
	if m.customizing {
		for _, role := range profilerecommend.Roles() {
			plan = append(plan, roleStep(role))
		}
	}
	plan = append(plan, stepBackend, stepPermission)
	if m.deps.Plugin != nil {
		plan = append(plan, stepPluginIntro)
	}
	plan = append(plan, stepScope)
	if len(m.collisions) > 0 || m.stage == stageOverwrite {
		plan = append(plan, stepOverwrite)
	}
	if m.deps.Plugin != nil {
		plan = append(plan, stepPluginPreview)
	}
	if !m.deps.OnboardingCompleted {
		plan = append(plan, stepDemo)
	}
	return plan
}

func (m *Model) setupProgress() (current, total int, ok bool) {
	if m.demoOnly || m.stage == stageLoading || m.stage == stageDone {
		return 0, 0, false
	}
	var currentStep semanticStep
	switch m.stage {
	case stageRecommendation:
		currentStep = stepRecommendation
	case stageRoleCLI, stageRoleModelDefault, stageRoleModel:
		currentStep = roleStep(m.currentRole())
	case stageAutonomousBackend:
		currentStep = stepBackend
	case stageAutonomousPermissionMode:
		currentStep = stepPermission
	case stagePluginIntro:
		currentStep = stepPluginIntro
	case stageScope:
		currentStep = stepScope
	case stageOverwrite:
		currentStep = stepOverwrite
	case stagePluginPreview:
		currentStep = stepPluginPreview
	case stageDemoPrompt:
		currentStep = stepDemo
	default:
		return 0, 0, false
	}
	plan := m.stepPlan()
	for i, step := range plan {
		if step == currentStep {
			return i + 1, len(plan), true
		}
	}
	return 0, 0, false
}

func (m *Model) defaultFocus(next stage, options []string) int {
	preferred := ""
	switch next {
	case stageAutonomousBackend:
		preferred = string(usersettings.BackendHeadless)
	case stageAutonomousPermissionMode:
		preferred = string(usersettings.PermissionModeConservative)
	default:
		return 0
	}
	if index := slices.Index(options, preferred); index >= 0 {
		return index
	}
	return 0
}

func (m *Model) optionLabel(option string) string {
	switch m.stage {
	case stageAutonomousBackend:
		switch usersettings.AutonomousBackend(option) {
		case usersettings.BackendHeadless:
			return "Headless - Runs each autonomous step in non-interactive print mode (recommended)"
		case usersettings.BackendInteractive:
			return "Interactive - Opens every autonomous step in an interactive session with autonomy instructions"
		case usersettings.BackendInteractiveClaude:
			return "Interactive for Claude - Opens Claude interactively while other CLIs use non-interactive print mode"
		}
	case stageAutonomousPermissionMode:
		switch usersettings.AutonomousPermissionMode(option) {
		case usersettings.PermissionModeConservative:
			return "Conservative - Use each CLI's default permission flags; unavailable tools may stop unattended work"
		case usersettings.PermissionModeYOLO:
			return "YOLO - Pre-approve shell, file, and network actions; use only inside an external sandbox"
		}
	case stageScope:
		if option == "global" {
			return "Global - Use this profile in every project"
		}
		if option == "project" {
			return "Project - Use this profile only in the current repository"
		}
	}
	return option
}

func (m *Model) screenContent() (title, body, prompt string) {
	switch m.stage {
	case stageLoading:
		title = "Set Up Agent Runner"
		body = "Welcome to initial setup. Agent Runner is inspecting the available CLIs and their models so it can prepare a four-role profile recommendation."
		prompt = tuistyle.SpinnerGlyph(m.loadingPhase) + " Discovering available CLIs and models..."
	case stageRecommendation:
		title = "Recommended Agent Profile"
		body = m.recommendationSummary()
		prompt = "Accept the complete profile or customize each role in order."
	case stageRoleCLI:
		role := m.currentRole()
		title = roleTitle(role) + " CLI"
		body = roleExplanation(role) + " This choice controls which installed CLI launches the role."
		prompt = "Choose the CLI for " + roleTitle(role) + "."
	case stageRoleModel:
		role := m.currentRole()
		title = roleTitle(role) + " Model"
		body = roleExplanation(role) + " This choice controls the model used when that role runs through " + m.currentCLI() + "."
		prompt = "Choose the model for " + roleTitle(role) + "."
	case stageRoleModelDefault:
		role := m.currentRole()
		discovery, _ := m.snapshot.Discovery(m.currentCLI())
		title = roleTitle(role) + " Model"
		body = roleExplanation(role) + " Agent Runner will use the CLI default and leave the model field unset."
		if discovery.DiscoveryError != "" {
			body += " Model discovery reported: " + discovery.DiscoveryError + "."
		} else {
			body += " No selectable models were returned by this CLI."
		}
		prompt = "Continue with the CLI default?"
	case stageAutonomousBackend:
		title = "Autonomous Backend"
		body = "Choose how autonomous steps are invoked at runtime. Headless uses non-interactive print mode and is the recommended default; Interactive opens every CLI in a session, while Interactive for Claude does so only for Claude."
		prompt = "Choose the backend for autonomous steps."
	case stageAutonomousPermissionMode:
		title = "Autonomous Permission Mode"
		body = "Choose how much tool authority autonomous steps receive. Conservative keeps each CLI's normal permission behavior, while YOLO pre-approves actions for externally sandboxed runs."
		prompt = "Choose the permission mode for autonomous steps."
	case stagePluginIntro:
		title = "Agent Skills"
		body = "Agent Runner uses skills from https://github.com/Codagent-AI/agent-skills. They provide focused spec, TDD, validation, PR, and CI workflows for the CLIs in this profile.\n\nNext you will choose the profile scope, which also controls where these skills are installed."
		prompt = "Continue to profile scope."
	case stageScope:
		title = "Config Scope"
		body = "Global saves the profile to ~/.agent-runner/config.yaml for every project. Project saves it to .agent-runner/config.yaml in the current repository, where it applies only to that project."
		prompt = "Where should Agent Runner save the profile and install skills?"
	case stageOverwrite:
		title = "Existing Agent Profiles"
		body = "These managed entries already exist and will be replaced: " + strings.Join(m.collisions, ", ") + ". Unmanaged agents and unrelated configuration will be preserved."
		prompt = "Overwrite the existing managed entries?"
	case stagePluginPreview:
		title = "Install Agent Skills"
		body, prompt = m.pluginPreviewContent()
	case stageDemoPrompt:
		title = "Agent Runner Workflow Demo"
		body = "Agent Runner includes a short interactive demo showing UI prompts, interactive and autonomous agents, shell commands, and captured data. It takes about two minutes and runs real workflow steps."
		prompt = "Run the demo now?"
	case stageDone:
		title, body = m.doneContent()
	default:
		title = "Set Up Agent Runner"
	}
	return title, body, prompt
}

func (m *Model) recommendationSummary() string {
	var b strings.Builder
	b.WriteString("Your four-role profile configures interactive leadership, an independent planning crosscheck, implementation, and acceptance testing. Crosscheck challenges Lead's planning artifacts for omissions and completeness; Agent Validator remains responsible for implementation-code review.\n\n")
	for _, selection := range m.selections {
		model := selection.Model
		if model == "" {
			model = "CLI default"
		}
		fmt.Fprintf(&b, "%s: %s / %s\n", roleTitle(selection.Role), selection.CLI, model)
	}
	for _, discovery := range m.snapshot.Discoveries() {
		if discovery.DiscoveryError != "" {
			fmt.Fprintf(&b, "\n%s model discovery was limited (%s); affected recommendations use the CLI default.", discovery.CLI, discovery.DiscoveryError)
		}
	}
	for _, status := range m.pairStatuses {
		if !status.Limited {
			continue
		}
		fmt.Fprintf(
			&b,
			"\n%s could not establish known model-family diversity, so normal recommendation precedence was used.",
			pairTitle(status.Pair),
		)
	}
	return strings.TrimSpace(b.String())
}

func roleTitle(role profilerecommend.Role) string {
	switch role {
	case profilerecommend.Lead:
		return "Lead"
	case profilerecommend.Crosscheck:
		return "Crosscheck"
	case profilerecommend.Implementor:
		return "Implementor"
	case profilerecommend.Tester:
		return "Tester"
	default:
		return string(role)
	}
}

func roleExplanation(role profilerecommend.Role) string {
	switch role {
	case profilerecommend.Lead:
		return "Lead works with you interactively to shape goals, decisions, plans, and specifications."
	case profilerecommend.Crosscheck:
		return "Crosscheck independently challenges Lead's planning artifacts and looks for omissions; Agent Validator reviews implementation code."
	case profilerecommend.Implementor:
		return "Implementor carries out code changes and focused validation autonomously."
	case profilerecommend.Tester:
		return "Tester exercises completed work against acceptance expectations independently."
	default:
		return ""
	}
}

func pairTitle(pair profilerecommend.Pair) string {
	switch pair {
	case profilerecommend.LeadCrosscheck:
		return "Lead/Crosscheck"
	case profilerecommend.ImplementorTester:
		return "Implementor/Tester"
	default:
		return string(pair)
	}
}

func (m *Model) doneContent() (title, body string) {
	switch {
	case m.result == ResultCancelled:
		return "Setup Cancelled", ""
	case m.result == ResultExitRequested:
		return "Setup Interrupted", ""
	case m.err != nil:
		return "Setup Failed", m.err.Error()
	default:
		return "Setup Complete", ""
	}
}

func (m *Model) pluginPreviewContent() (body, prompt string) {
	switch {
	case m.pluginInstalling:
		return tuistyle.SpinnerGlyph(m.loadingPhase) + " Installing agent skills.\n\nThis can take a moment.", ""
	case m.pluginPreview == nil:
		return "Preparing skill installation...", ""
	default:
		body = m.pluginPreview.Output
		if m.pluginResult != nil && m.pluginResult.Warning != "" {
			body += "\n\nWarning: " + m.pluginResult.Warning
		}
		return body, "Install skills for your configured CLIs?"
	}
}

func renderWrapped(text string, width int, render func(...string) string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = tuistyle.Sanitize(line)
		if line == "" {
			lines = append(lines, "")
			continue
		}
		for _, wrapped := range wrapTextLine(line, width) {
			lines = append(lines, render(wrapped))
		}
	}
	return strings.Join(lines, "\n")
}

func wrapTextLine(s string, width int) []string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var out []string
	var cur []string
	curW := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, strings.Join(cur, " "))
		cur = nil
		curW = 0
	}
	for _, word := range words {
		wordW := runewidth.StringWidth(word)
		if wordW > width {
			flush()
			remaining := word
			for runewidth.StringWidth(remaining) > width {
				chunk := runewidth.Truncate(remaining, width, "")
				if chunk == "" {
					_, size := utf8.DecodeRuneInString(remaining)
					if size == 0 {
						break
					}
					chunk = remaining[:size]
				}
				out = append(out, chunk)
				remaining = remaining[len(chunk):]
			}
			if remaining != "" {
				cur = []string{remaining}
				curW = runewidth.StringWidth(remaining)
			}
			continue
		}
		if curW == 0 {
			cur = []string{word}
			curW = wordW
			continue
		}
		if curW+1+wordW > width {
			if wordW <= 4 && len(cur) > 1 {
				last := cur[len(cur)-1]
				cur = cur[:len(cur)-1]
				flush()
				cur = []string{last, word}
				curW = runewidth.StringWidth(last) + 1 + wordW
			} else {
				flush()
				cur = []string{word}
				curW = wordW
			}
			continue
		}
		cur = append(cur, word)
		curW += 1 + wordW
	}
	flush()
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

type PathDetector struct{}

func (PathDetector) DetectAdapters() ([]string, error) {
	var found []string
	for _, adapter := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		if _, err := exec.LookPath(adapter); err == nil {
			found = append(found, adapter)
		}
	}
	return found, nil
}

type SubprocessModels struct{}

var (
	subprocessModelTimeout = 5 * time.Second
	modelCommandContext    = exec.CommandContext
)

func (SubprocessModels) ModelsFor(adapter string) ([]string, error) {
	var args []string
	switch adapter {
	case "claude":
		return []string{"opus", "sonnet"}, nil
	case "codex":
		args = []string{"debug", "models"}
	case "opencode":
		args = []string{"models"}
	default:
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), subprocessModelTimeout)
	defer cancel()
	out, err := modelCommandContext(ctx, adapter, args...).Output() // #nosec G204 -- adapter is selected from supported CLI names.
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("discover %s models: %w", adapter, ctx.Err())
		}
		return nil, nil
	}
	return parseModelOutput(adapter, string(out)), nil
}

func parseModelOutput(adapter, out string) []string {
	switch adapter {
	case "claude":
		return parseClaudeModels(out)
	case "codex":
		return parseCodexModels(out)
	default:
		return parseLineModels(out)
	}
}

var claudeModelPattern = regexp.MustCompile(`^[a-z0-9._-]*(opus|sonnet|haiku)[a-z0-9._-]*$`)

func parseClaudeModels(out string) []string {
	seen := map[string]bool{}
	var models []string
	for _, field := range strings.Fields(out) {
		candidate := strings.Trim(field, `|*`+"`"+`"(),:;`)
		if claudeModelPattern.MatchString(candidate) && !seen[candidate] {
			seen[candidate] = true
			models = append(models, candidate)
		}
	}
	return models
}

func parseCodexModels(out string) []string {
	type entry struct {
		Slug       string `json:"slug"`
		Visibility string `json:"visibility"`
	}
	var entries []entry
	var envelope struct {
		Models []entry `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err == nil && len(envelope.Models) > 0 {
		entries = envelope.Models
	} else if err := json.Unmarshal([]byte(out), &entries); err != nil {
		var one entry
		dec := json.NewDecoder(strings.NewReader("[" + strings.Trim(out, " \n,") + "]"))
		if err := dec.Decode(&entries); err != nil {
			if err := json.Unmarshal([]byte(out), &one); err != nil || one.Slug == "" {
				return nil
			}
			entries = []entry{one}
		}
	}
	var models []string
	for _, entry := range entries {
		if entry.Slug == "" || entry.Visibility != "list" || slices.Contains(models, entry.Slug) {
			continue
		}
		models = append(models, entry.Slug)
	}
	return models
}

func parseLineModels(out string) []string {
	seen := map[string]bool{}
	var models []string
	for _, line := range strings.Split(out, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		models = append(models, candidate)
	}
	return models
}
