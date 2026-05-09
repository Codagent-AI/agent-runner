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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"

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

type Deps struct {
	Detector            AdapterDetector
	Models              ModelDiscoverer
	Profiles            ProfileWriter
	Collisions          CollisionDetector
	Settings            SettingsStore
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
	stageInteractiveModel
	stageHeadlessCLI
	stageHeadlessModel
	stageScope
	stageOverwrite
	stageDemoPrompt
	stageDone
)

var (
	scopeOptions      = []string{"global", "project"}
	overwriteOptions  = []string{"Overwrite", "Cancel"}
	demoPromptOptions = []string{"Continue", "Not now", "Dismiss"}
)

const (
	minCenterWidth  = 80
	minCenterHeight = 24
)

type animTick struct{}

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

	spring   harmonica.Spring
	yOffset  float64
	yVel     float64
	animDone bool
}

func NewModel(deps *Deps) *Model {
	deps = fillDefaults(deps)
	m := &Model{
		deps:     *deps,
		width:    80,
		height:   24,
		spring:   harmonica.NewSpring(harmonica.FPS(60), 8.0, 0.8),
		animDone: true,
	}
	if !m.loadAdapters() {
		// adapters loaded, stage is now stageInteractiveCLI
	}
	return m
}

func NewDemoPromptModel(deps *Deps) *Model {
	deps = fillDefaults(deps)
	m := &Model{
		deps:     *deps,
		width:    80,
		height:   24,
		spring:   harmonica.NewSpring(harmonica.FPS(60), 8.0, 0.8),
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
		m.yOffset, m.yVel = m.spring.Update(m.yOffset, m.yVel, 0)
		if m.yOffset < 0.5 && m.yOffset > -0.5 {
			m.yOffset = 0
			m.yVel = 0
			m.animDone = true
			return m, nil
		}
		return m, m.tickAnim()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancel()
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j", "tab":
			m.move(1)
		case "enter":
			if m.enter() {
				return m, tea.Quit
			}
			if !m.animDone {
				return m, m.tickAnim()
			}
		}
	}
	return m, nil
}

func (m *Model) tickAnim() tea.Cmd {
	return tea.Tick(time.Second/60, func(time.Time) tea.Msg {
		return animTick{}
	})
}

func (m *Model) startAnim() {
	m.yOffset = float64(m.height)
	m.yVel = 0
	m.animDone = false
}

func (m *Model) Result() Result { return m.result }

func (m *Model) Err() error { return m.err }

func (m *Model) move(delta int) {
	if len(m.options) <= 1 {
		return
	}
	m.focus = (m.focus + delta + len(m.options)) % len(m.options)
}

func (m *Model) enter() bool {
	if m.terminal {
		return true
	}
	selected := ""
	if len(m.options) > 0 {
		selected = m.options[m.focus]
	}
	switch m.stage {
	case stageInteractiveCLI:
		m.interactiveCLI = selected
		return m.loadModels(stageInteractiveModel, selected)
	case stageInteractiveModel:
		m.interactiveModel = selected
		m.setStageAnimated(stageHeadlessCLI, m.adapters)
	case stageHeadlessCLI:
		m.headlessCLI = selected
		return m.loadModels(stageHeadlessModel, selected)
	case stageHeadlessModel:
		m.headlessModel = selected
		m.setStageAnimated(stageScope, scopeOptions)
	case stageScope:
		m.scope = selected
		if err := m.resolveTarget(); err != nil {
			return m.fail(err)
		}
		collisions, err := m.deps.Collisions.Collisions(m.targetPath)
		if err != nil {
			return m.fail(err)
		}
		m.collisions = collisions
		if len(collisions) > 0 {
			m.setStageAnimated(stageOverwrite, overwriteOptions)
			return false
		}
		return m.write()
	case stageOverwrite:
		if selected == "Cancel" {
			m.cancel()
			return true
		}
		return m.write()
	case stageDemoPrompt:
		return m.handleDemoPrompt(selected)
	}
	return false
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
		_ = m.deps.Settings.Update(func(settings usersettings.Settings) usersettings.Settings {
			settings.Onboarding.Dismissed = stamp
			return settings
		})
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

func (m *Model) loadModels(next stage, adapter string) bool {
	models, err := m.deps.Models.ModelsFor(adapter)
	if err != nil {
		models = nil
	}
	if len(models) == 0 {
		m.skipModelSelection(next)
		return false
	}
	m.setStageAnimated(next, models)
	return false
}

func (m *Model) skipModelSelection(next stage) {
	if next == stageInteractiveModel {
		m.interactiveModel = ""
		m.setStageAnimated(stageHeadlessCLI, m.adapters)
		return
	}
	m.headlessModel = ""
	m.setStageAnimated(stageScope, scopeOptions)
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

func (m *Model) write() bool {
	err := m.deps.Profiles.WriteProfile(&profilewrite.Request{
		TargetPath:       m.targetPath,
		InteractiveCLI:   m.interactiveCLI,
		InteractiveModel: m.interactiveModel,
		HeadlessCLI:      m.headlessCLI,
		HeadlessModel:    m.headlessModel,
	})
	if err != nil {
		return m.fail(err)
	}
	stamp := m.deps.Clock().UTC().Format(time.RFC3339)
	if err := m.deps.Settings.Update(func(settings usersettings.Settings) usersettings.Settings {
		settings.Setup.CompletedAt = stamp
		return settings
	}); err != nil {
		return m.fail(err)
	}

	if m.deps.OnboardingCompleted {
		m.result = ResultCompleted
		m.terminal = true
		m.stage = stageDone
		return true
	}

	m.setStageAnimated(stageDemoPrompt, demoPromptOptions)
	return false
}

func (m *Model) setStage(next stage, options []string) {
	m.stage = next
	m.options = append([]string(nil), options...)
	m.focus = 0
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

func (m *Model) fail(err error) bool {
	m.err = err
	m.result = ResultFailed
	m.terminal = true
	m.stage = stageDone
	return true
}

func (m *Model) View() string {
	title, body := m.screenContent()

	var b strings.Builder
	b.WriteString(tuistyle.SectionStyle.Render(title))
	b.WriteString("\n\n")
	if body != "" {
		b.WriteString(lipgloss.NewStyle().Width(max(40, m.width-4)).Render(tuistyle.NormalStyle.Render(body)))
		b.WriteString("\n\n")
	}
	for i, option := range m.options {
		prefix := "  "
		style := tuistyle.NormalStyle
		if i == m.focus {
			prefix = "> "
			style = tuistyle.AccentStyle
		}
		b.WriteString(style.Render(prefix + option))
		b.WriteByte('\n')
	}

	content := b.String()

	if m.width >= minCenterWidth && m.height >= minCenterHeight {
		yOff := int(m.yOffset)
		content = lipgloss.Place(m.width, m.height+yOff, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}

func (m *Model) screenContent() (title, body string) {
	switch m.stage {
	case stageInteractiveCLI:
		title = "Set Up Agent Runner"
		body = "Welcome! Let's configure the agent CLIs and models that Agent Runner will use.\n\nFirst, choose a CLI for the planner agent. The planner handles interactive work like planning, conversations, and decisions that need your input."
	case stageInteractiveModel:
		title = "Planner Model"
		body = "Choose the model for the planner agent (" + m.interactiveCLI + "). This model will be used for interactive planning and conversational steps in your workflows."
	case stageHeadlessCLI:
		title = "Implementor CLI"
		body = "Now choose a CLI for the implementor agent. The implementor handles headless tasks like code generation and implementation work that runs without interaction."
	case stageHeadlessModel:
		title = "Implementor Model"
		body = "Choose the model for the implementor agent (" + m.headlessCLI + "). This model will be used for unattended implementation steps in your workflows."
	case stageScope:
		title = "Config Scope"
		body = "Choose where to save your agent configuration.\n\nGlobal writes to ~/.agent-runner/config.yaml and applies everywhere. Project writes to .agent-runner/config.yaml in your current directory and applies only to this project."
	case stageOverwrite:
		title = "Existing Agent Profiles"
		body = "These entries already exist and will be replaced: " + strings.Join(m.collisions, ", ")
	case stageDemoPrompt:
		title = "Agent Runner Workflow Demo"
		body = "Agent Runner includes a short interactive demo that walks you through the different workflow step types — UI prompts, interactive agents, headless agents, shell commands, and data capture.\n\nIt takes about two minutes and runs real workflow steps so you can see how everything works together."
	case stageDone:
		switch {
		case m.result == ResultCancelled:
			title = "Setup Cancelled"
			body = ""
		case m.err != nil:
			title = "Setup Failed"
			body = m.err.Error()
		default:
			title = "Setup Complete"
			body = ""
		}
	default:
		title = "Set Up Agent Runner"
		body = ""
	}
	return title, body
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
		args = []string{"models", "list"}
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
