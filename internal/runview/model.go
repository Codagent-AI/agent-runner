package runview

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/uistep"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
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

// LaunchDebugMsg asks the shell to exit the TUI and exec the built-in debug
// workflow for the currently viewed agent-runner run.
type LaunchDebugMsg struct {
	FailedRunID      string
	FailedSessionDir string
	FailedProjectDir string
}

// ResumeListMsg asks the shell to exit the current TUI flow and exec
// `agent-runner --resume`, which opens the list TUI on the current-dir tab.
type ResumeListMsg struct{}

type ExitMsg struct {
	UserRequested bool
}

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
	originCwd  string
	entered    Entered

	path         []*StepNode
	cursor       int // cached projected-row index; selected is authoritative
	selected     *StepNode
	selectedKey  string
	treeOffset   int
	sidebarWidth int // widest settled width for this run-view entry
	detailOffset int
	// detailLineCount is the total number of lines in the selected detail document.
	// It is used to clamp the selected detail's independent scroll offset.
	detailLineCount int

	loadedFull map[string]bool
	// inputExpanded stores the user's current-input choice by stable node key.
	// It deliberately survives selection changes and width recalculation.
	inputExpanded map[string]bool

	active        bool
	pulsePhase    float64
	termWidth     int
	termHeight    int
	showLegend    bool
	showSummary   bool
	summaryOffset int // scroll offset (in step rows) for the summary screen
	loadErr       string
	notice        string // transient message shown below the step list (e.g. spawn error)

	resolverCfg     ResolverConfig
	startTime       time.Time
	recordedVersion string
	profileSet      string
	workflowEntry   discovery.WorkflowEntry // set when entered == FromDefinition

	// Live-run fields (FromLiveRun mode only).
	running          bool   // true until ExecDoneMsg arrives
	suspended        bool   // true while an interactive step owns the terminal
	pulseStopped     bool   // true when suspension consumed the scheduled pulse tick
	refreshStopped   bool   // true when suspension consumed the scheduled refresh tick
	quitConfirming   bool   // quit-confirmation modal is visible
	liveResult       string // set on ExecDoneMsg ("success"/"failed"/"stopped")
	followActive     bool   // execution may move selection to the active leaf
	followTail       bool   // selected streaming detail stays pinned to its tail
	activeStepPrefix string // last known active step prefix from StepStateMsg
	copyNoticeSeq    int    // increments on successful copy so stale clear timers are ignored

	// Resume-exec state. When the user selects an agent step after the live
	// run completes, the Model is the top-level tea.Program — there's no
	// switcher to intercept ResumeMsg — so we stash the info here and quit.
	// The CLI wrapper reads it via ResumeAgentCLI/ResumeSessionID after
	// p.Run() returns and execs the agent CLI.
	resumeAgentCLI        string
	resumeSessionID       string
	resumeToList          bool
	launchDebugRunID      string
	launchDebugSessionDir string
	launchDebugProjectDir string
	exitRequested         bool

	// Alt-screen management. When the program starts without tea.WithAltScreen
	// (FromLiveRun mode), alt-screen entry is deferred so a fast non-interactive
	// step followed by an interactive step does not flash the TUI.
	altScreen         bool // true once alt-screen has been entered
	suppressAltScreen bool // set when SuspendedMsg arrives before the deferred timer

	liveUI       *uistep.Model
	liveUIStepID string
	liveUIReply  chan<- model.UIStepResult
}

// ResumeAgentCLI returns the agent CLI name captured from a ResumeMsg in
// live-run mode. Empty when no resume was requested.
func (m *Model) ResumeAgentCLI() string { return m.resumeAgentCLI }

// ResumeSessionID returns the agent CLI session ID captured from a ResumeMsg
// in live-run mode. Empty when no resume was requested.
func (m *Model) ResumeSessionID() string { return m.resumeSessionID }

// ResumeToList reports whether the user requested to leave the run view and
// exec back into the list TUI.
func (m *Model) ResumeToList() bool { return m.resumeToList }

// LaunchDebugRunID returns the failed agent-runner run ID selected for debug.
func (m *Model) LaunchDebugRunID() string { return m.launchDebugRunID }

// LaunchDebugSessionDir returns the absolute session directory selected for debug.
func (m *Model) LaunchDebugSessionDir() string { return m.launchDebugSessionDir }

// LaunchDebugProjectDir returns the original project directory for the failed run.
func (m *Model) LaunchDebugProjectDir() string { return m.launchDebugProjectDir }

// ExitRequested reports whether the user explicitly requested application exit.
func (m *Model) ExitRequested() bool { return m.exitRequested }

// SessionDir returns the session directory the Model was constructed for.
func (m *Model) SessionDir() string { return m.sessionDir }

// ProjectDir returns the project directory the Model was constructed for.
func (m *Model) ProjectDir() string { return m.projectDir }

// Entered returns the entry path used to construct the Model.
func (m *Model) Entered() Entered { return m.entered }

// StartInAltScreen marks the model as already entering alt-screen before the
// live-run program starts. This is used for TUI-to-live-run handoffs where the
// caller wants to avoid exposing the underlying terminal between screens.
func (m *Model) StartInAltScreen() {
	m.altScreen = true
	m.suppressAltScreen = false
}

// New constructs a runview Model from a session directory.
// For FromDefinition mode, sessionDir carries the workflow file path rather than
// a real session directory; audit log loading and run-lock checks are skipped.
func New(sessionDir, projectDir string, entered Entered) (*Model, error) {
	// FromDefinition: load workflow directly from the file path in sessionDir.
	if entered == FromDefinition {
		return NewForDefinition(&discovery.WorkflowEntry{SourcePath: sessionDir}, projectDir)
	}

	state, _ := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	resolved, _ := ResolveWorkflow(sessionDir, projectDir, &state)
	tree, loadErr, workflowMissing := loadRunTree(sessionDir, entered, &state, resolved)

	m := &Model{
		tree:          tree,
		sessionDir:    sessionDir,
		projectDir:    projectDir,
		originCwd:     resolved.OriginCwd,
		entered:       entered,
		path:          []*StepNode{tree.Root},
		loadedFull:    make(map[string]bool),
		inputExpanded: make(map[string]bool),
		loadErr:       loadErr,
		running:       entered == FromLiveRun,
		followActive:  entered == FromLiveRun,
		followTail:    entered == FromLiveRun,
		altScreen:     entered != FromLiveRun,
	}
	m.setSelected(firstRealChild(m.currentContainer()))
	if entered == FromList || entered == FromInspect {
		m.recordedVersion = recordedWorkflowVersion(state.WorkflowFile)
	}
	m.profileSet = state.ProfileSet

	if entered != FromLiveRun {
		m.active = runlock.Check(sessionDir) == runlock.LockActive
		if m.active {
			m.followActive = true
			m.followTail = true
		}
	}

	m.resolverCfg = ResolverConfig{
		WorkflowsRoot: resolved.WorkflowsRoot,
		RepoRoot:      resolved.RepoRoot,
	}
	if m.resolverCfg.WorkflowsRoot == "" {
		m.resolverCfg.WorkflowsRoot = recordedWorkflowsRoot(state.WorkflowFile)
	}

	m.startTime = parseStartTimeFromID(filepath.Base(sessionDir))

	// FileTailer zero value is safe: offset=0, buffer=nil. ReadSince returns
	// (nil, nil) for missing/empty audit logs.
	events, err := m.tailer.ReadSince(sessionDir)
	if err != nil {
		workflowMissing = false
		if m.loadErr != "" {
			m.loadErr = m.loadErr + "; audit log: " + err.Error()
		} else {
			m.loadErr = "audit log: " + err.Error()
		}
	}
	failedHistoricalRun := entered != FromLiveRun && auditRunFailed(events)
	events = filterAuditEventsForWorkflowState(events, state.WorkflowHash, tree.Root, currentStepID(&state), state.Completed)
	if workflowMissing && len(tree.Root.Children) == 0 && reconstructTopLevelStepsFromAudit(tree.Root, events) {
		m.loadErr = ""
		// Re-run the position filter now that currentStepID can be resolved
		// against the recovered top-level order.
		events = filterAuditEventsForWorkflowState(events, state.WorkflowHash, tree.Root, currentStepID(&state), state.Completed)
	}
	for _, e := range events {
		tree.ApplyEvent(e)
	}
	failedHistoricalRun = failedHistoricalRun || entered != FromLiveRun && findFailedLeaf(tree.Root) != nil
	current := m.applyCurrentStepState(&state, !failedHistoricalRun)
	if failedHistoricalRun {
		// Applying the saved position first ensures lazy nested workflows are
		// available to the failure search. Preserve their terminal audit status
		// and select the concrete failed execution at root scope.
		m.setSelected(findFailedLeaf(tree.Root))
	}
	if m.followActive {
		if current != nil {
			m.applyAutoFollowToNode(current)
		} else {
			m.applyAutoFollowToInProgress()
		}
	}

	return m, nil
}

func auditRunFailed(events []RawEvent) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "run_end" {
			continue
		}
		outcome, _ := stringField(events[i].Data, "outcome")
		return outcome == "failed"
	}
	return false
}

func loadRunTree(
	sessionDir string,
	entered Entered,
	state *model.RunState,
	resolved ResolvedWorkflow,
) (*Tree, string, bool) {
	if resolved.AbsPath != "" {
		workflow, err := loader.LoadWorkflow(resolved.AbsPath, loader.Options{})
		if err == nil {
			return BuildTree(&workflow, resolved.AbsPath), "", false
		}
		var filenameErr *workflowcatalog.FilenameError
		reconstructable := (entered == FromList || entered == FromInspect) && errors.As(err, &filenameErr)
		return fallbackRunTree(sessionDir, state, resolved.OriginCwd), "load workflow: " + err.Error(), reconstructable
	}
	if state.WorkflowFile != "" || state.WorkflowName != "" {
		loadErr := "workflow file not found (state: " + describeWorkflowHint(state, sessionDir) + ")"
		return fallbackRunTree(sessionDir, state, resolved.OriginCwd), loadErr, true
	}
	return fallbackRunTree(sessionDir, state, resolved.OriginCwd), "", false
}

func fallbackRunTree(sessionDir string, state *model.RunState, originCwd string) *Tree {
	rootName := state.WorkflowName
	if rootName == "" {
		rootName = parseWorkflowNameFromID(filepath.Base(sessionDir))
	}
	if rootName == "" {
		rootName = filepath.Base(sessionDir)
	}
	return &Tree{
		Root: &StepNode{
			ID:     rootName,
			Type:   NodeRoot,
			Status: StatusPending,
		},
		WorkflowPath: recordedWorkflowDisplayPath(state.WorkflowFile, originCwd),
	}
}

func recordedWorkflowDisplayPath(workflowFile, originCwd string) string {
	if workflowFile == "" || strings.HasPrefix(workflowFile, "builtin:") || filepath.IsAbs(workflowFile) {
		return workflowFile
	}
	if originCwd != "" {
		return filepath.Join(originCwd, workflowFile)
	}
	return filepath.Clean(workflowFile)
}

func recordedWorkflowsRoot(workflowFile string) string {
	if workflowFile == "" || strings.HasPrefix(workflowFile, "builtin:") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(workflowFile)), "/")
	for index := len(parts) - 2; index >= 0; index-- {
		if parts[index] == "workflows" {
			return filepath.FromSlash(strings.Join(parts[:index+1], "/"))
		}
	}
	return ""
}

func currentStepID(state *model.RunState) string {
	if state == nil {
		return ""
	}
	if state.CurrentStep.Nested != nil {
		return state.CurrentStep.Nested.StepID
	}
	return state.CurrentStep.StepID
}

func (m *Model) applyCurrentStepState(state *model.RunState, markInProgress bool) *StepNode {
	if state == nil {
		return nil
	}
	if state.CurrentStep.Nested != nil {
		return m.applyNestedCurrentStepState(m.tree.Root, state.CurrentStep.Nested, markInProgress)
	}
	if state.CurrentStep.StepID == "" {
		return nil
	}
	node := childByID(m.tree.Root, state.CurrentStep.StepID)
	markCurrentNode(node, !markInProgress)
	return node
}

func (m *Model) applyNestedCurrentStepState(container *StepNode, current *model.NestedStepState, markInProgress bool) *StepNode {
	if container == nil || current == nil || current.StepID == "" {
		return nil
	}
	scope := container.Drilldown()
	if scope == nil {
		return nil
	}
	node := childByID(scope, current.StepID)
	if node == nil {
		return nil
	}
	markCurrentNode(node, !markInProgress || current.Completed && current.Child == nil)

	if current.Iteration != nil {
		iter := findIteration(node, *current.Iteration)
		if iter == nil {
			iter = ensureIteration(node, *current.Iteration)
		}
		markCurrentNode(iter, !markInProgress || current.Completed && current.Child == nil)
		if current.Child != nil {
			if child := m.applyNestedCurrentStepState(iter, current.Child, markInProgress); child != nil {
				return child
			}
		}
		return iter
	}

	if current.Child != nil {
		if node.Type == NodeSubWorkflow {
			if err := m.tree.EnsureSubWorkflowLoaded(node); err != nil && node.ErrorMessage == "" {
				node.ErrorMessage = err.Error()
			}
		}
		if child := m.applyNestedCurrentStepState(node, current.Child, markInProgress); child != nil {
			return child
		}
	}
	return node
}

func markCurrentNode(node *StepNode, completed bool) {
	if node == nil || completed {
		return
	}
	node.Status = StatusInProgress
	node.Aborted = false
	node.Outcome = ""
}

// NewForDefinition constructs a runview Model for inspecting a workflow definition
// without an associated run instance. The workflow file is loaded directly.
func NewForDefinition(entry *discovery.WorkflowEntry, projectDir string) (*Model, error) {
	sourcePath := entry.SourcePath
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
		originCwd:     projectDir,
		entered:       FromDefinition,
		path:          []*StepNode{tree.Root},
		loadedFull:    make(map[string]bool),
		inputExpanded: make(map[string]bool),
		loadErr:       loadErr,
		altScreen:     true,
		workflowEntry: *entry,
	}
	return m, nil
}

// deriveCanonicalFromPath produces a display name from a workflow source path
// when no canonical name is available (e.g. builtin:core/finalize-pr-v1.0.yaml → core:finalize-pr).
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
	m.followActive = false
	m.followTail = false
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
const altScreenDelay = 1000 * time.Millisecond

type deferredAltScreenMsg struct{}
type copyNoticeExpiredMsg struct{ seq int }

func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.hasLiveUpdates() {
		cmds = append(cmds, tuistyle.DoRefresh(), tuistyle.DoPulse())
	}
	if m.entered == FromLiveRun && !m.altScreen {
		cmds = append(cmds, tea.Tick(altScreenDelay, func(time.Time) tea.Msg {
			return deferredAltScreenMsg{}
		}))
	}
	return tea.Batch(cmds...)
}

func (m *Model) hasLiveUpdates() bool {
	return m.active || m.running
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

	case *liverun.UIRequestMsg:
		return m.handleUIRequestMsg(msg)

	case deferredAltScreenMsg:
		cmd := m.handleDeferredAltScreen()
		return m, cmd

	case copyNoticeExpiredMsg:
		m.handleCopyNoticeExpired(msg)
		return m, nil

	case liverun.ShowTUIMsg:
		return m.handleShowTUIMsg()

	case liverun.SuspendedMsg:
		m.handleSuspendedMsg()
		return m, nil

	case liverun.ResumedMsg:
		cmd := m.handleResumedMsg()
		return m, cmd

	case ResumeMsg:
		// Top-level live-run model: no switcher intercepts this, so stash the
		// info and quit. The CLI wrapper execs the agent CLI after p.Run()
		// returns.
		m.resumeAgentCLI = msg.AgentCLI
		m.resumeSessionID = msg.SessionID
		return m, tea.Quit

	case ResumeListMsg:
		m.resumeToList = true
		return m, tea.Quit

	case LaunchDebugMsg:
		m.launchDebugRunID = msg.FailedRunID
		m.launchDebugSessionDir = msg.FailedSessionDir
		m.launchDebugProjectDir = msg.FailedProjectDir
		return m, tea.Quit

	case ExitMsg:
		// In the live-run path this Model is the top-level tea.Program model
		// (no switcher wrap), so ExitMsg must be translated into tea.Quit here.
		// When wrapped in the switcher (FromList / FromInspect paths), the
		// switcher intercepts ExitMsg before delegation, so this branch is
		// inert in that case.
		if msg.UserRequested {
			m.exitRequested = true
		}
		return m, tea.Quit

	case liverun.ExecDoneMsg:
		m.handleExecDoneMsg(msg)
		return m, nil

	// ---- Keyboard / mouse ----

	case tea.KeyMsg:
		if m.quitConfirming {
			return m.handleKey(msg)
		}
		if m.liveUI != nil && m.liveUIVisible() {
			return m.handleLiveUIKey(msg)
		}
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

func (m *Model) handleUIRequestMsg(msg *liverun.UIRequestMsg) (tea.Model, tea.Cmd) {
	m.liveUI = uistep.NewModel(&msg.Request)
	m.liveUIStepID = msg.Request.StepID
	m.liveUIReply = msg.Reply
	m.refreshData()
	if m.followActive {
		m.applyAutoFollowToInProgress()
	}
	m.rebuildDetail()
	return m.handleShowTUIMsg()
}

func (m *Model) handleShowTUIMsg() (tea.Model, tea.Cmd) {
	if !m.altScreen {
		m.altScreen = true
		m.suppressAltScreen = false
		return m, tea.Batch(tea.EnterAltScreen, tea.EnableMouseCellMotion)
	}
	return m, nil
}

func (m *Model) handleSuspendedMsg() {
	m.suspended = true
	// Terminal ownership is not an input gesture. In particular, it cannot
	// re-enable follow after the user deliberately paused exploration.
	m.rebuildDetail()
	if !m.altScreen {
		m.suppressAltScreen = true
	}
}

func (m *Model) handleResumedMsg() tea.Cmd {
	m.suspended = false
	if m.hasLiveUpdates() {
		selectedBefore := m.selectedNode()
		previousLineCount := m.detailLineCount
		m.refreshData()
		if m.followActive {
			m.applyAutoFollowCursor()
		}
		lineCount := m.rebuildDetail()
		if m.shouldFollowTail() && (m.selectedNode() != selectedBefore || lineCount != previousLineCount) {
			m.scrollSelectedDetailToTail()
		}
		m.clampDetailOffset(lineCount)
	}
	// BubbleTea's RestoreTerminal does not re-enable mouse mode after
	// ReleaseTerminal disables it, so we re-enable it explicitly.
	cmds := []tea.Cmd{tea.EnableMouseCellMotion}
	if m.pulseStopped && m.hasLiveUpdates() {
		cmds = append(cmds, tuistyle.DoPulse())
	}
	if m.refreshStopped && m.hasLiveUpdates() {
		cmds = append(cmds, tuistyle.DoRefresh())
	}
	m.pulseStopped = false
	m.refreshStopped = false
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleLiveUIKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "down":
		return m.handleKey(msg)
	case "k":
		m.scrollLiveUI(-1)
		return m, nil
	case "j":
		m.scrollLiveUI(1)
		return m, nil
	case "q", "ctrl+c":
		return m.handleKey(msg)
	case "esc":
		return m.handleKey(msg)
	case "l":
		m.followLiveUI()
		return m, nil
	}
	if !m.liveUI.HandlesKey(msg.String()) {
		return m.handleKey(msg)
	}

	next, _ := m.liveUI.Update(msg)
	if updated, ok := next.(*uistep.Model); ok {
		m.liveUI = updated
	}
	if m.liveUI != nil && m.liveUI.Done() {
		result := m.liveUI.Result()
		reply := m.liveUIReply
		m.liveUI = nil
		m.liveUIStepID = ""
		m.liveUIReply = nil
		if reply != nil {
			reply <- result
		}
	}
	return m, nil
}

func (m *Model) scrollLiveUI(delta int) {
	if delta < 0 {
		m.followActive = false
		m.followTail = false
	}
	m.detailOffset += delta
	if m.detailOffset < 0 {
		m.detailOffset = 0
	}
}

// requestQuit handles the `q` action: while a run is live it opens the
// quit-confirmation modal; otherwise it requests exit immediately.
func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.running {
		m.quitConfirming = true
		return m, nil
	}
	m.exitRequested = true
	return m, emitExit
}

// scrollSummary adjusts the summary scroll offset. The lower bound is applied
// here; the upper bound depends on rendered row count vs. available height and
// is clamped at render time (see renderSummary), matching the detail-offset model.
func (m *Model) scrollSummary(delta int) {
	m.moveCursor(delta)
}

func (m *Model) liveUIVisible() bool {
	if m.liveUI == nil {
		return false
	}
	active := m.liveUINode()
	if active == nil {
		return true
	}
	return m.selectedNode() == active
}

func (m *Model) liveUINode() *StepNode {
	if m.liveUIStepID == "" {
		return nil
	}
	return findDeepestInProgressUI(m.tree.Root, m.liveUIStepID)
}

func (m *Model) followLiveUI() {
	active := m.liveUINode()
	if active == nil {
		return
	}
	m.followActive = true
	m.followTail = true
	m.path = []*StepNode{m.tree.Root}
	m.treeOffset = 0
	m.navigateToNode(active)
}

func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.termWidth = msg.Width
	m.termHeight = msg.Height
	// A resize establishes a new layout measurement rather than preserving an
	// old width that was settled for a different terminal.
	m.sidebarWidth = 0
	lineCount := m.rebuildDetail()
	m.clampDetailOffset(lineCount)
}

func (m *Model) handleOutputChunkMsg(msg liverun.OutputChunkMsg) {
	m.applyOutputChunk(msg)
	lineCount := m.rebuildDetail()
	if m.shouldFollowSelectedTail(msg.StepPrefix) {
		m.scrollSelectedDetailToTail()
	}
	m.clampDetailOffset(lineCount)
}

func (m *Model) handleStepStateMsg(msg liverun.StepStateMsg) {
	m.activeStepPrefix = msg.ActiveStepPrefix
	m.refreshData()
	selectedBefore := m.selectedNode()
	if m.followActive {
		m.applyAutoFollowCursor()
	}
	lineCount := m.rebuildDetail()
	if m.shouldFollowTail() && m.selectedNode() != selectedBefore {
		m.scrollSelectedDetailToTail()
	}
	m.clampDetailOffset(lineCount)
}

func (m *Model) handleExecDoneMsg(msg liverun.ExecDoneMsg) {
	// Drain any outstanding audit events before deciding which step to
	// focus. Step statuses reach the tree via audit.log (not OutputChunkMsg),
	// so without this refresh findFailedLeaf can miss a step that finished
	// just before ExecDoneMsg.
	m.refreshData()
	m.running = false
	m.active = false
	m.liveResult = msg.Result
	if msg.Err != nil {
		m.notice = msg.Err.Error()
	}
	switch msg.Result {
	case "failed":
		m.showSummary = false
		if failed := findFailedLeaf(m.tree.Root); failed != nil {
			m.path = []*StepNode{m.tree.Root}
			m.treeOffset = 0
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
	// Terminal detail is historical inspection: live updates may no longer
	// change a user's selection or detail viewport.
	m.followActive = false
	m.followTail = false
	m.rebuildDetail()
}

func (m *Model) scrollDetailUp() {
	if m.detailOffset > m.maxDetailOffset() {
		m.detailOffset = m.maxDetailOffset()
	}
	m.detailOffset--
	if m.detailOffset < 0 {
		m.detailOffset = 0
	}
}

func (m *Model) handleMouse(msg tea.MouseMsg) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.followActive = false
		m.followTail = false
		if m.detailOffset > m.maxDetailOffset() {
			m.detailOffset = m.maxDetailOffset()
		}
		m.detailOffset -= 3
		if m.detailOffset < 0 {
			m.detailOffset = 0
		}
	case tea.MouseButtonWheelDown:
		m.detailOffset += 3
	}
}

func (m *Model) handleRefreshMsg() tea.Cmd {
	if m.suspended {
		m.refreshStopped = true
		return nil
	}
	// FromLiveRun leaves m.active=false because no runlock is held, but the
	// in-process runner is still emitting audit events we need to pick up so
	// step statuses stay current.
	if !m.hasLiveUpdates() {
		return nil
	}
	selectedBefore := m.selectedNode()
	previousLineCount := m.detailLineCount
	m.refreshData()
	if m.followActive {
		m.applyAutoFollowCursor()
	}
	lineCount := m.rebuildDetail()
	if m.shouldFollowTail() && (m.selectedNode() != selectedBefore || lineCount != previousLineCount) {
		m.scrollSelectedDetailToTail()
	}
	m.clampDetailOffset(lineCount)
	if !m.hasLiveUpdates() {
		return nil
	}
	return tuistyle.DoRefresh()
}

func (m *Model) shouldFollowTail() bool {
	return (m.active || m.running) && m.followTail && !m.liveUIVisible()
}

func (m *Model) shouldFollowSelectedTail(prefix string) bool {
	if !m.shouldFollowTail() {
		return false
	}
	return m.tree.FindByPrefix(prefix) == m.selectedNode()
}

func (m *Model) handlePulseMsg() tea.Cmd {
	if m.suspended {
		m.pulseStopped = true
		return nil
	}
	if !m.hasLiveUpdates() {
		return nil
	}
	m.pulsePhase += (50.0 / 1000.0) * 2 * math.Pi
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

func (m *Model) canLaunchDebug() bool {
	return m.entered != FromDefinition &&
		m.sessionDir != "" &&
		!m.running && !m.active
}

func (m *Model) canResumeAgentSession(n *StepNode) bool {
	if n == nil || n.SessionID == "" || m.running || m.active {
		return false
	}
	return n.Type != NodeAgentCall || (n.Status != StatusPending && n.Status != StatusInProgress)
}

func isAgentNode(n *StepNode) bool {
	return n != nil && (n.Type == NodeHeadlessAgent || n.Type == NodeInteractiveAgent || n.Type == NodeAgentCall)
}

func (m *Model) resumeAgentTargetForSelection() *StepNode {
	if m.rootStatus() != StatusSuccess {
		return nil
	}
	selected := m.selectedNode()
	if isAgentNode(selected) && m.canResumeAgentSession(selected) {
		return selected
	}
	// Resume scope follows the selected execution's structural ancestry, not
	// the optional manual drill path. Inline-expanded nested rows therefore
	// resume within their own sub-workflow before considering root.
	for ancestor := selected; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type != NodeSubWorkflow && ancestor.Type != NodeRoot {
			continue
		}
		if target := m.lastResumableAgentInWorkflow(ancestor); target != nil {
			return target
		}
	}
	return nil
}

func (m *Model) lastResumableAgentInWorkflow(scope *StepNode) *StepNode {
	if scope == nil {
		return nil
	}
	if isAgentNode(scope) && m.canResumeAgentSession(scope) {
		return scope
	}
	for i := len(scope.Children) - 1; i >= 0; i-- {
		child := scope.Children[i]
		if found := m.lastResumableAgentInWorkflow(child); found != nil {
			return found
		}
	}
	return nil
}

// handleOverlayKey processes keys for the modal overlays (legend, summary,
// quit confirmation) that intercept input before the main key switch. These
// are checked in priority order; the bool result reports whether an overlay
// consumed the key.
func (m *Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case m.showLegend:
		switch msg.String() {
		case "?", "esc":
			m.showLegend = false
		}
		return m, nil, true
	case m.showSummary:
		switch msg.String() {
		case "v", "s":
			m.showSummary = false
			m.summaryOffset = 0
		case "esc":
			if len(m.path) > 1 {
				mdl, cmd := m.handleEsc()
				m.summaryOffset = 0
				return mdl, cmd, true
			}
			m.showSummary = false
			m.summaryOffset = 0
		case "q":
			// Dismiss the summary before opening the quit-confirmation modal so
			// its y/n keys reach that handler instead of this block.
			m.showSummary = false
			m.summaryOffset = 0
			mdl, cmd := m.requestQuit()
			return mdl, cmd, true
		case "up", "k":
			m.followActive = false
			if msg.String() == "k" {
				m.followTail = false
			}
			m.scrollSummary(-1)
		case "down", "j":
			if msg.String() == "down" {
				m.followActive = false
			}
			m.scrollSummary(1)
		case "enter":
			if selected := m.selectedNode(); selected != nil && selected.IsContainer() {
				mdl, cmd := m.handleEnter()
				m.summaryOffset = 0
				return mdl, cmd, true
			}
		}
		return m, nil, true
	case m.quitConfirming:
		switch msg.String() {
		case "y", "Y":
			m.exitRequested = true
			return m, tea.Quit, true
		case "n", "N", "esc":
			m.quitConfirming = false
		}
		return m, nil, true
	}
	return m, nil, false
}

// handleKey processes a key message. Extracted from Update to keep the main
// message switch within funlen limits.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.exitRequested = true
		return m, tea.Quit
	}

	if mdl, cmd, handled := m.handleOverlayKey(msg); handled {
		return mdl, cmd
	}

	switch msg.String() {
	case "q":
		return m.requestQuit()
	case "?":
		m.showLegend = true
	case "s":
		m.showSummary = !m.showSummary
		m.summaryOffset = 0
	case "esc":
		m.followActive = false
		return m.handleEsc()
	case "enter":
		m.followActive = false
		return m.handleEnter()
	case "up":
		cmd := m.handleStepNavigation(-1)
		return m, cmd
	case "down":
		cmd := m.handleStepNavigation(1)
		return m, cmd
	case "k":
		m.followActive = false
		m.followTail = false
		m.scrollDetailUp()
	case "j":
		m.detailOffset++
	case "t":
		m.handleTailFollowKey()
	case "l":
		m.handleFollowKey()
	case "r":
		return m.handleResumeKey()
	case "d":
		return m.handleDebugKey()
	case "g":
		m.handleLoadFull()
		m.rebuildDetail()
	case "i":
		m.toggleSelectedInputExpansion()
	case "c":
		cmd := m.handleCopySelectedDetail()
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleDebugKey() (tea.Model, tea.Cmd) {
	if !m.canLaunchDebug() {
		return m, nil
	}
	runID := filepath.Base(m.sessionDir)
	return m, func() tea.Msg {
		return LaunchDebugMsg{
			FailedRunID:      runID,
			FailedSessionDir: m.sessionDir,
			FailedProjectDir: m.copyDirectory(),
		}
	}
}

func (m *Model) handleFollowKey() {
	if !m.hasLiveUpdates() && m.liveUI == nil {
		return
	}
	if m.liveUI != nil {
		m.followLiveUI()
		return
	}
	m.followActive = true
	m.followTail = true
	m.path = []*StepNode{m.tree.Root}
	m.treeOffset = 0
	m.applyAutoFollowCursor()
	lineCount := m.rebuildDetail()
	m.scrollSelectedDetailToTail()
	m.clampDetailOffset(lineCount)
}

func (m *Model) handleTailFollowKey() {
	if !m.hasLiveUpdates() {
		return
	}
	m.followTail = true
	lineCount := m.rebuildDetail()
	m.scrollSelectedDetailToTail()
	m.clampDetailOffset(lineCount)
}

func (m *Model) handleResumeKey() (tea.Model, tea.Cmd) {
	if m.entered == FromDefinition {
		entry := m.workflowEntry
		return m, func() tea.Msg { return discovery.StartRunMsg{Entry: entry} }
	}
	if m.canResumeRun() {
		runID := filepath.Base(m.sessionDir)
		return m, func() tea.Msg { return ResumeRunMsg{RunID: runID} }
	}
	if target := m.resumeAgentTargetForSelection(); target != nil {
		return m, func() tea.Msg {
			return ResumeMsg{AgentCLI: target.AgentCLI, SessionID: target.SessionID}
		}
	}
	return m, nil
}

func (m *Model) handleDeferredAltScreen() tea.Cmd {
	if !m.altScreen && !m.suppressAltScreen {
		m.altScreen = true
		return tea.Batch(tea.EnterAltScreen, tea.EnableMouseCellMotion)
	}
	return nil
}

func (m *Model) handleCopyNoticeExpired(msg copyNoticeExpiredMsg) {
	if msg.seq == m.copyNoticeSeq && m.notice == "copied selected step detail" {
		m.notice = ""
	}
}

func (m *Model) handleCopySelectedDetail() tea.Cmd {
	text := m.selectedStepDetailText()
	if text == "" {
		m.notice = "copy failed: no selected step detail"
		return nil
	}
	if err := writeClipboard(text); err != nil {
		m.notice = "copy failed: " + err.Error()
		return nil
	}
	m.notice = "copied selected step detail"
	m.copyNoticeSeq++
	seq := m.copyNoticeSeq
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return copyNoticeExpiredMsg{seq: seq}
	})
}

func (m *Model) selectedStepDetailText() string {
	doc := m.selectedDetailDocument(m.rightPaneWidth())
	if len(doc.header) == 0 {
		return ""
	}
	return m.copyTextWithContext(doc.renderCopy())
}

func (m *Model) selectedDetailDocument(width int) detailDocument {
	node := m.selectedNode()
	m.loadHistoricalOutput(node)
	var previous *StepNode
	if m.tree != nil {
		previous = m.tree.PreviousExecution(node)
		m.loadHistoricalOutput(previous)
	}
	expanded := false
	if node != nil && m.inputExpanded != nil {
		expanded = m.inputExpanded[node.NodeKey()]
	}
	return buildDetailDocument(node, detailBuildOptions{
		width:         width,
		loadedFull:    node != nil && m.loadedFull[node.NodeKey()],
		inputExpanded: expanded,
		previous:      previous,
		pulsePhase:    m.pulsePhase,
		runActive:     m.running || m.active,
		resumeReady:   m.canResumeAgentSession(node),
		resolverCfg:   m.resolverCfg,
	})
}

func (m *Model) toggleSelectedInputExpansion() {
	node := m.selectedNode()
	if node == nil {
		return
	}
	input := ""
	switch node.Type {
	case NodeShell:
		input = currentCommand(node)
	case NodeScript:
		input = node.StaticScript
	case NodeHeadlessAgent, NodeInteractiveAgent, NodeAgentCall:
		input = currentPrompt(node)
	}
	if len(wrappedPlainLines(input, m.rightPaneWidth()-3)) <= 3 {
		return
	}
	if m.inputExpanded == nil {
		m.inputExpanded = make(map[string]bool)
	}
	key := node.NodeKey()
	m.inputExpanded[key] = !m.inputExpanded[key]
	m.rebuildDetail()
}

func (m *Model) copyTextWithContext(detail string) string {
	var parts []string
	if dir := m.copyDirectory(); dir != "" {
		parts = append(parts, "directory: "+dir)
	}
	if breadcrumb := strings.TrimSpace(tuistyle.Sanitize(m.renderBreadcrumb())); breadcrumb != "" {
		parts = append(parts, "breadcrumb: "+breadcrumb)
	}
	if detail != "" {
		if len(parts) > 0 {
			parts = append(parts, "")
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, "\n")
}

func (m *Model) copyDirectory() string {
	if m.originCwd != "" {
		return m.originCwd
	}
	return m.projectDir
}

func (m *Model) handleStepNavigation(delta int) tea.Cmd {
	m.followActive = false
	m.moveCursor(delta)
	m.detailOffset = 0
	m.rebuildDetail()
	return nil
}

// applyAutoFollowCursor selects the current active leaf without changing the
// manual drill scope.
func (m *Model) applyAutoFollowCursor() {
	if active := m.liveUINode(); active != nil {
		m.applyAutoFollowToNode(active)
		return
	}
	active := m.tree.FindByPrefix(m.activeStepPrefix)
	if active == nil {
		return
	}
	m.applyAutoFollowToNode(active)
}

func (m *Model) applyAutoFollowToInProgress() {
	m.applyAutoFollowToNode(deepestInProgressNode(m.tree.Root))
}

func (m *Model) applyAutoFollowToNode(active *StepNode) {
	if active == nil {
		return
	}
	// Following changes selection only. It deliberately leaves the manual
	// breadcrumb scope untouched; the projector exposes the active ancestry
	// whenever it lies under that scope.
	if scope := m.currentContainer(); scope == nil || !isDescendantOf(active, scope) {
		return
	}
	m.setSelected(active)
	m.ensureTreeSelectionVisible(m.cursor)
}

func deepestInProgressNode(n *StepNode) *StepNode {
	return deepestMatchingNode(n, func(node *StepNode) bool {
		return node.Parent != nil && node.Status == StatusInProgress
	})
}

func findDeepestInProgressUI(n *StepNode, stepID string) *StepNode {
	return deepestMatchingNode(n, func(node *StepNode) bool {
		return node.ID == stepID && node.Type == NodeUI && node.Status == StatusInProgress
	})
}

func deepestMatchingNode(n *StepNode, matches func(*StepNode) bool) *StepNode {
	if n == nil {
		return nil
	}
	var found *StepNode
	for _, child := range n.Children {
		if candidate := deepestMatchingNode(child, matches); candidate != nil {
			found = candidate
		}
	}
	if found != nil {
		return found
	}
	if matches(n) {
		return n
	}
	return nil
}

// rebuildDetail recomputes the selected detail's line count after a change to
// its content or width. Tree selection is intentionally not derived from it.
func (m *Model) rebuildDetail() int {
	lines := m.selectedDetailDocument(m.rightPaneWidth()).renderScreen()
	m.detailLineCount = len(lines)
	return len(lines)
}

// maxDetailOffset returns the maximum valid offset for the currently visible
// right-pane content.
func (m *Model) maxDetailOffset() int {
	return max(0, m.rightPaneLineCount(m.detailLineCount)-m.bodyHeight())
}

func (m *Model) rightPaneLineCount(fallback int) int {
	if !m.liveUIVisible() {
		return fallback
	}
	return len(m.liveUIDetailLines(m.rightPaneWidth()))
}

func (m *Model) liveUIDetailLines(width int) []string {
	if m.liveUI == nil {
		return nil
	}
	// The rail consumes three columns, so give the embedded form only the
	// remaining selected-detail width before the document wraps it.
	m.liveUI.SetWidth(max(1, width-3))
	doc := m.selectedDetailDocument(width)
	for i := range doc.sections {
		if doc.sections[i].label == currentFormLabel {
			doc.sections[i].body = m.liveUI.View()
			doc.sections[i].copy = ""
			break
		}
	}
	return doc.renderScreen()
}

// rightPaneWidth estimates the right-pane width for selected-detail line
// calculation using the same tree layout measurement as rendering.
func (m *Model) rightPaneWidth() int {
	layout := measureTreePaneLayout(m.termWidth, rowTexts(m.buildProjectedRenderedRows()), m.sidebarWidth)
	return layout.detail
}

func (m *Model) scrollSelectedDetailToTail() {
	m.detailOffset = m.maxDetailOffset()
}

func (m *Model) clampDetailOffset(lineCount int) {
	lineCount = m.rightPaneLineCount(lineCount)
	maxOffset := max(0, lineCount-m.bodyHeight())
	if m.detailOffset < 0 {
		m.detailOffset = 0
	}
	if m.detailOffset > maxOffset {
		m.detailOffset = maxOffset
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

// loadHistoricalOutput loads an execution's bounded persisted evidence. Calls
// and ordinary autonomous steps use the same source, while only agents pass
// raw output through their normal CLI filter before it reaches selected detail.
func (m *Model) loadHistoricalOutput(node *StepNode) {
	m.loadHistoricalOutputFromDisk(node, false)
}

func (m *Model) loadHistoricalOutputFromDisk(node *StepNode, full bool) bool {
	if node == nil || m.sessionDir == "" || !historicalOutputNode(node) || (!full && node.OutputLoaded) {
		return false
	}
	// A live in-process child is already feeding chunks through OutputChunkMsg.
	// Reading its still-growing file as well would duplicate bytes.
	if m.entered == FromLiveRun && node.Status == StatusInProgress {
		return false
	}
	prefix := node.OutputPrefix
	if node.Type == NodeAgentCall {
		prefix = node.CallOutputPrefix
		if !full && node.CallOutputLoaded {
			return false
		}
	}
	if prefix == "" {
		return false
	}
	base := liverun.SanitizeOutputPrefix(prefix)
	if base == "" || base == "." || base == ".." || strings.Contains(base, "..") {
		m.loadErr = fmt.Sprintf("%s: invalid output prefix %q", outputLoadLabel(node), prefix)
		return false
	}
	root, err := os.OpenRoot(filepath.Join(m.sessionDir, "output"))
	if errors.Is(err, os.ErrNotExist) {
		markHistoricalOutputLoaded(node)
		m.clearOutputLoadError(node)
		return true
	}
	if err != nil {
		m.loadErr = outputLoadLabel(node) + ": " + err.Error()
		return false
	}

	stdout, stdoutFound, stderr, stderrFound, err := readOutputs(root, base, full)
	if err == nil && !stdoutFound && !stderrFound {
		legacyBase := liverun.LegacySanitizeOutputPrefix(prefix)
		if legacyBase != base {
			stdout, stdoutFound, stderr, stderrFound, err = readOutputs(root, legacyBase, full)
		}
	}
	if err != nil {
		err = errors.Join(err, root.Close())
		m.loadErr = outputLoadLabel(node) + ": " + err.Error()
		return false
	}
	if err := root.Close(); err != nil {
		m.loadErr = outputLoadLabel(node) + ": " + err.Error()
		return false
	}
	if node.Type == NodeHeadlessAgent || node.Type == NodeAgentCall {
		stdout, stderr = filterAgentOutput(node, stdout, stderr)
	}
	if stdoutFound {
		node.Stdout = stdout
	}
	if stderrFound {
		node.Stderr = stderr
	}
	markHistoricalOutputLoaded(node)
	m.clearOutputLoadError(node)
	return true
}

func markHistoricalOutputLoaded(node *StepNode) {
	node.OutputLoaded = true
	if node.Type == NodeAgentCall {
		node.CallOutputLoaded = true
	}
}

func historicalOutputNode(node *StepNode) bool {
	if node == nil || isInteractiveExecution(node) {
		return false
	}
	switch node.Type {
	case NodeShell, NodeScript, NodeHeadlessAgent, NodeAgentCall:
		return true
	default:
		return false
	}
}

func outputLoadLabel(node *StepNode) string {
	if node != nil && node.Type == NodeAgentCall {
		return "load call output"
	}
	return "load output"
}

func filterAgentOutput(node *StepNode, rawStdout, rawStderr string) (stdout, stderr string) {
	stdout, stderr = rawStdout, rawStderr
	adapter, err := cli.Get(node.AgentCLI)
	if err != nil {
		return stdout, stderr
	}
	if filter, ok := adapter.(cli.HeadlessResultFilter); ok {
		exitCode := 0
		if node.ExitCode != nil {
			exitCode = *node.ExitCode
		}
		_, stderr = filter.FilterHeadlessResult(exitCode, stdout, stderr)
	}
	if filter, ok := adapter.(cli.OutputFilter); ok {
		stdout = filter.FilterOutput(stdout)
	}
	return stdout, stderr
}

func readOutputs(root *os.Root, base string, full bool) (stdout string, stdoutFound bool, stderr string, stderrFound bool, err error) {
	stdout, stdoutFound, err = readOutput(root, base+".out", full)
	if err != nil {
		return "", false, "", false, err
	}
	stderr, stderrFound, err = readOutput(root, base+".err", full)
	return stdout, stdoutFound, stderr, stderrFound, err
}

func readOutput(root *os.Root, name string, full bool) (output string, found bool, err error) {
	file, err := root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("open %s: %w", name, err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	if full {
		data, err := io.ReadAll(file)
		if err != nil {
			return "", false, fmt.Errorf("read %s: %w", name, err)
		}
		return string(data), true, nil
	}

	info, err := file.Stat()
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", name, err)
	}
	start := max(info.Size()-int64(maxOutputBytes), 0)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", false, fmt.Errorf("seek %s: %w", name, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxOutputBytes)))
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", name, err)
	}
	output = string(data)
	if start > 0 {
		if idx := strings.IndexByte(output, '\n'); idx >= 0 {
			output = output[idx+1:]
		}
	}
	return tailOutputCap(output), true, nil
}

func (m *Model) clearOutputLoadError(node *StepNode) {
	if strings.HasPrefix(m.loadErr, outputLoadLabel(node)+": ") {
		m.loadErr = ""
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
	rows := m.projectedRows()
	if len(rows) == 0 {
		return
	}
	index := -1
	for i, row := range rows {
		if row.selectable() && row.node == m.selectedNode() {
			index = i
			break
		}
	}
	if index < 0 {
		for _, row := range rows {
			if row.selectable() {
				m.setSelected(row.node)
				return
			}
		}
		return
	}
	for index += delta; index >= 0 && index < len(rows); index += delta {
		if rows[index].selectable() {
			m.setSelected(rows[index].node)
			m.ensureTreeSelectionVisible(index)
			return
		}
	}
}

func (m *Model) ensureTreeSelectionVisible(row int) {
	height := m.bodyHeight()
	if row < m.treeOffset {
		m.treeOffset = row
	} else if row >= m.treeOffset+height {
		m.treeOffset = row - height + 1
	}
	if m.treeOffset < 0 {
		m.treeOffset = 0
	}
}

func (m *Model) handleEsc() (tea.Model, tea.Cmd) {
	if len(m.path) > 1 {
		leaving := m.path[len(m.path)-1]
		m.path = m.path[:len(m.path)-1]
		m.setSelected(leaving)
		m.treeOffset = 0
		m.detailOffset = 0
		m.rebuildDetail()
		return m, nil
	}
	// At top level: show quit-confirm while running, otherwise navigate back.
	if m.running {
		m.quitConfirming = true
		return m, nil
	}
	if m.shouldResumeListOnEsc() {
		return m, func() tea.Msg { return ResumeListMsg{} }
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
	case NodeHeadlessAgent, NodeInteractiveAgent:
		if n.IsContainer() {
			m.path = append(m.path, n)
			m.setSelected(firstRealChild(n.Drilldown()))
			m.treeOffset = 0
			m.detailOffset = 0
			m.rebuildDetail()
			return m, nil
		}
		if m.canResumeAgentSession(n) {
			return m, func() tea.Msg {
				return ResumeMsg{AgentCLI: n.AgentCLI, SessionID: n.SessionID}
			}
		}
		return m, nil

	case NodeLoop, NodeGroup:
		m.path = append(m.path, n)
		m.setSelected(firstRealChild(n.Drilldown()))
		m.treeOffset = 0
		m.detailOffset = 0
		m.rebuildDetail()
		return m, nil

	case NodeSubWorkflow:
		if err := m.tree.EnsureSubWorkflowLoaded(n); err != nil && n.ErrorMessage == "" {
			n.ErrorMessage = err.Error()
		}
		m.path = append(m.path, n)
		m.setSelected(firstRealChild(n.Drilldown()))
		m.treeOffset = 0
		m.detailOffset = 0
		m.rebuildDetail()
		return m, nil

	case NodeIteration:
		target := n.Drilldown()
		if target != n && target.Type == NodeSubWorkflow {
			if err := m.tree.EnsureSubWorkflowLoaded(target); err != nil && target.ErrorMessage == "" {
				target.ErrorMessage = err.Error()
			}
		}
		m.path = append(m.path, n)
		m.setSelected(firstRealChild(n.Drilldown()))
		m.treeOffset = 0
		m.detailOffset = 0
		m.rebuildDetail()
		return m, nil

	case NodeAgentCall:
		if m.canResumeAgentSession(n) {
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
	if n.Type == NodeShell || n.Type == NodeScript || n.Type == NodeHeadlessAgent || n.Type == NodeAgentCall {
		prefix := n.OutputPrefix
		if n.Type == NodeAgentCall {
			prefix = n.CallOutputPrefix
		}
		if m.sessionDir == "" || prefix == "" || m.loadHistoricalOutputFromDisk(n, true) {
			m.loadedFull[n.NodeKey()] = true
		}
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
	m.ensureProjectionContainersLoaded(target)
	m.setSelected(target)
	m.detailOffset = 0
}

// lastTopLevelChild returns the final direct child of root, or nil when root
// has no children. Successful live runs use it to focus the final workflow
// step without opening the optional summary screen.
func lastTopLevelChild(root *StepNode) *StepNode {
	if root == nil || len(root.Children) == 0 {
		return nil
	}
	return root.Children[len(root.Children)-1]
}

// findFailedLeaf returns the deepest non-container StepNode with StatusFailed.
// Equal-depth failures use durable terminal-event order when both candidates
// have it, otherwise traversal preserves workflow order.
func findFailedLeaf(n *StepNode) *StepNode {
	if n == nil {
		return nil
	}
	var best *StepNode
	bestDepth := -1
	var visit func(*StepNode, int)
	visit = func(node *StepNode, depth int) {
		if node == nil {
			return
		}
		for _, child := range node.Children {
			visit(child, depth+1)
		}
		if node.Status == StatusFailed && !node.IsContainer() {
			deeper := depth > bestDepth
			moreRecent := depth == bestDepth && best != nil &&
				node.FailureOrdinal > 0 && best.FailureOrdinal > 0 &&
				node.FailureOrdinal > best.FailureOrdinal
			if deeper || moreRecent {
				best = node
				bestDepth = depth
			}
		}
	}
	visit(n, 0)
	return best
}

func (m *Model) refreshData() {
	// Views opened from the run list or inspect command read called-agent output
	// from files rather than receiving the in-process OutputChunkMsg stream. An
	// in-progress call's file can grow between refreshes, so make those snapshots
	// reloadable before applying the next batch of lifecycle events. Resetting
	// before replay also guarantees one final load when this batch completes the
	// call or the run lock disappears.
	if m.active {
		invalidateInProgressAgentCallOutput(m.tree.Root)
	}
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

func invalidateInProgressAgentCallOutput(node *StepNode) {
	if node == nil {
		return
	}
	if node.Type == NodeAgentCall && node.Status == StatusInProgress {
		node.CallOutputLoaded = false
		node.OutputLoaded = false
	}
	for _, child := range node.Children {
		invalidateInProgressAgentCallOutput(child)
	}
}

func emitBack() tea.Msg { return BackMsg{} }
func emitExit() tea.Msg { return ExitMsg{UserRequested: true} }

func (m *Model) shouldResumeListOnEsc() bool {
	if m.entered == FromInspect || m.entered == FromDefinition {
		return false
	}
	if m.liveResult == "success" || m.liveResult == "failed" {
		return true
	}
	if m.active {
		return false
	}
	if status := m.rootStatus(); status == StatusSuccess || status == StatusFailed {
		return true
	}
	if m.sessionDir == "" {
		return false
	}
	state, err := stateio.ReadState(filepath.Join(m.sessionDir, "state.json"))
	if err != nil {
		return false
	}
	return state.Completed
}

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
