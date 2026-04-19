package runview

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

type ExitMsg struct{}

// Entered describes how the user reached the run view.
type Entered int

const (
	FromList    Entered = iota
	FromInspect         // read-only post-run inspection
	FromLiveRun         // live workflow execution (runner goroutine is active)
)

// Model is the bubbletea model for the single-run detail view.
type Model struct {
	tree       *Tree
	tailer     FileTailer
	sessionDir string
	projectDir string
	entered    Entered

	path         []*StepNode
	cursor       int
	detailOffset int

	loadedFull map[*StepNode]bool

	active      bool
	pulsePhase  float64
	termWidth   int
	termHeight  int
	detailWidth int
	showLegend  bool
	loadErr     string

	resolverCfg ResolverConfig
	startTime   time.Time

	// Live-run fields (FromLiveRun mode only).
	running          bool   // true until ExecDoneMsg arrives
	quitConfirming   bool   // quit-confirmation modal is visible
	liveResult       string // set on ExecDoneMsg ("success"/"failed"/"stopped")
	autoFollow       bool   // cursor tracks activeStep; enabled by default in FromLiveRun
	tailFollow       bool   // detail pane viewport pinned to tail; enabled by default in FromLiveRun
	activeStepPrefix string // last known active step prefix from StepStateMsg

	// Resume-exec state. When the user selects an agent step after the live
	// run completes, the Model is the top-level tea.Program — there's no
	// switcher to intercept ResumeMsg — so we stash the info here and quit.
	// The CLI wrapper reads it via ResumeAgentCLI/ResumeSessionID after
	// p.Run() returns and execs the agent CLI.
	resumeAgentCLI  string
	resumeSessionID string
}

// ResumeAgentCLI returns the agent CLI name captured from a ResumeMsg in
// live-run mode. Empty when no resume was requested.
func (m *Model) ResumeAgentCLI() string { return m.resumeAgentCLI }

// ResumeSessionID returns the agent CLI session ID captured from a ResumeMsg
// in live-run mode. Empty when no resume was requested.
func (m *Model) ResumeSessionID() string { return m.resumeSessionID }

// New constructs a runview Model from a session directory.
func New(sessionDir, projectDir string, entered Entered) (*Model, error) {
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
		loadedFull: make(map[*StepNode]bool),
		loadErr:    loadErr,
		running:    entered == FromLiveRun,
		autoFollow: entered == FromLiveRun,
		tailFollow: entered == FromLiveRun,
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

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tuistyle.DoRefresh(), tuistyle.DoPulse())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height

	// ---- Live-run messages ----

	case liverun.OutputChunkMsg:
		m.applyOutputChunk(msg)
		if m.tailFollow && m.selectedNode() == m.tree.FindByPrefix(msg.StepPrefix) {
			m.detailOffset = math.MaxInt32
		}
		return m, nil

	case liverun.StepStateMsg:
		m.activeStepPrefix = msg.ActiveStepPrefix
		if m.autoFollow {
			m.navigateToNode(m.tree.FindByPrefix(msg.ActiveStepPrefix))
		}
		return m, nil

	case liverun.SuspendedMsg, liverun.ResumedMsg:
		// Terminal handoff bookkeeping; no visual change needed.
		return m, nil

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
			// Land on the final top-level step so the user sees the
			// workflow's end state. Loop iterations and other deep
			// leaves emit StepStateMsg before their tree nodes exist
			// (audit replay runs lazily), so cursor often gets stuck
			// on the last step whose node was already in the tree —
			// not the actual last step that ran.
			if last := lastTopLevelChild(m.tree.Root); last != nil {
				m.navigateToNode(last)
			}
		}
		return m, nil

	// ---- Keyboard / mouse ----

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.tailFollow = false
			m.scrollDetail(-3)
		case tea.MouseButtonWheelDown:
			m.scrollDetail(3)
		}

	case tuistyle.RefreshMsg:
		// FromLiveRun leaves m.active=false because no runlock is held, but
		// the in-process runner is still emitting audit events we need to
		// pick up so step statuses stay current.
		if m.active || m.running {
			m.refreshData()
		}
		return m, tuistyle.DoRefresh()

	case tuistyle.PulseMsg:
		if m.active || m.running {
			m.pulsePhase += (50.0 / 1000.0) * 2 * math.Pi
		}
		return m, tuistyle.DoPulse()
	}
	return m, nil
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
	case "up", "k":
		m.autoFollow = false
		m.tailFollow = false
		m.moveCursor(-1)
	case "down", "j":
		m.autoFollow = false
		m.tailFollow = false
		m.moveCursor(1)
	case "l":
		m.autoFollow = true
		m.navigateToNode(m.tree.FindByPrefix(m.activeStepPrefix))
	case "pgup":
		m.tailFollow = false
		m.scrollDetail(-m.detailPageSize())
	case "pgdown":
		m.scrollDetail(m.detailPageSize())
	case "end", "G":
		m.tailFollow = true
		m.detailOffset = math.MaxInt32
	case "g":
		m.handleLoadFull()
	}
	return m, nil
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
	m.detailOffset = 0
}

func (m *Model) scrollDetail(delta int) {
	m.detailOffset += delta
	if m.detailOffset < 0 {
		m.detailOffset = 0
	}
}

func (m *Model) detailPageSize() int {
	if m.termHeight <= 6 {
		return 10
	}
	return m.termHeight - 6
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
		m.detailOffset = 0
		return m, nil
	}
	// At top level: show quit-confirm while running, otherwise navigate back.
	if m.running {
		m.quitConfirming = true
		return m, nil
	}
	if m.entered == FromList {
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
		m.detailOffset = 0
		return m, nil

	case NodeSubWorkflow:
		if err := m.tree.EnsureSubWorkflowLoaded(n); err != nil && n.ErrorMessage == "" {
			n.ErrorMessage = err.Error()
		}
		m.path = append(m.path, n)
		m.cursor = 0
		m.detailOffset = 0
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
		m.detailOffset = 0
		return m, nil

	case NodeHeadlessAgent, NodeInteractiveAgent:
		if n.SessionID != "" {
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
		m.loadedFull[n] = true
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
	// Preserve tail-pin while following: resetting to 0 would jump the
	// detail pane to the top until the next OutputChunkMsg re-pins it.
	if m.tailFollow {
		m.detailOffset = math.MaxInt32
	} else {
		m.detailOffset = 0
	}
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
