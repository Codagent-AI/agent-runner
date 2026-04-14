package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/tuistyle"
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
	currentDirOffset int

	projectDir   string
	projectsRoot string
	currentRuns  []runs.RunInfo
	loadErr      string
	pulsePhase   float64
	termWidth    int
	termHeight   int

	quitting bool
}

type worktreeTabState struct {
	subView      subView
	pickerCursor int
	pickerOffset int
	listCursor   int
	listOffset   int
	worktrees    []WorktreeEntry
	selectedDir  string
}

type allTabState struct {
	subView      subView
	pickerCursor int
	pickerOffset int
	listCursor   int
	listOffset   int
	dirs         []DirEntry
	selectedDir  string
}

// ViewRunMsg signals the switcher to open the run view for a specific run.
type ViewRunMsg struct {
	SessionDir string
	ProjectDir string
}

type refreshMsg = tuistyle.RefreshMsg
type pulseMsg = tuistyle.PulseMsg

var doRefresh = tuistyle.DoRefresh
var doPulse = tuistyle.DoPulse

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

	// Capture stable identity of the currently-highlighted item in each view
	// so we can re-anchor the numeric cursor after the underlying slices are
	// rebuilt and possibly reordered.
	prevCurrentSession := cursorSessionID(m.currentRuns, m.currentDirCursor)
	prevWorktreePickerPath := cursorWorktreePath(m.worktreeTab.worktrees, m.worktreeTab.pickerCursor)
	prevWorktreeRunSession := ""
	if wt := m.selectedWorktree(); wt != nil {
		prevWorktreeRunSession = cursorSessionID(wt.Runs, m.worktreeTab.listCursor)
	}
	prevAllPickerEncoded := cursorDirEncoded(m.allTab.dirs, m.allTab.pickerCursor)
	prevAllRunSession := ""
	if d := m.selectedAllDir(); d != nil {
		prevAllRunSession = cursorSessionID(d.Runs, m.allTab.listCursor)
	}

	currentRuns, err := runs.ListForDir(m.projectDir)
	if err != nil {
		errs = append(errs, fmt.Sprintf("current dir: %v", err))
	}
	m.currentRuns = currentRuns

	m.worktreeTab.worktrees = ListWorktrees(m.projectsRoot)
	allDirs, allErrs := listAllDirs(m.projectsRoot)
	m.allTab.dirs = allDirs
	errs = append(errs, allErrs...)

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

	// Re-anchor cursors using stable keys captured before the reload, then
	// clamp against the new slice lengths.
	m.currentDirCursor = reanchorRunCursor(m.currentRuns, prevCurrentSession, m.currentDirCursor)
	m.worktreeTab.pickerCursor = reanchorWorktreeCursor(m.worktreeTab.worktrees, prevWorktreePickerPath, m.worktreeTab.pickerCursor)
	if wt := m.selectedWorktree(); wt != nil {
		m.worktreeTab.listCursor = reanchorRunCursor(wt.Runs, prevWorktreeRunSession, m.worktreeTab.listCursor)
	}
	m.allTab.pickerCursor = reanchorDirCursor(m.allTab.dirs, prevAllPickerEncoded, m.allTab.pickerCursor)
	if d := m.selectedAllDir(); d != nil {
		m.allTab.listCursor = reanchorRunCursor(d.Runs, prevAllRunSession, m.allTab.listCursor)
	}

	if len(errs) > 0 {
		m.loadErr = strings.Join(errs, "; ")
	} else {
		m.loadErr = ""
	}
}

func cursorSessionID(runList []runs.RunInfo, cursor int) string {
	if cursor < 0 || cursor >= len(runList) {
		return ""
	}
	return runList[cursor].SessionID
}

func cursorWorktreePath(wts []WorktreeEntry, cursor int) string {
	if cursor < 0 || cursor >= len(wts) {
		return ""
	}
	return wts[cursor].Path
}

func cursorDirEncoded(dirs []DirEntry, cursor int) string {
	if cursor < 0 || cursor >= len(dirs) {
		return ""
	}
	return dirs[cursor].Encoded
}

func reanchorRunCursor(runList []runs.RunInfo, key string, fallback int) int {
	if key != "" {
		for i := range runList {
			if runList[i].SessionID == key {
				return i
			}
		}
	}
	return clampCursor(fallback, len(runList))
}

func reanchorWorktreeCursor(wts []WorktreeEntry, key string, fallback int) int {
	if key != "" {
		for i := range wts {
			if wts[i].Path == key {
				return i
			}
		}
	}
	return clampCursor(fallback, len(wts))
}

func reanchorDirCursor(dirs []DirEntry, key string, fallback int) int {
	if key != "" {
		for i := range dirs {
			if dirs[i].Encoded == key {
				return i
			}
		}
	}
	return clampCursor(fallback, len(dirs))
}

func listAllDirs(projectsRoot string) (dirs []DirEntry, errs []string) {
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("projects root: %v", err)}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(projectsRoot, entry.Name())
		runList, listErr := runs.ListForDir(projectDir)
		if listErr != nil {
			errs = append(errs, fmt.Sprintf("dir %s: %v", entry.Name(), listErr))
		}
		path := runs.ReadProjectPath(projectDir)
		dirs = append(dirs, DirEntry{
			Path:    path,
			Encoded: entry.Name(),
			Runs:    runList,
		})
	}

	sort.SliceStable(dirs, func(i, j int) bool {
		ti, tj := mostRecentRun(dirs[i].Runs), mostRecentRun(dirs[j].Runs)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return dirs[i].Path < dirs[j].Path
	})
	return dirs, errs
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
		case "c":
			m.activeTab = tabCurrentDir
		case "w":
			if m.worktreeTab.worktrees != nil {
				m.activeTab = tabWorktrees
			}
		case "a":
			m.activeTab = tabAll

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
			return m, viewRunCmd(r.SessionDir, m.projectDir)
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
				projDir := filepath.Join(m.projectsRoot, wt.Encoded)
				return m, viewRunCmd(r.SessionDir, projDir)
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
				projDir := filepath.Join(m.projectsRoot, d.Encoded)
				return m, viewRunCmd(r.SessionDir, projDir)
			}
		}
	}
	return m, nil
}

func viewRunCmd(sessionDir, projectDir string) tea.Cmd {
	return func() tea.Msg {
		return ViewRunMsg{SessionDir: sessionDir, ProjectDir: projectDir}
	}
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
