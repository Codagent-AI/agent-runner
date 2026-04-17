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
	running        bool // true until ExecDoneMsg arrives
	quitConfirming bool // quit-confirmation modal is visible
	liveResult     string // set on ExecDoneMsg ("success"/"failed"/"stopped")
}

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
		return m, nil

	case liverun.StepStateMsg:
		// Bookkeeping only for now; auto-follow is out of scope for this task.
		_ = msg.ActiveStepPrefix
		return m, nil

	case liverun.SuspendedMsg, liverun.ResumedMsg:
		// Terminal handoff bookkeeping; no visual change needed.
		return m, nil

	case liverun.ExecDoneMsg:
		m.running = false
		m.liveResult = msg.Result
		// After completion behave identically to FromInspect.
		return m, nil

	// ---- Keyboard / mouse ----

	case tea.KeyMsg:
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
			return m.handleEsc()
		case "enter":
			return m.handleEnter()
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "pgup":
			m.scrollDetail(-m.detailPageSize())
		case "pgdown":
			m.scrollDetail(m.detailPageSize())
		case "g":
			m.handleLoadFull()
		}

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollDetail(-3)
		case tea.MouseButtonWheelDown:
			m.scrollDetail(3)
		}

	case tuistyle.RefreshMsg:
		if m.active {
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

// applyOutputChunk finds the step matching msg.StepPrefix and appends msg.Bytes
// to its in-memory output buffer. The 2000-line / 256 KB tail-render threshold
// defined in output.go governs what the detail pane shows.
func (m *Model) applyOutputChunk(msg liverun.OutputChunkMsg) {
	node := m.tree.FindByPrefix(msg.StepPrefix)
	if node == nil {
		return
	}
	switch msg.Stream {
	case "stdout":
		node.Stdout += string(msg.Bytes)
	case "stderr":
		node.Stderr += string(msg.Bytes)
	}
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
