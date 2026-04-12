package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/runs"
)

type tab int

const (
	tabCurrentDir tab = iota
	tabWorktrees
	tabAll
)

type subView int

const (
	subViewPicker subView = iota
	subViewRunList
)

// DirEntry represents a project directory in the All tab.
type DirEntry struct {
	Path    string
	Encoded string
	Runs    []runs.RunInfo
}

// Model is the bubbletea model for the run list TUI.
type Model struct {
	activeTab        tab
	worktreeTab      worktreeTabState
	allTab           allTabState
	currentDirCursor int

	projectDir   string
	projectsRoot string
	currentRuns  []runs.RunInfo
	loadErr      string
	pulsePhase   float64
	termWidth    int
	termHeight   int

	selected *runs.RunInfo
	quitting bool
}

type worktreeTabState struct {
	subView      subView
	pickerCursor int
	listCursor   int
	worktrees    []WorktreeEntry
	selectedDir  string
}

type allTabState struct {
	subView      subView
	pickerCursor int
	listCursor   int
	dirs         []DirEntry
	selectedDir  string
}

type refreshMsg struct{}
type pulseMsg struct{}

func doRefresh() tea.Cmd {
	return tea.Every(2*time.Second, func(t time.Time) tea.Msg {
		return refreshMsg{}
	})
}

func doPulse() tea.Cmd {
	return tea.Every(50*time.Millisecond, func(t time.Time) tea.Msg {
		return pulseMsg{}
	})
}

// SelectedRun returns the run the user chose to resume, or nil.
func (m *Model) SelectedRun() *runs.RunInfo {
	return m.selected
}

// New creates a new Model.
func New() (*Model, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot determine working directory: %w", err)
	}

	projectsRoot := filepath.Join(home, ".agent-runner", "projects")
	encoded := audit.EncodePath(cwd)
	projectDir := filepath.Join(projectsRoot, encoded)

	m := &Model{
		projectDir:   projectDir,
		projectsRoot: projectsRoot,
	}
	m.loadData()
	return m, nil
}

func (m *Model) loadData() {
	var errs []string

	currentRuns, err := runs.ListForDir(m.projectDir)
	if err != nil {
		errs = append(errs, fmt.Sprintf("current dir: %v", err))
	}
	m.currentRuns = currentRuns

	m.worktreeTab.worktrees = ListWorktrees(m.projectsRoot)
	m.allTab.dirs = listAllDirs(m.projectsRoot)

	if m.worktreeTab.subView == subViewRunList && m.worktreeTab.selectedDir != "" {
		for i, wt := range m.worktreeTab.worktrees {
			if wt.Path == m.worktreeTab.selectedDir {
				wtRuns, wtErr := runs.ListForDir(filepath.Join(m.projectsRoot, wt.Encoded))
				if wtErr != nil {
					errs = append(errs, fmt.Sprintf("worktree %s: %v", wt.Name, wtErr))
				}
				m.worktreeTab.worktrees[i].Runs = wtRuns
				break
			}
		}
	}

	if m.allTab.subView == subViewRunList && m.allTab.selectedDir != "" {
		for i, d := range m.allTab.dirs {
			if d.Encoded == m.allTab.selectedDir {
				dirRuns, dirErr := runs.ListForDir(filepath.Join(m.projectsRoot, d.Encoded))
				if dirErr != nil {
					errs = append(errs, fmt.Sprintf("dir %s: %v", d.Path, dirErr))
				}
				m.allTab.dirs[i].Runs = dirRuns
				break
			}
		}
	}

	if len(errs) > 0 {
		m.loadErr = strings.Join(errs, "; ")
	} else {
		m.loadErr = ""
	}
}

func listAllDirs(projectsRoot string) []DirEntry {
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return nil
	}

	var dirs []DirEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(projectsRoot, entry.Name())
		runList, _ := runs.ListForDir(projectDir)
		path := runs.ReadProjectPath(projectDir)
		dirs = append(dirs, DirEntry{
			Path:    path,
			Encoded: entry.Name(),
			Runs:    runList,
		})
	}
	return dirs
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(doRefresh(), doPulse())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "tab":
			m.nextTab()
		case "shift+tab":
			m.prevTab()

		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)

		case "enter":
			return m.handleEnter()

		case "esc":
			m.handleEsc()
		}

	case refreshMsg:
		m.loadData()
		return m, doRefresh()

	case pulseMsg:
		m.pulsePhase += (50.0 / 1000.0) * 2 * math.Pi
		return m, doPulse()
	}

	return m, nil
}

func (m *Model) nextTab() {
	if m.worktreeTab.worktrees != nil {
		m.activeTab = (m.activeTab + 1) % 3
	} else {
		if m.activeTab == tabCurrentDir {
			m.activeTab = tabAll
		} else {
			m.activeTab = tabCurrentDir
		}
	}
}

func (m *Model) prevTab() {
	if m.worktreeTab.worktrees != nil {
		m.activeTab = (m.activeTab + 2) % 3
	} else {
		if m.activeTab == tabCurrentDir {
			m.activeTab = tabAll
		} else {
			m.activeTab = tabCurrentDir
		}
	}
}

func (m *Model) moveCursor(delta int) {
	switch m.activeTab {
	case tabCurrentDir:
		m.currentDirCursor = clampCursor(m.currentDirCursor+delta, len(m.currentRuns))
	case tabWorktrees:
		if m.worktreeTab.subView == subViewPicker {
			m.worktreeTab.pickerCursor = clampCursor(m.worktreeTab.pickerCursor+delta, len(m.worktreeTab.worktrees))
		} else {
			wt := m.selectedWorktree()
			if wt != nil {
				m.worktreeTab.listCursor = clampCursor(m.worktreeTab.listCursor+delta, len(wt.Runs))
			}
		}
	case tabAll:
		if m.allTab.subView == subViewPicker {
			m.allTab.pickerCursor = clampCursor(m.allTab.pickerCursor+delta, len(m.allTab.dirs))
		} else {
			d := m.selectedAllDir()
			if d != nil {
				m.allTab.listCursor = clampCursor(m.allTab.listCursor+delta, len(d.Runs))
			}
		}
	}
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.activeTab {
	case tabCurrentDir:
		if m.currentDirCursor < len(m.currentRuns) {
			r := m.currentRuns[m.currentDirCursor]
			if r.Status == runs.StatusInactive {
				m.selected = &r
				m.quitting = true
				return m, tea.Quit
			}
		}
	case tabWorktrees:
		if m.worktreeTab.subView == subViewPicker {
			if m.worktreeTab.pickerCursor < len(m.worktreeTab.worktrees) {
				wt := m.worktreeTab.worktrees[m.worktreeTab.pickerCursor]
				m.worktreeTab.selectedDir = wt.Path
				m.worktreeTab.subView = subViewRunList
				m.worktreeTab.listCursor = 0
			}
		} else {
			wt := m.selectedWorktree()
			if wt != nil && m.worktreeTab.listCursor < len(wt.Runs) {
				r := wt.Runs[m.worktreeTab.listCursor]
				if r.Status == runs.StatusInactive {
					m.selected = &r
					m.quitting = true
					return m, tea.Quit
				}
			}
		}
	case tabAll:
		if m.allTab.subView == subViewPicker {
			if m.allTab.pickerCursor < len(m.allTab.dirs) {
				d := m.allTab.dirs[m.allTab.pickerCursor]
				m.allTab.selectedDir = d.Encoded
				m.allTab.subView = subViewRunList
				m.allTab.listCursor = 0
			}
		} else {
			d := m.selectedAllDir()
			if d != nil && m.allTab.listCursor < len(d.Runs) {
				r := d.Runs[m.allTab.listCursor]
				if r.Status == runs.StatusInactive {
					m.selected = &r
					m.quitting = true
					return m, tea.Quit
				}
			}
		}
	}
	return m, nil
}

func (m *Model) handleEsc() {
	switch m.activeTab {
	case tabWorktrees:
		if m.worktreeTab.subView == subViewRunList {
			m.worktreeTab.subView = subViewPicker
		}
	case tabAll:
		if m.allTab.subView == subViewRunList {
			m.allTab.subView = subViewPicker
		}
	}
}

func (m *Model) selectedWorktree() *WorktreeEntry {
	for i := range m.worktreeTab.worktrees {
		if m.worktreeTab.worktrees[i].Path == m.worktreeTab.selectedDir {
			return &m.worktreeTab.worktrees[i]
		}
	}
	return nil
}

func (m *Model) selectedAllDir() *DirEntry {
	for i := range m.allTab.dirs {
		if m.allTab.dirs[i].Encoded == m.allTab.selectedDir {
			return &m.allTab.dirs[i]
		}
	}
	return nil
}

func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString("\n  ")
	b.WriteString(headerStyle.Render("Agent Runner"))
	b.WriteString("\n\n")
	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")
	b.WriteString(m.renderSubheader())
	b.WriteString("\n\n")
	b.WriteString(m.renderBody())

	if m.loadErr != "" {
		b.WriteString("\n")
		b.WriteString("  " + dimStyle.Render("Error loading runs: "+sanitize(m.loadErr)))
	}

	b.WriteString("\n\n\n\n")
	b.WriteString(m.renderHelp())
	b.WriteString("\n")

	return b.String()
}

func (m *Model) renderTabs() string {
	var parts []string

	renderTab := func(label string, t tab) string {
		if m.activeTab == t {
			return activeTabStyle.Render("● " + label)
		}
		return dimTabStyle.Render("○ " + label)
	}

	parts = append(parts, renderTab("Current Dir", tabCurrentDir))
	if m.worktreeTab.worktrees != nil {
		parts = append(parts, renderTab("Worktrees", tabWorktrees))
	}
	parts = append(parts, renderTab("All", tabAll))

	return "  " + strings.Join(parts, "    ")
}

func (m *Model) renderSubheader() string {
	switch m.activeTab {
	case tabCurrentDir:
		cwd, _ := os.Getwd()
		return "  " + pathStyle.Render(sanitize(shortenPath(cwd)))
	case tabWorktrees:
		if m.worktreeTab.subView == subViewRunList {
			wt := m.selectedWorktree()
			if wt != nil {
				return "  " + dimStyle.Render("← Worktrees") + dimStyle.Render("  /  ") + normalStyle.Render(sanitize(wt.Name))
			}
		}
		cwd, _ := os.Getwd()
		return "  " + pathStyle.Render(sanitize(shortenPath(cwd))+"  (git repo)")
	case tabAll:
		if m.allTab.subView == subViewRunList {
			d := m.selectedAllDir()
			if d != nil {
				return "  " + dimStyle.Render("← All") + dimStyle.Render("  /  ") + normalStyle.Render(sanitize(shortenPath(d.Path)))
			}
		}
		return "  " + pathStyle.Render("All project directories")
	}
	return ""
}

func (m *Model) renderBody() string {
	switch m.activeTab {
	case tabCurrentDir:
		if len(m.currentRuns) == 0 {
			return m.renderEmpty()
		}
		return m.renderRunList(m.currentRuns, m.currentDirCursor)
	case tabWorktrees:
		if m.worktreeTab.subView == subViewPicker {
			return m.renderWorktreePicker()
		}
		wt := m.selectedWorktree()
		if wt == nil || len(wt.Runs) == 0 {
			return m.renderEmpty()
		}
		return m.renderRunList(wt.Runs, m.worktreeTab.listCursor)
	case tabAll:
		if m.allTab.subView == subViewPicker {
			return m.renderAllPicker()
		}
		d := m.selectedAllDir()
		if d == nil || len(d.Runs) == 0 {
			return m.renderEmpty()
		}
		return m.renderRunList(d.Runs, m.allTab.listCursor)
	}
	return ""
}

func (m *Model) renderEmpty() string {
	return "\n" +
		dimStyle.Render("               No runs found for this directory.") + "\n\n" +
		dimStyle.Render("               Press tab to view other scopes.")
}

func (m *Model) renderRunList(runList []runs.RunInfo, cursor int) string {
	var b strings.Builder
	for i, r := range runList {
		isSel := i == cursor
		prefix := "   "
		if isSel {
			prefix = cursorStyle.Render("▶") + "  "
		}

		workflow := sanitize(truncate(r.WorkflowName, 18))
		step := r.CurrentStep
		if step == "" {
			step = "—"
		}
		step = sanitize(truncate(step, 16))

		statusIcon := m.renderStatusIcon(r.Status)
		ts := formatTime(r.StartTime)

		style := dimStyle
		if isSel {
			style = selectedStyle
		}

		line := fmt.Sprintf("%-18s  %-16s  %s  %s",
			style.Render(workflow),
			style.Render(step),
			statusIcon,
			dimStyle.Render(ts),
		)
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (m *Model) renderStatusIcon(s runs.Status) string {
	switch s {
	case runs.StatusActive:
		t := (math.Sin(m.pulsePhase) + 1) / 2
		c := lerpColor("#4ade80", "#2d8f57", t)
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
	case runs.StatusInactive:
		return statusInactive.Render("○")
	case runs.StatusCompleted:
		return statusDone.Render("✓")
	}
	return " "
}

func (m *Model) renderWorktreePicker() string {
	var b strings.Builder
	for i, wt := range m.worktreeTab.worktrees {
		isSel := i == m.worktreeTab.pickerCursor
		prefix := "   "
		if isSel {
			prefix = cursorStyle.Render("▶") + "  "
		}

		style := dimStyle
		if isSel {
			style = selectedStyle
		}

		summary := runSummary(wt.Runs)
		name := sanitize(truncate(wt.Name, 14))
		path := sanitize(shortenPath(wt.Path))

		line := fmt.Sprintf("%-14s  %-40s  %s",
			style.Render(name),
			dimStyle.Render(truncate(path, 40)),
			dimStyle.Render(summary),
		)
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (m *Model) renderAllPicker() string {
	var b strings.Builder
	for i, d := range m.allTab.dirs {
		isSel := i == m.allTab.pickerCursor
		prefix := "   "
		if isSel {
			prefix = cursorStyle.Render("▶") + "  "
		}

		style := dimStyle
		if isSel {
			style = selectedStyle
		}

		summary := runSummary(d.Runs)
		path := sanitize(shortenPath(d.Path))

		line := fmt.Sprintf("%-50s  %s",
			style.Render(truncate(path, 50)),
			dimStyle.Render(summary),
		)
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (m *Model) renderHelp() string {
	var parts []string

	switch m.activeTab {
	case tabCurrentDir:
		if len(m.currentRuns) > 0 {
			parts = append(parts, "↑↓ navigate", "enter resume", "tab switch tab", "q quit")
		} else {
			parts = append(parts, "tab switch tab", "q quit")
		}
	case tabWorktrees:
		if m.worktreeTab.subView == subViewPicker {
			parts = append(parts, "↑↓ navigate", "enter view runs", "tab switch tab", "q quit")
		} else {
			parts = append(parts, "↑↓ navigate", "enter resume", "esc back", "tab switch tab", "q quit")
		}
	case tabAll:
		if m.allTab.subView == subViewPicker {
			parts = append(parts, "↑↓ navigate", "enter view runs", "tab switch tab", "q quit")
		} else {
			parts = append(parts, "↑↓ navigate", "enter resume", "esc back", "tab switch tab", "q quit")
		}
	}

	return "  " + helpStyle.Render(strings.Join(parts, "   "))
}

// Helpers

func clampCursor(v, limit int) int {
	if limit <= 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}

func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func truncate(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width-1, "…")
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04") + " today"
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02")
	}
	return t.Format("Jan 02 2006")
}

func runSummary(runList []runs.RunInfo) string {
	total := len(runList)
	if total == 0 {
		return "no runs"
	}

	active := 0
	for _, r := range runList {
		if r.Status == runs.StatusActive {
			active++
		}
	}

	label := "runs"
	if total == 1 {
		label = "run"
	}

	if active > 0 {
		return fmt.Sprintf("%d %s  ● %d active", total, label, active)
	}
	return fmt.Sprintf("%d %s", total, label)
}

func lerpColor(hex1, hex2 string, t float64) string {
	r1, g1, b1 := parseHex(hex1)
	r2, g2, b2 := parseHex(hex2)

	r := uint8(float64(r1) + t*(float64(r2)-float64(r1)))
	g := uint8(float64(g1) + t*(float64(g2)-float64(g1)))
	b := uint8(float64(b1) + t*(float64(b2)-float64(b1)))

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func parseHex(hex string) (r, g, b uint8) {
	hex = strings.TrimPrefix(hex, "#")
	_, _ = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\][^\x1b]*\x1b\\`)

func sanitize(s string) string {
	s = ansiEscapeRe.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || (unicode.IsPrint(r) && !unicode.Is(unicode.Co, r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
