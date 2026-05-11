package listview

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/tuistyle"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

type tab int

const (
	tabNew tab = iota // must be first; bare invocation lands here
	tabCurrentDir
	tabWorktrees
	tabAll
)

// InitialTab is an exported type for selecting the starting tab via WithInitialTab.
type InitialTab int

const (
	InitialTabNew        InitialTab = InitialTab(tabNew)
	InitialTabCurrentDir InitialTab = InitialTab(tabCurrentDir)
)

// WithInitialTab returns an option that sets the starting tab for New().
func WithInitialTab(t InitialTab) func(*Model) {
	return func(m *Model) { m.activeTab = tab(t) }
}

// newTabState holds all state for the "new" tab (workflow browser + search).
type newTabState struct {
	workflows     []discovery.WorkflowEntry
	filtered      []int // indices into workflows (-1 = blank-line separator)
	cursor        int   // index into filtered of the selected row
	offset        int   // scroll offset
	searchText    string
	searchFocused bool // true when search box has focus
}

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
	newTab           newTabState
	worktreeTab      worktreeTabState
	allTab           allTabState
	currentDirCursor int
	currentDirOffset int

	projectDir     string
	projectsRoot   string
	cwd            string
	currentRuns    []runs.RunInfo
	loadErr        string
	errMsg         string
	pulsePhase     float64
	pulseScheduled bool
	termWidth      int
	termHeight     int

	quitting bool
}

type worktreeTabState struct {
	subView      subView
	pickerCursor int
	pickerOffset int
	listCursor   int
	listOffset   int
	worktrees    []WorktreeEntry
	repoName     string
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

// ResumeRunMsg asks the shell to exit the TUI and exec `agent-runner --resume
// <run-id>`, resuming an interrupted workflow run. RunID is the session
// directory name (same semantics as runview.ResumeRunMsg). ProjectDir is the
// original cwd of the run's project; the caller must chdir there before
// re-exec so that resolveResumeStatePath looks in the correct project tree.
type ResumeRunMsg struct {
	RunID      string
	ProjectDir string
}

type refreshMsg = tuistyle.RefreshMsg
type pulseMsg = tuistyle.PulseMsg

var doRefresh = tuistyle.DoRefresh
var doPulse = tuistyle.DoPulse

// New creates a new Model. Functional options (e.g. WithInitialTab) may be
// passed to override defaults. The default starting tab is tabNew.
func New(opts ...func(*Model)) (*Model, error) {
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
	userWorkflowsDir := filepath.Join(home, ".agent-runner", "workflows")

	workflows := discovery.Enumerate(builtinworkflows.FS, cwd, userWorkflowsDir)

	m := &Model{
		activeTab:    tabNew,
		projectDir:   projectDir,
		projectsRoot: projectsRoot,
		cwd:          cwd,
	}
	m.newTab.workflows = workflows
	m.newTab.filtered = buildFilteredRows(workflows, "")
	m.newTab.cursor = firstSelectableRow(m.newTab.filtered)

	for _, opt := range opts {
		opt(m)
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

	m.worktreeTab.repoName, m.worktreeTab.worktrees = ListWorktrees(m.projectsRoot)
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
	return tea.Batch(doRefresh(), m.schedulePulseIfNeeded())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height

	case tea.KeyMsg:
		m.errMsg = "" // auto-clear inline error on any keypress

		// When the new tab is active and the search box has focus, most keys
		// go to the filter text. Global quit and tab-switching still work.
		if m.activeTab == tabNew && m.newTab.searchFocused {
			return m.handleSearchKey(msg)
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "tab", "right":
			m.nextTab()
		case "shift+tab", "left":
			m.prevTab()
		case "n":
			m.activeTab = tabNew
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

		case "r":
			return m.handleResumeRun()

		case "?":
			return m.handleHelpRun()

		case "esc":
			m.handleEsc()
		}
		cmd := m.schedulePulseIfNeeded()
		return m, cmd

	case refreshMsg:
		m.loadData()
		return m, tea.Batch(doRefresh(), m.schedulePulseIfNeeded())

	case pulseMsg:
		m.pulseScheduled = false
		if !m.visibleRunListHasActiveRun() {
			return m, nil
		}
		m.pulsePhase += (50.0 / 1000.0) * 2 * math.Pi
		cmd := m.schedulePulseIfNeeded()
		return m, cmd
	}

	return m, nil
}

func (m *Model) schedulePulseIfNeeded() tea.Cmd {
	if m.pulseScheduled || !m.visibleRunListHasActiveRun() {
		return nil
	}
	m.pulseScheduled = true
	return doPulse()
}

func (m *Model) visibleRunListHasActiveRun() bool {
	switch m.activeTab {
	case tabCurrentDir:
		return runListHasActiveRun(m.currentRuns)
	case tabWorktrees:
		if m.worktreeTab.subView == subViewRunList {
			if wt := m.selectedWorktree(); wt != nil {
				return runListHasActiveRun(wt.Runs)
			}
		}
	case tabAll:
		if m.allTab.subView == subViewRunList {
			if d := m.selectedAllDir(); d != nil {
				return runListHasActiveRun(d.Runs)
			}
		}
	}
	return false
}

func runListHasActiveRun(runList []runs.RunInfo) bool {
	for i := range runList {
		if runList[i].Status == runs.StatusActive {
			return true
		}
	}
	return false
}

func (m *Model) nextTab() {
	if m.worktreeTab.worktrees != nil {
		m.activeTab = (m.activeTab + 1) % 4
	} else {
		// Cycle: tabNew → tabCurrentDir → tabAll → tabNew
		switch m.activeTab {
		case tabNew:
			m.activeTab = tabCurrentDir
		case tabCurrentDir:
			m.activeTab = tabAll
		default:
			m.activeTab = tabNew
		}
	}
}

func (m *Model) prevTab() {
	if m.worktreeTab.worktrees != nil {
		m.activeTab = (m.activeTab + 3) % 4
	} else {
		switch m.activeTab {
		case tabNew:
			m.activeTab = tabAll
		case tabAll:
			m.activeTab = tabCurrentDir
		default:
			m.activeTab = tabNew
		}
	}
}

func (m *Model) moveCursor(delta int) {
	switch m.activeTab {
	case tabNew:
		m.moveNewTabCursor(delta)
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

// moveNewTabCursor navigates the new tab's cursor, skipping blank-line separators.
// Moving up from the first item focuses the search box.
// Moving down from the search box moves to the first selectable item.
func (m *Model) moveNewTabCursor(delta int) {
	if m.newTab.searchFocused {
		if delta > 0 {
			m.newTab.searchFocused = false
			m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
		}
		return
	}

	filtered := m.newTab.filtered
	if len(filtered) == 0 {
		return
	}

	// Moving up from the first selectable row focuses the search box.
	if delta < 0 {
		first := firstSelectableRow(filtered)
		if m.newTab.cursor <= first {
			m.newTab.searchFocused = true
			return
		}
	}

	// Advance and skip separators.
	pos := m.newTab.cursor + delta
	for pos >= 0 && pos < len(filtered) && filtered[pos] == -1 {
		if delta > 0 {
			pos++
		} else {
			pos--
		}
	}
	if pos >= 0 && pos < len(filtered) && filtered[pos] != -1 {
		m.newTab.cursor = pos
	}
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.activeTab {
	case tabNew:
		cmd := m.newTabEnterCmd()
		return m, cmd
	case tabCurrentDir:
		if m.currentDirCursor < len(m.currentRuns) {
			r := m.currentRuns[m.currentDirCursor]
			if runlock.CheckOwnedByOther(r.SessionDir, os.Getpid()) {
				m.errMsg = "run is active in another process"
				return m, nil
			}
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
				if runlock.CheckOwnedByOther(r.SessionDir, os.Getpid()) {
					m.errMsg = "run is active in another process"
					return m, nil
				}
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
				if runlock.CheckOwnedByOther(r.SessionDir, os.Getpid()) {
					m.errMsg = "run is active in another process"
					return m, nil
				}
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

func (m *Model) handleResumeRun() (tea.Model, tea.Cmd) {
	if m.activeTab == tabNew {
		cmd := m.newTabStartRunCmd()
		return m, cmd
	}
	r := m.cursorInactiveRun()
	if r == nil {
		return m, nil
	}
	runID := r.SessionID
	projectDir := m.cursorProjectDir()
	return m, func() tea.Msg { return ResumeRunMsg{RunID: runID, ProjectDir: projectDir} }
}

func (m *Model) handleHelpRun() (tea.Model, tea.Cmd) {
	const canonicalName = "onboarding:help"
	for _, e := range m.newTab.workflows {
		if e.CanonicalName == canonicalName {
			entry := e
			return m, func() tea.Msg { return discovery.StartRunMsg{Entry: entry} }
		}
	}
	m.errMsg = fmt.Sprintf("cannot start help workflow: %q not found", canonicalName)
	return m, nil
}

// cursorProjectDir returns the original cwd (project directory) for the run
// currently under the cursor. This is the directory the caller must be in for
// resolveResumeStatePath to locate the run's state file.
func (m *Model) cursorProjectDir() string {
	switch m.activeTab {
	case tabCurrentDir:
		return m.cwd
	case tabWorktrees:
		if m.worktreeTab.subView == subViewRunList {
			if wt := m.selectedWorktree(); wt != nil {
				return wt.Path
			}
		}
	case tabAll:
		if m.allTab.subView == subViewRunList {
			if d := m.selectedAllDir(); d != nil {
				return d.Path
			}
		}
	}
	return ""
}

// cursorInactiveRun returns the run under the cursor when it is inactive,
// or nil if the cursor is not on an inactive run or is on a picker sub-view.
func (m *Model) cursorInactiveRun() *runs.RunInfo {
	switch m.activeTab {
	case tabCurrentDir:
		if m.currentDirCursor < len(m.currentRuns) {
			r := &m.currentRuns[m.currentDirCursor]
			if r.Status == runs.StatusInactive {
				return r
			}
		}
	case tabWorktrees:
		if m.worktreeTab.subView == subViewRunList {
			wt := m.selectedWorktree()
			if wt != nil && m.worktreeTab.listCursor < len(wt.Runs) {
				r := &wt.Runs[m.worktreeTab.listCursor]
				if r.Status == runs.StatusInactive {
					return r
				}
			}
		}
	case tabAll:
		if m.allTab.subView == subViewRunList {
			d := m.selectedAllDir()
			if d != nil && m.allTab.listCursor < len(d.Runs) {
				r := &d.Runs[m.allTab.listCursor]
				if r.Status == runs.StatusInactive {
					return r
				}
			}
		}
	}
	return nil
}

func (m *Model) handleEsc() {
	switch m.activeTab {
	case tabNew:
		if m.newTab.searchText != "" {
			m.newTab.searchText = ""
			m.newTab.filtered = buildFilteredRows(m.newTab.workflows, "")
			m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
			m.newTab.searchFocused = true
		}
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
