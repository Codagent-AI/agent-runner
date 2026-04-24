package runview

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

// Messages emitted by the runview Model to the parent switcher.
type BackMsg struct{}

// ResumeMsg asks the shell to exit the TUI and exec the step's agent CLI
// with `--resume <session-id>`, resuming that agent's own conversation.
// AgentCLI is the binary name captured from the step's audit (e.g. "claude").
// SessionID is the CLI's own session ID, NOT an agent-runner run ID.
type ResumeMsg struct {
	AgentCLI  string
	SessionID string
}

// ResumeRunMsg asks the shell to exit the TUI and exec `agent-runner --resume
// <run-id>`, resuming the interrupted workflow run itself. RunID is the
// agent-runner run ID (the session directory name), NOT an agent CLI session ID.
type ResumeRunMsg struct {
	RunID string
}

type ExitMsg struct{}

// Entered describes how the user reached the run view.
type Entered int

const (
	FromList       Entered = iota
	FromInspect            // read-only post-run inspection
	FromLiveRun            // live workflow execution (runner goroutine is active)
	FromDefinition         // viewing a workflow definition with no run instance
)

// Model is the bubbletea model for the single-run detail view.
type Model struct {
	tree       *Tree
	tailer     FileTailer
	sessionDir string
	projectDir string
	entered    Entered

	path      []*StepNode
	cursor    int
	logOffset int
	// logLineCount is the total number of log lines from the last rebuildRanges call.
	// It is used to compute maxLogOffset for clamping and scroll normalization.
	logLineCount int

	loadedFull map[string]bool

	stepRanges []stepLineRange
	logAnchor  stepLineAnchor

	active     bool
	pulsePhase float64
	termWidth  int
	termHeight int
	showLegend bool
	loadErr    string
	notice     string // transient message shown below the step list (e.g. spawn error)

	resolverCfg   ResolverConfig
	startTime     time.Time
	workflowEntry discovery.WorkflowEntry // set when entered == FromDefinition

	// Live-run fields (FromLiveRun mode only).
	running          bool   // true until ExecDoneMsg arrives
	quitConfirming   bool   // quit-confirmation modal is visible
	liveResult       string // set on ExecDoneMsg ("success"/"failed"/"stopped")
	autoFollow       bool   // cursor tracks activeStep; enabled by default in FromLiveRun
	activeStepPrefix string // last known active step prefix from StepStateMsg

	// Resume-exec state. When the user selects an agent step after the live
	// run completes, the Model is the top-level tea.Program — there's no
	// switcher to intercept ResumeMsg — so we stash the info here and quit.
	// The CLI wrapper reads it via ResumeAgentCLI/ResumeSessionID after
	// p.Run() returns and execs the agent CLI.
	resumeAgentCLI  string
	resumeSessionID string

	// Alt-screen management. When the program starts without tea.WithAltScreen
	// (FromLiveRun mode), alt-screen entry is deferred so a fast non-interactive
	// step followed by an interactive step does not flash the TUI.
	altScreen         bool // true once alt-screen has been entered
	suppressAltScreen bool // set when SuspendedMsg arrives before the deferred timer
}

// ResumeAgentCLI returns the agent CLI name captured from a ResumeMsg in
// live-run mode. Empty when no resume was requested.
func (m *Model) ResumeAgentCLI() string { return m.resumeAgentCLI }

// ResumeSessionID returns the agent CLI session ID captured from a ResumeMsg
// in live-run mode. Empty when no resume was requested.
func (m *Model) ResumeSessionID() string { return m.resumeSessionID }

// SessionDir returns the session directory the Model was constructed for.
func (m *Model) SessionDir() string { return m.sessionDir }

// ProjectDir returns the project directory the Model was constructed for.
func (m *Model) ProjectDir() string { return m.projectDir }

// Entered returns the entry path used to construct the Model.
func (m *Model) Entered() Entered { return m.entered }

// New constructs a runview Model from a session directory.
// For FromDefinition mode, sessionDir carries the workflow file path rather than
// a real session directory; audit log loading and run-lock checks are skipped.
func New(sessionDir, projectDir string, entered Entered) (*Model, error) {
	// FromDefinition: load workflow directly from the file path in sessionDir.
	if entered == FromDefinition {
		e := discovery.WorkflowEntry{SourcePath: sessionDir}
		return newForDefinition(sessionDir, projectDir, &e)
	}

	state, _ := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	resolved, _ := ResolveWorkflow(sessionDir, projectDir, &state)

	var (
		tree    *Tree
		loadErr string
	)
	if resolved.AbsPath != "" {
		wf, err := loader.LoadWorkflow(resolved.AbsPath, loader.Options{})
		if err != nil {
			loadErr = "load workflow: " + err.Error()
		} else {
			tree = BuildTree(&wf, resolved.AbsPath)
		}
	} else if state.WorkflowFile != "" || state.WorkflowName != "" {
		loadErr = "workflow file not found (state: " + describeWorkflowHint(&state, sessionDir) + ")"
	}
	if tree == nil {
		rootName := state.WorkflowName
		if rootName == "" {
			rootName = parseWorkflowNameFromID(filepath.Base(sessionDir))
		}
		if rootName == "" {
			rootName = filepath.Base(sessionDir)
		}
		tree = &Tree{
			Root: &StepNode{
				ID:     rootName,
				Type:   NodeRoot,
				Status: StatusPending,
			},
		}
	}

	m := &Model{
		tree:       tree,
		sessionDir: sessionDir,
		projectDir: projectDir,
		entered:    entered,
		path:       []*StepNode{tree.Root},
		loadedFull: make(map[string]bool),
		loadErr:    loadErr,
		running:    entered == FromLiveRun,
		autoFollow: entered == FromLiveRun,
		altScreen:  entered != FromLiveRun,
	}

	if entered != FromLiveRun {
		m.active = runlock.Check(sessionDir) == runlock.LockActive
	}

	m.resolverCfg = ResolverConfig{
		WorkflowsRoot: resolved.WorkflowsRoot,
		RepoRoot:      resolved.RepoRoot,
	}

	m.startTime = parseStartTimeFromID(filepath.Base(sessionDir))

	// FileTailer zero value is safe: offset=0, buffer=nil. ReadSince returns
	// (nil, nil) for missing/empty audit logs.
	events, err := m.tailer.ReadSince(sessionDir)
	if err != nil {
		if m.loadErr != "" {
			m.loadErr = m.loadErr + "; audit log: " + err.Error()
		} else {
			m.loadErr = "audit log: " + err.Error()
		}
	}
	for _, e := range events {
		tree.ApplyEvent(e)
	}

	return m, nil
}

// NewForDefinition constructs a runview Model for inspecting a workflow definition
// without an associated run instance. The workflow file is loaded directly.
func NewForDefinition(entry *discovery.WorkflowEntry, projectDir string) (*Model, error) {
	return newForDefinition(entry.SourcePath, projectDir, entry)
}

func newForDefinition(sourcePath, projectDir string, entry *discovery.WorkflowEntry) (*Model, error) {
	var (
		tree    *Tree
		loadErr string
	)

	wf, err := loader.LoadWorkflow(sourcePath, loader.Options{})
	if err != nil {
		loadErr = "load workflow: " + err.Error()
	} else {
		tree = BuildTree(&wf, sourcePath)
	}

	// Use the canonical name from the entry, or derive from the source path.
	rootName := entry.CanonicalName
	if rootName == "" {
		rootName = deriveCanonicalFromPath(sourcePath)
	}

	if tree == nil {
		tree = &Tree{
			Root: &StepNode{
				ID:     rootName,
				Type:   NodeRoot,
				Status: StatusPending,
			},
		}
	} else if rootName != "" {
		tree.Root.ID = rootName
	}

	m := &Model{
		tree:          tree,
		sessionDir:    sourcePath,
		projectDir:    projectDir,
		entered:       FromDefinition,
		path:          []*StepNode{tree.Root},
		loadedFull:    make(map[string]bool),
		loadErr:       loadErr,
		altScreen:     true,
		workflowEntry: *entry,
	}
	return m, nil
}

// deriveCanonicalFromPath produces a display name from a workflow source path
// when no canonical name is available (e.g. builtin:core/finalize-pr.yaml → core:finalize-pr).
func deriveCanonicalFromPath(sourcePath string) string {
	if rel, ok := strings.CutPrefix(sourcePath, "builtin:"); ok {
		rel = strings.TrimSuffix(rel, filepath.Ext(rel))
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		if len(parts) == 2 {
			return parts[0] + ":" + parts[1]
		}
		return rel
	}
	base := filepath.Base(sourcePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// NewForReentry creates a Model for re-entering the run view after a resumed
// agent CLI subprocess has exited. It re-reads audit and state files from
// sessionDir so any events produced by the resumed session appear. The
// entered mode is preserved from the original entry path (FromLiveRun,
// FromList, or FromInspect) so back-navigation still works. A non-nil
// spawnErr is surfaced to the user in the view.
func NewForReentry(sessionDir, projectDir string, entered Entered, spawnErr error) (*Model, error) {
	m, err := New(sessionDir, projectDir, entered)
	if err != nil {
		return nil, err
	}
	m.running = false
	m.autoFollow = false
	m.altScreen = true
	if spawnErr != nil {
		m.notice = spawnErr.Error()
	}
	return m, nil
}

// describeWorkflowHint returns a compact description of what the resolver
// tried, used in the user-facing error when nothing matched.
func describeWorkflowHint(state *model.RunState, sessionDir string) string {
	var parts []string
	if state.WorkflowFile != "" {
		parts = append(parts, "file="+state.WorkflowFile)
	}
	name := state.WorkflowName
	if name == "" {
		name = parseWorkflowNameFromID(filepath.Base(sessionDir))
	}
	if name != "" {
		parts = append(parts, "name="+name)
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ", ")
}

// altScreenDelay is how long the live-run TUI waits before entering alt-screen.
// If an interactive step starts within this window, alt-screen is suppressed
// entirely — avoiding the flash that would otherwise occur when the TUI
// briefly appears and then immediately releases the terminal.
const altScreenDelay = 200 * time.Millisecond

type deferredAltScreenMsg struct{}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tuistyle.DoRefresh(), tuistyle.DoPulse()}
	if m.entered == FromLiveRun && !m.altScreen {
		cmds = append(cmds, tea.Tick(altScreenDelay, func(time.Time) tea.Msg {
			return deferredAltScreenMsg{}
		}))
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)

	// ---- Live-run messages ----

	case liverun.OutputChunkMsg:
		m.handleOutputChunkMsg(msg)
		return m, nil

	case liverun.StepStateMsg:
		m.handleStepStateMsg(msg)
		return m, nil

	case deferredAltScreenMsg:
		if !m.altScreen && !m.suppressAltScreen {
			m.altScreen = true
			return m, tea.Batch(tea.EnterAltScreen, tea.EnableMouseCellMotion)
		}
		return m, nil

	case liverun.ShowTUIMsg:
		if !m.altScreen {
			m.altScreen = true
			m.suppressAltScreen = false
			return m, tea.Batch(tea.EnterAltScreen, tea.EnableMouseCellMotion)
		}
		return m, nil

	case liverun.SuspendedMsg:
		if !m.altScreen {
			m.suppressAltScreen = true
		}
		return m, nil

	case liverun.ResumedMsg:
		// BubbleTea's RestoreTerminal does not re-enable mouse mode after
		// ReleaseTerminal disables it, so we re-enable it explicitly.
		return m, tea.EnableMouseCellMotion

	case ResumeMsg:
		// Top-level live-run model: no switcher intercepts this, so stash the
		// info and quit. The CLI wrapper execs the agent CLI after p.Run()
		// returns.
		m.resumeAgentCLI = msg.AgentCLI
		m.resumeSessionID = msg.SessionID
		return m, tea.Quit

	case ExitMsg:
		// In the live-run path this Model is the top-level tea.Program model
		// (no switcher wrap), so ExitMsg must be translated into tea.Quit here.
		// When wrapped in the switcher (FromList / FromInspect paths), the
		// switcher intercepts ExitMsg before delegation, so this branch is
		// inert in that case.
		return m, tea.Quit

	case liverun.ExecDoneMsg:
		m.handleExecDoneMsg(msg)
		return m, nil

	// ---- Keyboard / mouse ----

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		m.handleMouse(msg)

	case tuistyle.RefreshMsg:
		cmd := m.handleRefreshMsg()
		return m, cmd

	case tuistyle.PulseMsg:
		cmd := m.handlePulseMsg()
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.termWidth = msg.Width
	m.termHeight = msg.Height
	lineCount := m.rebuildRanges()
	// Re-resolve anchor so the same block stays in view after resize.
	if m.logAnchor.stepKey != "" {
		for _, r := range m.stepRanges {
			if r.node.NodeKey() == m.logAnchor.stepKey {
				m.logOffset = max(0, r.startLine+m.logAnchor.lineOffsetInBlock)
				break
			}
		}
	}
	m.clampLogOffset(lineCount)
}

func (m *Model) handleOutputChunkMsg(msg liverun.OutputChunkMsg) {
	m.applyOutputChunk(msg)
	if m.active || m.running {
		m.logOffset = math.MaxInt32
	}
	lineCount := m.rebuildRanges()
	m.clampLogOffset(lineCount)
}

func (m *Model) handleStepStateMsg(msg liverun.StepStateMsg) {
	m.activeStepPrefix = msg.ActiveStepPrefix
	if m.autoFollow {
		m.applyAutoFollowCursor()
	}
	if m.active || m.running {
		m.logOffset = math.MaxInt32
	}
	lineCount := m.rebuildRanges()
	m.clampLogOffset(lineCount)
}

func (m *Model) handleExecDoneMsg(msg liverun.ExecDoneMsg) {
	// Drain any outstanding audit events before deciding which step to
	// focus. Step statuses reach the tree via audit.log (not OutputChunkMsg),
	// so without this refresh findFailedLeaf can miss a step that finished
	// just before ExecDoneMsg.
	m.refreshData()
	m.running = false
	m.liveResult = msg.Result
	switch msg.Result {
	case "failed":
		if failed := findFailedLeaf(m.tree.Root); failed != nil {
			m.navigateToNode(failed)
		}
	case "success":
		// Land on the final top-level step so the user sees the workflow's
		// end state. Loop iterations and other deep leaves emit StepStateMsg
		// before their tree nodes exist (audit replay runs lazily), so cursor
		// often gets stuck on the last step whose node was already in the tree
		// — not the actual last step that ran.
		if last := lastTopLevelChild(m.tree.Root); last != nil {
			m.navigateToNode(last)
		}
	}
	m.rebuildRanges()
}

func (m *Model) scrollLogUp() {
	if m.logOffset > m.maxLogOffset() {
		m.logOffset = m.maxLogOffset()
	}
	m.logOffset--
	if m.logOffset < 0 {
		m.logOffset = 0
	}
	m.syncSelectionToLog()
}

func (m *Model) handleMouse(msg tea.MouseMsg) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.autoFollow = false
		if m.logOffset > m.maxLogOffset() {
			m.logOffset = m.maxLogOffset()
		}
		m.logOffset -= 3
		if m.logOffset < 0 {
			m.logOffset = 0
		}
		m.syncSelectionToLog()
	case tea.MouseButtonWheelDown:
		m.autoFollow = false
		m.logOffset += 3
		m.syncSelectionToLog()
	}
}

func (m *Model) handleRefreshMsg() tea.Cmd {
	// FromLiveRun leaves m.active=false because no runlock is held, but the
	// in-process runner is still emitting audit events we need to pick up so
	// step statuses stay current.
	if m.active || m.running {
		m.refreshData()
		m.logOffset = math.MaxInt32
		m.rebuildRanges()
	}
	return tuistyle.DoRefresh()
}

func (m *Model) handlePulseMsg() tea.Cmd {
	if m.active || m.running {
		m.pulsePhase += (50.0 / 1000.0) * 2 * math.Pi
	}
	return tuistyle.DoPulse()
}

// canResumeRun reports whether the `r` resume-run action is available.
// True only when the run is inactive (interrupted, not active elsewhere, not
// a just-finished live run, and not in a terminal completed/failed state).
// Always false in FromDefinition mode (r emits StartRunMsg instead).
func (m *Model) canResumeRun() bool {
	return m.entered != FromDefinition &&
		m.sessionDir != "" &&
		!m.running && !m.active && m.liveResult == "" &&
		m.rootStatus() != StatusFailed && m.rootStatus() != StatusSuccess
}

// handleKey processes a key message. Extracted from Update to keep the main
// message switch within funlen limits.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showLegend {
		switch msg.String() {
		case "?", "esc":
			m.showLegend = false
		}
		return m, nil
	}

	// Quit-confirmation modal.
	if m.quitConfirming {
		switch msg.String() {
		case "y", "Y":
			return m, tea.Quit
		case "n", "N", "esc":
			m.quitConfirming = false
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		if m.running {
			m.quitConfirming = true
			return m, nil
		}
		return m, emitExit
	case "?":
		m.showLegend = true
	case "esc":
		m.autoFollow = false
		return m.handleEsc()
	case "enter":
		m.autoFollow = false
		return m.handleEnter()
	case "up":
		m.autoFollow = false
		m.moveCursor(-1)
		m.rebuildRanges()
		m.syncLogToSelection()
	case "down":
		m.autoFollow = false
		m.moveCursor(1)
		m.rebuildRanges()
		m.syncLogToSelection()
	case "k":
		m.autoFollow = false
		m.scrollLogUp()
	case "j":
		m.autoFollow = false
		m.logOffset++
		m.syncSelectionToLog()
	case "l":
		m.autoFollow = true
		m.applyAutoFollowCursor()
	case "r":
		if m.entered == FromDefinition {
			entry := m.workflowEntry
			return m, func() tea.Msg { return discovery.StartRunMsg{Entry: entry} }
		}
		if m.canResumeRun() {
			runID := filepath.Base(m.sessionDir)
			return m, func() tea.Msg { return ResumeRunMsg{RunID: runID} }
		}
	case "g":
		m.handleLoadFull()
		m.rebuildRanges()
	}
	return m, nil
}

// applyAutoFollowCursor moves the cursor to the ancestor-at-current-level of
// the currently active step. This is the new auto-follow logic that does NOT
// drill into sub-workflows or loops.
func (m *Model) applyAutoFollowCursor() {
	active := m.tree.FindByPrefix(m.activeStepPrefix)
	if active == nil {
		return
	}
	target := m.ancestorAtCurrentLevel(active)
	if target == nil {
		return
	}
	if idx := m.indexOfChild(target); idx >= 0 {
		m.cursor = idx
	}
}

// ancestorAtCurrentLevel walks node.Parent until it finds a node whose parent
// is m.currentContainer(), returning that node. Returns nil if not found.
func (m *Model) ancestorAtCurrentLevel(node *StepNode) *StepNode {
	if node == nil {
		return nil
	}
	cc := m.currentContainer()
	for n := node; n != nil; n = n.Parent {
		if n.Parent == cc {
			return n
		}
	}
	return nil
}

// indexOfChild returns the index of node in m.currentChildren(), or -1.
func (m *Model) indexOfChild(node *StepNode) int {
	if node == nil {
		return -1
	}
	for i, c := range m.currentChildren() {
		if c == node {
			return i
		}
	}
	return -1
}

// pendingSelected returns the selected node if it is pending, else nil.
func (m *Model) pendingSelected() *StepNode {
	n := m.selectedNode()
	if n != nil && n.Status == StatusPending {
		return n
	}
	return nil
}

// rebuildRanges recomputes m.stepRanges from the current tree state and
// selection. Called after any mutation that changes log content or width.
func (m *Model) rebuildRanges() int {
	lines, ranges := buildLogLines(
		m.currentChildren(),
		m.pendingSelected(),
		m.rightPaneWidth(),
		m.loadedFull,
		m.pulsePhase,
		m.running || m.active,
		m.resolverCfg,
	)
	m.stepRanges = ranges
	m.logLineCount = len(lines)
	return len(lines)
}

// maxLogOffset returns the maximum valid logOffset for the current log content.
func (m *Model) maxLogOffset() int {
	return max(0, m.logLineCount-m.bodyHeight())
}

// rightPaneWidth estimates the right-pane width for range computation.
// Uses the same formula as renderTwoColumn but with a conservative list-
// column estimate so ranges are close enough for scroll sync.
func (m *Model) rightPaneWidth() int {
	_, rightWidth, _ := twoColumnPaneWidths(m.termWidth, m.buildStepRows(m.currentChildren()))
	return rightWidth
}

// syncLogToSelection sets logOffset so the selected step's block is in view.
func (m *Model) syncLogToSelection() {
	children := m.currentChildren()
	if m.cursor < 0 || m.cursor >= len(children) {
		return
	}
	sel := children[m.cursor]
	for _, r := range m.stepRanges {
		if r.node == sel {
			m.logOffset = r.startLine
			m.logAnchor = stepLineAnchor{stepKey: sel.NodeKey(), lineOffsetInBlock: 0}
			return
		}
	}
}

// syncSelectionToLog updates the step-list cursor to the latest started step
// whose block overlaps the current log viewport.
func (m *Model) syncSelectionToLog() {
	bodyH := m.bodyHeight()
	var winner *StepNode
	winnerStart := 0
	for _, r := range m.stepRanges {
		if r.startLine < m.logOffset+bodyH && r.endLine > m.logOffset {
			if winner == nil || r.startLine > winnerStart {
				winner = r.node
				winnerStart = r.startLine
			}
		}
	}
	if winner != nil {
		target := m.ancestorAtCurrentLevel(winner)
		if idx := m.indexOfChild(target); idx >= 0 {
			m.cursor = idx
		}
		m.logAnchor = stepLineAnchor{
			stepKey:           winner.NodeKey(),
			lineOffsetInBlock: m.logOffset - winnerStart,
		}
	}
}

func (m *Model) clampLogOffset(lineCount int) {
	maxOffset := max(0, lineCount-m.bodyHeight())
	if m.logOffset < 0 {
		m.logOffset = 0
	}
	if m.logOffset > maxOffset {
		m.logOffset = maxOffset
	}
}

// applyOutputChunk finds the step matching msg.StepPrefix and appends msg.Bytes
// to its in-memory output buffer, capping the stored string at the same
// 2000-line / 256 KB limit used by the render path so that chatty steps do not
// grow without bound in memory.
func (m *Model) applyOutputChunk(msg liverun.OutputChunkMsg) {
	node := m.tree.FindByPrefix(msg.StepPrefix)
	if node == nil {
		return
	}
	switch msg.Stream {
	case "stdout":
		node.Stdout = tailOutputCap(node.Stdout + string(msg.Bytes))
	case "stderr":
		node.Stderr = tailOutputCap(node.Stderr + string(msg.Bytes))
	}
}

// tailOutputCap enforces the maxOutputLines / maxOutputBytes cap on a string,
// keeping only the tail. This matches the limits in output.go so memory stays
// bounded even for long-running chatty steps.
func tailOutputCap(s string) string {
	if len(s) <= maxOutputBytes && strings.Count(s, "\n") < maxOutputLines {
		return s
	}
	// Byte cap: keep last maxOutputBytes, then drop any partial leading line.
	if len(s) > maxOutputBytes {
		s = s[len(s)-maxOutputBytes:]
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
	}
	// After byte-capping the string is much shorter; skip the expensive
	// SplitAfter/Join allocation path when the line count is already within limit.
	if strings.Count(s, "\n") <= maxOutputLines {
		return s
	}
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > maxOutputLines {
		lines = lines[len(lines)-maxOutputLines:]
	}
	return strings.Join(lines, "")
}

func (m *Model) moveCursor(delta int) {
	children := m.currentChildren()
	n := len(children)
	if n == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
}

func (m *Model) handleEsc() (tea.Model, tea.Cmd) {
	if len(m.path) > 1 {
		leaving := m.path[len(m.path)-1]
		m.path = m.path[:len(m.path)-1]
		parent := m.currentContainer()
		for i, c := range parent.Children {
			if c == leaving {
				m.cursor = i
				break
			}
		}
		m.logOffset = 0
		m.rebuildRanges()
		return m, nil
	}
	// At top level: show quit-confirm while running, otherwise navigate back.
	if m.running {
		m.quitConfirming = true
		return m, nil
	}
	if m.entered == FromList || m.entered == FromDefinition {
		return m, emitBack
	}
	return m, emitExit
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	n := m.selectedNode()
	if n == nil {
		return m, nil
	}

	switch n.Type {
	case NodeLoop:
		m.path = append(m.path, n)
		m.cursor = 0
		m.logOffset = 0
		m.rebuildRanges()
		return m, nil

	case NodeSubWorkflow:
		if err := m.tree.EnsureSubWorkflowLoaded(n); err != nil && n.ErrorMessage == "" {
			n.ErrorMessage = err.Error()
		}
		m.path = append(m.path, n)
		m.cursor = 0
		m.logOffset = 0
		m.rebuildRanges()
		return m, nil

	case NodeIteration:
		target := n.Drilldown()
		if target != n && target.Type == NodeSubWorkflow {
			if err := m.tree.EnsureSubWorkflowLoaded(target); err != nil && target.ErrorMessage == "" {
				target.ErrorMessage = err.Error()
			}
		}
		m.path = append(m.path, n)
		m.cursor = 0
		m.logOffset = 0
		m.rebuildRanges()
		return m, nil

	case NodeHeadlessAgent, NodeInteractiveAgent:
		// Resume is only meaningful after the run has ended — while the
		// workflow is live, the agent session is owned by the runner and
		// cannot be attached to by the user.
		if n.SessionID != "" && !m.running {
			return m, func() tea.Msg {
				return ResumeMsg{AgentCLI: n.AgentCLI, SessionID: n.SessionID}
			}
		}
	}

	return m, nil
}

func (m *Model) handleLoadFull() {
	n := m.selectedNode()
	if n == nil {
		return
	}
	if n.Type == NodeShell || n.Type == NodeHeadlessAgent {
		m.loadedFull[n.NodeKey()] = true
	}
}

// navigateToNode sets path and cursor so that target is the selected node.
// It respects the auto-flatten rule: iteration nodes with FlattenTarget are
// kept in the path while their FlattenTarget sub-workflow is skipped.
// Sub-workflows in the path are lazy-loaded if necessary. If target is not
// located in the resolved container's children, path and cursor are left
// unchanged to avoid silently misaligning them.
func (m *Model) navigateToNode(target *StepNode) {
	if target == nil {
		return
	}
	// Collect ancestors from target.Parent up to (but not including) root.
	var ancestors []*StepNode
	for n := target.Parent; n != nil && n != m.tree.Root; n = n.Parent {
		ancestors = append(ancestors, n)
	}
	// Reverse so ancestors are in root-first order.
	for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
		ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
	}

	// Build the path, omitting FlattenTarget nodes (their parent iteration
	// stays in the path and provides the container via Drilldown()).
	newPath := []*StepNode{m.tree.Root}
	for _, anc := range ancestors {
		if p := anc.Parent; p != nil && p.Type == NodeIteration && p.FlattenTarget == anc {
			continue
		}
		newPath = append(newPath, anc)
	}

	// Ensure any sub-workflows in the path are loaded.
	for _, pathNode := range newPath[1:] {
		if pathNode.Type == NodeSubWorkflow && !pathNode.SubLoaded {
			if err := m.tree.EnsureSubWorkflowLoaded(pathNode); err != nil && pathNode.ErrorMessage == "" {
				pathNode.ErrorMessage = err.Error()
			}
		}
	}

	// Resolve target's cursor index in the proposed container before
	// mutating state — a miss would otherwise leave path out of sync with
	// cursor. Temporarily swap in newPath to reuse currentChildren().
	savedPath := m.path
	m.path = newPath
	children := m.currentChildren()
	cursor := -1
	for i, c := range children {
		if c == target {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		// Target not in visible children; revert and bail out.
		m.path = savedPath
		return
	}

	m.cursor = cursor
	m.logOffset = 0
}

// lastTopLevelChild returns the final direct child of root, or nil when
// root has no children. Used to park the cursor on the last workflow step
// after a successful run.
func lastTopLevelChild(root *StepNode) *StepNode {
	if root == nil || len(root.Children) == 0 {
		return nil
	}
	return root.Children[len(root.Children)-1]
}

// findFailedLeaf returns the deepest non-container StepNode with StatusFailed,
// or nil if none exists.
func findFailedLeaf(n *StepNode) *StepNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if found := findFailedLeaf(c); found != nil {
			return found
		}
	}
	if n.Status == StatusFailed && !n.IsContainer() {
		return n
	}
	return nil
}

func (m *Model) refreshData() {
	m.active = runlock.Check(m.sessionDir) == runlock.LockActive
	events, err := m.tailer.ReadSince(m.sessionDir)
	if err != nil {
		m.loadErr = "refresh: " + err.Error()
	} else {
		m.loadErr = ""
	}
	for _, e := range events {
		m.tree.ApplyEvent(e)
	}
}

func emitBack() tea.Msg { return BackMsg{} }
func emitExit() tea.Msg { return ExitMsg{} }

var timestampRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}`)

func parseWorkflowNameFromID(sessionID string) string {
	loc := timestampRe.FindStringIndex(sessionID)
	if loc == nil {
		return sessionID
	}
	name := sessionID[:loc[0]]
	return strings.TrimRight(name, "-")
}

func parseStartTimeFromID(sessionID string) time.Time {
	loc := timestampRe.FindStringIndex(sessionID)
	if loc == nil {
		return time.Time{}
	}
	tsPart := sessionID[loc[0]:]
	if len(tsPart) >= 19 {
		ts := []byte(tsPart)
		if ts[13] == '-' && ts[16] == '-' {
			ts[13] = ':'
			ts[16] = ':'
		}
		tsPart = string(ts)
	}
	if len(tsPart) > 19 && tsPart[19] == '-' {
		withDot := tsPart[:19] + "." + tsPart[20:]
		if t, err := time.Parse(time.RFC3339Nano, withDot); err == nil {
			return t
		}
	}
	if t, err := time.Parse(time.RFC3339, tsPart); err == nil {
		return t
	}
	return time.Time{}
}
