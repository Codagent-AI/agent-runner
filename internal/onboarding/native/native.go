// Package native implements the mandatory first-run setup flow.
package native

import (
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
	Detector   AdapterDetector
	Models     ModelDiscoverer
	Profiles   ProfileWriter
	Collisions CollisionDetector
	Settings   SettingsStore
	Clock      func() time.Time
	HomeDir    func() (string, error)
	Cwd        func() (string, error)
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

type stage int

const (
	stageWelcome stage = iota
	stageInteractiveCLI
	stageInteractiveModel
	stageHeadlessCLI
	stageHeadlessModel
	stageScope
	stageConfirm
	stageOverwrite
	stageDone
)

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
	result           Result
	err              error
	terminal         bool
}

func NewModel(deps *Deps) *Model {
	deps = fillDefaults(deps)
	return &Model{deps: *deps, stage: stageWelcome, width: 80, options: []string{"Continue"}}
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
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancel()
			return m, tea.Quit
		case "up", "k", "left", "h":
			m.move(-1)
		case "down", "j", "right", "l", "tab":
			m.move(1)
		case "enter":
			if m.enter() {
				return m, tea.Quit
			}
		}
	}
	return m, nil
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
	case stageWelcome:
		return m.loadAdapters()
	case stageInteractiveCLI:
		m.interactiveCLI = selected
		return m.loadModels(stageInteractiveModel, selected)
	case stageInteractiveModel:
		m.interactiveModel = selected
		m.setStage(stageHeadlessCLI, m.adapters)
	case stageHeadlessCLI:
		m.headlessCLI = selected
		return m.loadModels(stageHeadlessModel, selected)
	case stageHeadlessModel:
		m.headlessModel = selected
		m.setStage(stageScope, []string{"global", "project"})
	case stageScope:
		m.scope = selected
		if err := m.resolveTarget(); err != nil {
			return m.fail(err)
		}
		m.setStage(stageConfirm, []string{"Confirm", "Cancel"})
	case stageConfirm:
		if selected == "Cancel" {
			m.cancel()
			return true
		}
		collisions, err := m.deps.Collisions.Collisions(m.targetPath)
		if err != nil {
			return m.fail(err)
		}
		m.collisions = collisions
		if len(collisions) > 0 {
			m.setStage(stageOverwrite, []string{"Overwrite", "Cancel"})
			return false
		}
		return m.write()
	case stageOverwrite:
		if selected == "Cancel" {
			m.cancel()
			return true
		}
		return m.write()
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
		return m.fail(fmt.Errorf("discover models for %s: %w", adapter, err))
	}
	if len(models) == 0 {
		if next == stageInteractiveModel {
			m.interactiveModel = ""
			m.setStage(stageHeadlessCLI, m.adapters)
			return false
		}
		m.headlessModel = ""
		m.setStage(stageScope, []string{"global", "project"})
		return false
	}
	m.setStage(next, models)
	return false
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
	m.result = ResultCompleted
	m.terminal = true
	m.stage = stageDone
	return true
}

func (m *Model) setStage(next stage, options []string) {
	m.stage = next
	m.options = append([]string(nil), options...)
	m.focus = 0
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
	var b strings.Builder
	title := "Set Up Agent Runner"
	body := "Choose the agent CLIs and models Agent Runner should use."
	switch m.stage {
	case stageInteractiveCLI:
		title = "Interactive Agent CLI"
		body = "Choose the CLI adapter for planning and conversational work."
	case stageInteractiveModel:
		title = "Interactive Agent Model"
		body = "Choose the model for " + m.interactiveCLI + "."
	case stageHeadlessCLI:
		title = "Headless Agent CLI"
		body = "Choose the CLI adapter for unattended implementation work."
	case stageHeadlessModel:
		title = "Headless Agent Model"
		body = "Choose the model for " + m.headlessCLI + "."
	case stageScope:
		title = "Config Scope"
		body = "Choose where to write the profile configuration."
	case stageConfirm:
		title = "Confirm Agent Profile"
		body = fmt.Sprintf("Write profiles to %s\n\ninteractive_base: %s / %s\nheadless_base: %s / %s\nplanner: extends interactive_base\nimplementor: extends headless_base", m.targetPath, m.interactiveCLI, defaultModelText(m.interactiveModel), m.headlessCLI, defaultModelText(m.headlessModel))
	case stageOverwrite:
		title = "Existing Agent Profiles"
		body = "These entries already exist and will be replaced: " + strings.Join(m.collisions, ", ")
	case stageDone:
		if m.err != nil {
			title = "Setup Failed"
			body = m.err.Error()
		} else {
			title = "Setup Complete"
			body = ""
		}
	}
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
	return b.String()
}

func defaultModelText(model string) string {
	if model == "" {
		return "adapter default"
	}
	return model
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
	out, err := exec.Command(adapter, args...).Output() // #nosec G204 -- adapter is selected from supported CLI names.
	if err != nil {
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
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
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
