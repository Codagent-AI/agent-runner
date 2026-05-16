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
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/agentplugin"
	"github.com/codagent/agent-runner/internal/config"
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
	WriteProfile(*profilewrite.Request) error
}

type ProfileWriterFunc func(*profilewrite.Request) error

func (f ProfileWriterFunc) WriteProfile(req *profilewrite.Request) error { return f(req) }

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
	stageInteractiveCLI stage = iota
	stageInteractiveModelDefault
	stageInteractiveModel
	stageHeadlessCLI
	stageHeadlessModelDefault
	stageHeadlessModel
	stageScope
	stageOverwrite
	stagePluginIntro
	stagePluginPreview
	stageDemoPrompt
	stageDone
)

var (
	scopeOptions         = []string{"global", "project"}
	overwriteOptions     = []string{"Overwrite", "Cancel"}
	pluginConfirmOptions = []string{"Install"}
	demoPromptOptions    = []string{"Continue", "Not now", "Dismiss"}
	continueOptions      = []string{"Continue"}
)

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

type modelsLoadedMsg struct {
	next   stage
	models []string
	err    error
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
	deps             Deps
	stage            stage
	focus            int
	options          []string
	adapters         []string
	interactiveCLI   string
	interactiveModel string
	headlessCLI      string
	headlessModel    string
	scope            string
	targetPath       string
	collisions       []string
	width            int
	height           int
	result           Result
	err              error
	terminal         bool
	demoOnly         bool

	pluginPlan       *agentplugin.Plan
	pluginPreview    *agentplugin.Preview
	pluginResult     *agentplugin.Result
	pluginInstalling bool

	animDone  bool
	animFrame int
	prevView  string
	pending   *modelsLoadedMsg

	modelsLoading bool
	loadingPhase  float64
}

func NewModel(deps *Deps) *Model {
	deps = fillDefaults(deps)
	m := &Model{
		deps:     *deps,
		width:    80,
		height:   24,
		animDone: true,
	}
	m.loadAdapters()
	return m
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
		deps.Profiles = ProfileWriterFunc(profilewrite.Write)
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

func (m *Model) Init() tea.Cmd { return nil }

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
			m.animDone = true
			m.animFrame = 0
			m.prevView = ""
			if m.pending != nil {
				pending := *m.pending
				m.pending = nil
				cmd := m.applyModelsLoaded(pending)
				return m, cmd
			}
			return m, nil
		}
		cmd := m.tickAnim()
		return m, cmd
	case loadingTick:
		if !m.modelsLoading && !m.pluginInstalling {
			return m, nil
		}
		m.loadingPhase++
		cmd := m.tickLoading()
		return m, cmd
	case modelsLoadedMsg:
		if !m.animDone {
			m.pending = &msg
			return m, nil
		}
		cmd := m.applyModelsLoaded(msg)
		return m, cmd
	case pluginDryRunMsg:
		if msg.err != nil {
			m.fail(msg.err)
			return m, tea.Quit
		}
		m.clearAnimation()
		m.pluginPreview = msg.preview
		m.setStage(stagePluginPreview, pluginConfirmOptions)
		return m, nil
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
			cmd := m.tickAnim()
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}
	return m, nil
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if !m.animDone && key != "ctrl+c" && key != "esc" {
		return m, nil
	}
	if m.modelsLoading && key != "ctrl+c" && key != "esc" {
		return m, nil
	}
	if m.pluginInstalling && key != "ctrl+c" && key != "esc" {
		return m, nil
	}
	switch key {
	case "ctrl+c":
		m.exitRequested()
		return m, tea.Quit
	case "esc":
		m.cancel()
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j", "tab":
		m.move(1)
	case "left", "h":
		m.move(-1)
	case "right", "l":
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
	return tea.Tick(animFrameTime, func(time.Time) tea.Msg {
		return animTick{}
	})
}

func (m *Model) tickLoading() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return loadingTick{}
	})
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

func (m *Model) discoverModels(next stage, adapter string) tea.Cmd {
	return func() tea.Msg {
		models, err := m.deps.Models.ModelsFor(adapter)
		return modelsLoadedMsg{next: next, models: models, err: err}
	}
}

func (m *Model) applyModelsLoaded(msg modelsLoadedMsg) tea.Cmd {
	m.modelsLoading = false
	m.loadingPhase = 0
	if msg.err != nil || len(msg.models) == 0 {
		m.skipModelSelection(msg.next)
		return nil
	}
	m.setStage(msg.next, msg.models)
	return nil
}

func (m *Model) Result() Result { return m.result }

func (m *Model) Err() error { return m.err }

func (m *Model) Done() bool { return m.terminal }

func (m *Model) move(delta int) {
	if len(m.options) <= 1 {
		return
	}
	m.focus = (m.focus + delta + len(m.options)) % len(m.options)
}

func (m *Model) enter() (bool, tea.Cmd) {
	if m.terminal {
		return true, nil
	}
	selected := ""
	if len(m.options) > 0 {
		selected = m.options[m.focus]
	}
	switch m.stage {
	case stageInteractiveCLI:
		m.interactiveCLI = selected
		m.startModelLoading()
		m.setStageAnimated(stageInteractiveModel, nil)
		return false, tea.Batch(m.tickAnim(), m.tickLoading(), m.discoverModels(stageInteractiveModel, selected))
	case stageInteractiveModel:
		m.interactiveModel = selected
		m.setStageAnimated(stageHeadlessCLI, m.adapters)
	case stageInteractiveModelDefault:
		m.interactiveModel = ""
		m.setStageAnimated(stageHeadlessCLI, m.adapters)
	case stageHeadlessCLI:
		m.headlessCLI = selected
		m.startModelLoading()
		m.setStageAnimated(stageHeadlessModel, nil)
		return false, tea.Batch(m.tickAnim(), m.tickLoading(), m.discoverModels(stageHeadlessModel, selected))
	case stageHeadlessModel:
		m.headlessModel = selected
		m.setStageAfterModelSelection()
	case stageHeadlessModelDefault:
		m.headlessModel = ""
		m.setStageAfterModelSelection()
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
		return m.write()
	case stageOverwrite:
		if selected == "Cancel" {
			m.cancel()
			return true, nil
		}
		return m.write()
	case stagePluginIntro:
		m.setStageAnimated(stageScope, scopeOptions)
	case stagePluginPreview:
		m.startPluginInstallLoading()
		return false, tea.Batch(m.tickLoading(), m.runPluginInstall())
	case stageDemoPrompt:
		return m.handleDemoPrompt(selected), nil
	}
	return false, nil
}

func (m *Model) handleDemoPrompt(selected string) bool {
	switch selected {
	case "Continue":
		m.result = ResultDemo
		m.terminal = true
		m.stage = stageDone
		return true
	case "Not now":
		m.result = ResultCompleted
		m.terminal = true
		m.stage = stageDone
		return true
	case "Dismiss":
		stamp := m.deps.Clock().UTC().Format(time.RFC3339)
		if err := m.deps.Settings.Update(func(settings usersettings.Settings) usersettings.Settings {
			settings.Onboarding.Dismissed = stamp
			return settings
		}); err != nil {
			return m.fail(err)
		}
		m.result = ResultCompleted
		m.terminal = true
		m.stage = stageDone
		return true
	}
	return false
}

func (m *Model) loadAdapters() bool {
	adapters, err := m.deps.Detector.DetectAdapters()
	if err != nil {
		return m.fail(err)
	}
	if len(adapters) == 0 {
		return m.fail(fmt.Errorf("no supported CLI adapters were found on $PATH"))
	}
	m.adapters = adapters
	m.setStage(stageInteractiveCLI, adapters)
	return false
}

func (m *Model) skipModelSelection(next stage) {
	if next == stageInteractiveModel {
		m.setStage(stageInteractiveModelDefault, continueOptions)
		return
	}
	m.setStage(stageHeadlessModelDefault, continueOptions)
}

func (m *Model) startModelLoading() {
	m.modelsLoading = true
	m.loadingPhase = 0
}

func (m *Model) startPluginInstallLoading() {
	m.pluginInstalling = true
	m.loadingPhase = 0
	m.options = nil
	m.focus = 0
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

func (m *Model) write() (bool, tea.Cmd) {
	err := m.deps.Profiles.WriteProfile(&profilewrite.Request{
		TargetPath:       m.targetPath,
		InteractiveCLI:   m.interactiveCLI,
		InteractiveModel: m.interactiveModel,
		HeadlessCLI:      m.headlessCLI,
		HeadlessModel:    m.headlessModel,
	})
	if err != nil {
		return m.fail(err), nil
	}

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

func (m *Model) setStageAfterModelSelection() {
	if m.deps.Plugin != nil {
		m.setStageAnimated(stagePluginIntro, continueOptions)
		return
	}
	m.setStageAnimated(stageScope, scopeOptions)
}

func (m *Model) complete() (bool, tea.Cmd) {
	stamp := m.deps.Clock().UTC().Format(time.RFC3339)
	if err := m.deps.Settings.Update(func(settings usersettings.Settings) usersettings.Settings {
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
	globalPath := filepath.Join(home, ".agent-runner", "config.yaml")
	cwd, err := m.deps.Cwd()
	if err != nil {
		return nil, err
	}
	projectPath := filepath.Join(cwd, ".agent-runner", "config.yaml")
	return m.deps.EnumCLIs(globalPath, projectPath)
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
		if err != nil {
			return pluginInstallMsg{err: err}
		}
		return pluginInstallMsg{result: result}
	}
}

func (m *Model) setStage(next stage, options []string) {
	m.stage = next
	m.options = append([]string(nil), options...)
	m.focus = m.defaultFocus(next, m.options)
}

func (m *Model) setStageAnimated(next stage, options []string) {
	m.setStage(next, options)
	m.startAnim()
}

func (m *Model) cancel() {
	m.result = ResultCancelled
	m.terminal = true
	m.stage = stageDone
}

func (m *Model) exitRequested() {
	m.result = ResultExitRequested
	m.terminal = true
	m.stage = stageDone
}

func (m *Model) fail(err error) bool {
	m.err = err
	m.result = ResultFailed
	m.terminal = true
	m.stage = stageDone
	return true
}

func (m *Model) View() string {
	content := m.renderPanel()

	if m.width >= minCenterWidth && m.height >= minCenterHeight {
		if !m.animDone {
			content = renderSetupTransition(content, m.animFrame)
		}
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
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

func renderSetupTransition(content string, frame int) string {
	content += "\n" + setupTransitionStatusStyle.Render(tuistyle.SpinnerGlyph(float64(frame))+" Preparing next step...")
	if frame <= animFrames/2 {
		return setupTransitionStyle.Render(content)
	}
	return content
}

func modelSelectionPrompt(loading bool, phase float64, cliName, readyPrompt string) string {
	if loading {
		return tuistyle.SpinnerGlyph(phase) + " Checking available models for " + cliName + "."
	}
	return readyPrompt
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
	// Account for two border cells and two columns of horizontal padding.
	return max(10, setupPanelWidth(termWidth)-panelFrameWidth)
}

func setupTextWidth(contentWidth int) int {
	return max(10, contentWidth-textWrapInset)
}

func (m *Model) renderOptions(width, textWidth int) string {
	if m.stage == stageDemoPrompt {
		return m.renderButtonRow(width)
	}

	var b strings.Builder
	for i, option := range m.options {
		label := m.optionLabel(option)
		prefix := "  "
		style := setupOptionStyle
		if i == m.focus {
			prefix = tuistyle.FocusedSelectorPrefix + " "
			style = setupFocusedOptionStyle
		}
		lines := wrapTextLine(prefix+label, textWidth)
		for _, line := range lines {
			b.WriteString(style.Render(line))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderButtonRow(width int) string {
	return tuistyle.RenderButtonRow(m.options, m.focus, width)
}

func (m *Model) renderProgress(width int) string {
	current, total, ok := m.setupProgress()
	if !ok {
		return ""
	}
	return tuistyle.RenderStepIndicator(current, total, width)
}

func (m *Model) setupProgress() (current, total int, ok bool) {
	if m.demoOnly {
		return 0, 0, false
	}
	total = 6
	hasOverwrite := m.stage == stageOverwrite || len(m.collisions) > 0
	hasPlugin := m.deps.Plugin != nil
	if hasOverwrite {
		total++
	}
	if hasPlugin {
		total += 2
	}

	switch m.stage {
	case stageInteractiveCLI:
		return 1, total, true
	case stageInteractiveModelDefault, stageInteractiveModel:
		return 2, total, true
	case stageHeadlessCLI:
		return 3, total, true
	case stageHeadlessModelDefault, stageHeadlessModel:
		return 4, total, true
	case stagePluginIntro:
		return 5, total, true
	case stageScope:
		step := 5
		if hasPlugin {
			step = 6
		}
		return step, total, true
	case stageOverwrite:
		step := 6
		if hasPlugin {
			step = 7
		}
		return step, total, true
	case stagePluginPreview:
		step := 7
		if hasOverwrite {
			step = 8
		}
		return step, total, true
	case stageDemoPrompt:
		return total, total, true
	default:
		return 0, 0, false
	}
}

func (m *Model) defaultFocus(next stage, options []string) int {
	preferred := ""
	switch next {
	case stageInteractiveCLI:
		preferred = "claude"
	case stageHeadlessCLI:
		preferred = "codex"
	default:
		return 0
	}
	for i, option := range options {
		if option == preferred {
			return i
		}
	}
	return 0
}

func (m *Model) optionLabel(option string) string {
	switch m.stage {
	case stageInteractiveCLI:
		if option == "claude" {
			return option + " (recommended)"
		}
	case stageHeadlessCLI:
		if option == "codex" {
			return option + " (recommended)"
		}
	}
	return option
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

func (m *Model) screenContent() (title, body, prompt string) {
	switch m.stage {
	case stageInteractiveCLI:
		title = "Set Up Agent Runner"
		body = "Welcome. Agent Runner uses a planner for interactive work and an implementor for unattended implementation tasks. The choices here become your default agent profile."
		prompt = "Choose the CLI for the planner agent."
	case stageInteractiveModelDefault:
		title = "Planner Model"
		body = "No selectable models were found for " + m.interactiveCLI + ". Agent Runner will use the CLI default and leave the model field unset."
		prompt = "Continue with the CLI default?"
	case stageInteractiveModel:
		title = "Planner Model"
		body = "The planner handles conversations, planning, and decisions that need your input. Pick the model that " + m.interactiveCLI + " should use for those interactive workflow steps."
		prompt = modelSelectionPrompt(m.modelsLoading, m.loadingPhase, m.interactiveCLI, "Choose the planner model.")
	case stageHeadlessCLI:
		title = "Implementor CLI"
		body = "The implementor runs headless tasks such as code generation, edits, and validation follow-ups. This CLI is used when Agent Runner needs work to continue without an interactive session."
		prompt = "Choose the CLI for the implementor agent."
	case stageHeadlessModelDefault:
		title = "Implementor Model"
		body = "No selectable models were found for " + m.headlessCLI + ". Agent Runner will use the CLI default and leave the model field unset."
		prompt = "Continue with the CLI default?"
	case stageHeadlessModel:
		title = "Implementor Model"
		body = "The implementor model is used for unattended implementation steps in your workflows. Pick the model that " + m.headlessCLI + " should use for that work."
		prompt = modelSelectionPrompt(m.modelsLoading, m.loadingPhase, m.headlessCLI, "Choose the implementor model.")
	case stageScope:
		title = "Config Scope"
		body = "Choose where to save this profile and install agent skills. Global applies everywhere from ~/.agent-runner/config.yaml. Project applies only in the current repository via .agent-runner/config.yaml."
		prompt = "Where should Agent Runner save the profile and install skills?"
	case stageOverwrite:
		title = "Existing Agent Profiles"
		body = "These entries already exist and will be replaced: " + strings.Join(m.collisions, ", ")
		prompt = "Overwrite the existing entries?"
	case stagePluginIntro:
		title = "Agent Skills"
		body = "Agent Runner uses skills from https://github.com/Codagent-AI/agent-skills. These skills provide focused workflows for spec-driven development, from evaluating ideas and writing specs to planning right-sized tasks and implementing changes with TDD.\n\nThey also connect implementation work to Agent Validator quality gates and PR/CI follow-up skills, so agents can validate changes, fix failures, and shepherd work through review. Next you will select where Agent Runner installs these skills."
		prompt = "Continue to skill location."
	case stagePluginPreview:
		title = "Install Agent Skills"
		body, prompt = m.pluginPreviewContent()
	case stageDemoPrompt:
		title = "Agent Runner Workflow Demo"
		body = "Agent Runner includes a short interactive demo that walks through UI prompts, interactive agents, headless agents, shell commands, and data capture. It takes about two minutes and runs real workflow steps."
		prompt = "Run the demo now?"
	case stageDone:
		title, body = m.doneContent()
	default:
		title = "Set Up Agent Runner"
		body = ""
	}
	return title, body, prompt
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
