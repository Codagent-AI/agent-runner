// Package runview builds the data layer for the single-run detail view:
// a step tree merged from workflow YAML and an incremental audit-log tailer.
// This package contains no TUI rendering code.
package runview

import (
	"strconv"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

// NodeType classifies a StepNode for rendering and drill-in decisions.
type NodeType int

// Node type constants.
const (
	NodeRoot NodeType = iota
	NodeShell
	NodeHeadlessAgent
	NodeInteractiveAgent
	NodeLoop
	NodeSubWorkflow
	NodeIteration
	NodeScript
	NodeUI
	NodeGroup
	NodeAgentCall
	NodeParentTurn
)

// NodeStatus is the visual status of a StepNode.
type NodeStatus int

// Status constants.
const (
	StatusPending NodeStatus = iota
	StatusInProgress
	StatusSuccess
	StatusFailed
	StatusWarning
	StatusSkipped
)

// StepNode is one node in the run-view tree. The tree is built from a
// workflow YAML (static structure) and mutated by audit events. Iteration
// nodes are created lazily as iteration_start events arrive. Sub-workflow
// bodies are loaded lazily on sub_workflow_start or on first drill-in.
type StepNode struct {
	ID     string
	Type   NodeType
	Status NodeStatus
	// WarningOrigin marks an explicitly non-blocking terminal failure. It may
	// be a leaf or a loop/container. WarningDescendant distinguishes ancestors
	// that merely contain such an origin.
	WarningOrigin     bool
	WarningDescendant bool
	Parent            *StepNode
	Children          []*StepNode

	// Body is the static template for a loop's inner steps. Used to seed
	// iteration children when iteration_start events arrive; also used as
	// the apparent children of a pending loop iteration the user drills into.
	Body []*StepNode

	// Static fields (populated from workflow YAML).
	StaticCommand            string
	StaticScript             string
	StaticPrompt             string
	StaticAgent              string
	StaticMode               model.StepMode
	StaticCLI                string
	StaticModel              string
	StaticSession            model.SessionStrategy
	StaticWorkflow           string // raw "workflow:" field value, e.g. "../core/implement-task-v1.0.yaml"
	StaticWorkflowPath       string // resolved absolute path (set at lazy-load time for sub-workflows)
	StaticLoopMax            *int
	StaticLoopOver           string
	StaticLoopAs             string
	StaticLoopRequireMatches *bool
	StaticParams             map[string]string // raw template strings from workflow YAML
	StaticSkipIf             string
	StaticBreakIf            string
	StaticWorkdir            string
	StaticContinueOnFailure  bool
	StaticWarnOnFailure      bool
	StaticCaptureStderr      bool
	StaticUITitle            string
	StaticUIBody             string
	StaticUIActions          []model.UIAction
	StaticUIInputs           []model.UIInput

	// Runtime fields (populated from audit events).
	InterpolatedCommand string
	InterpolatedPrompt  string
	InterpolatedParams  map[string]string
	ExitCode            *int
	DurationMs          *int64
	Stdout              string
	Stderr              string
	Outcome             string
	CaptureName         string
	AgentProfile        string
	AgentModel          string
	AgentCLI            string
	SessionID           string
	LoopType            string   // "counted" or "for-each"
	LoopMatches         []string // for-each resolved paths
	IterationsCompleted int
	BreakTriggered      bool
	ErrorMessage        string
	Aborted             bool // aborted mid-execution; UI suppresses blink when no run is active
	Attempts            []AttemptMetrics
	StartedAt           time.Time // wall-clock start of the current in-flight execution (from step_start); zero when not running
	OutputPrefix        string    // audit prefix used for this execution's persisted stdout/stderr files
	OutputLoaded        bool      // bounded persisted output has been read for this historical execution
	// StartOrdinal is the replay-order position of this logical node's latest
	// execution start. It is intentionally in-memory only: replay and live
	// tailing both derive it from the durable audit stream.
	StartOrdinal uint64
	// FailureOrdinal is the replay-order position of this logical node's latest
	// durable failure record. Equal-depth failure selection uses it when both
	// candidates have ordering evidence; zero preserves workflow-order fallback.
	FailureOrdinal uint64
	// TriggeredSkipIf is the recorded skip expression that caused a skipped
	// execution. StaticSkipIf is the configured expression and may differ after
	// interpolation.
	TriggeredSkipIf string

	// Agent-call-only fields. Calls are dynamic executions attached beneath
	// their workflow-authored parent agent; they are never workflow steps.
	CallID             string
	ParentAttemptID    string
	CallTargetKind     string
	CallTargetName     string
	CallRequestID      string
	CallSession        string
	CallSessionResumed bool
	CallCLILaunched    bool
	CallWorkdir        string
	CallUsageError     string
	CallOutputPrefix   string
	CallOutputLoaded   bool
	SummaryParentTurn  *StepNode

	// Iteration-only fields (set when the node is an iteration child of a loop).
	IterationIndex int    // 0-based internal; UI renders as IterationIndex+1
	BindingValue   string // for-each: the matched value bound to loop.As

	// SubLoaded indicates the sub-workflow body has been attached to Children.
	SubLoaded bool

	// AutoFlatten (on loop nodes): the loop's body is a single step with a
	// workflow: field. Iteration nodes inherit FlattenTarget from this flag.
	AutoFlatten bool

	// FlattenTarget (on iteration nodes): when set, drill-in should skip this
	// iteration and enter FlattenTarget's children (the sub-workflow body).
	FlattenTarget *StepNode
}

// AttemptMetrics contains the metrics for one execution of a logical step.
// Runtime display fields on StepNode remain latest-wins, while Attempts is
// append-only so summaries can account for retries and resume sessions.
type AttemptMetrics struct {
	Attempt      int
	Usage        *model.UsageRecord
	CostUSD      *float64
	DurationMs   *int64
	Outcome      string
	AgentInvoked bool // whether this attempt actually launched an agent; gates mid-run coverage denominators
}

// NodeKey returns a stable key for a node based on its structural position in
// the tree. Unlike ID, this disambiguates duplicate step IDs across iterations
// and nested workflows, and unlike pointer identity it survives equivalent tree
// rebuilds.
func (n *StepNode) NodeKey() string {
	if n == nil {
		return ""
	}
	if n.Parent == nil {
		return "root:" + n.ID
	}
	parentKey := n.Parent.NodeKey()
	if n.Type == NodeIteration {
		return parentKey + "/iter:" + strconv.Itoa(n.IterationIndex)
	}
	if n.Type == NodeAgentCall {
		return parentKey + "/call:" + n.CallID
	}
	if n.Type == NodeParentTurn {
		return parentKey + "/parent-turn"
	}
	if idx := indexStepNode(n.Parent.Children, n); idx >= 0 {
		return parentKey + "/child:" + strconv.Itoa(idx) + ":" + n.ID
	}
	if idx := indexStepNode(n.Parent.Body, n); idx >= 0 {
		return parentKey + "/body:" + strconv.Itoa(idx) + ":" + n.ID
	}
	return parentKey + "/detached:" + n.ID
}

func indexStepNode(nodes []*StepNode, target *StepNode) int {
	for i, node := range nodes {
		if node == target {
			return i
		}
	}
	return -1
}

// Tree is the root container for a run-view tree.
type Tree struct {
	Root *StepNode

	// PullRequestURL is the most recently recorded pull-request link from the
	// audit stream. Keeping it with the replayed tree makes historical and live
	// run views use the same update path.
	PullRequestURL string

	// nextStartOrdinal assigns a deterministic replay order to execution
	// starts. It is not persisted because it is reconstructed from audit order.
	nextStartOrdinal uint64
	// nextFailureOrdinal assigns replay order to durable failed-leaf events.
	// It is reconstructed from the same durable audit stream.
	nextFailureOrdinal uint64

	// Warnings contains distinct user-visible warning messages retained from
	// the run's audit stream.
	Warnings []string

	// MetricsCaptured reports whether the audit stream contains the structured
	// metrics fields introduced with run metrics. It stays false for legacy
	// audit logs so those runs retain the pre-metrics viewing experience.
	MetricsCaptured bool

	// RunTotals is populated from the authoritative totals on the latest
	// run_end event. It is nil while a run is active or for legacy audit logs.
	RunTotals *model.RunTotals
	// RunWarningCount is the persisted terminal completion count. Origin nodes
	// remain the source of navigation; this field preserves the summary even
	// when a truncated historical audit cannot reconstruct every leaf.
	RunWarningCount int

	// WorkflowPath is the resolved absolute path of the top-level workflow.
	WorkflowPath string

	// SubWorkflowLoader, if non-nil, is called to load a sub-workflow YAML
	// by resolved absolute path. Defaults to loader.LoadWorkflow at tree
	// construction — tests can inject a fake to avoid disk I/O.
	SubWorkflowLoader func(resolvedPath string) (model.Workflow, error)

	// ParentDirOf returns the directory used to resolve a sub-workflow's
	// relative "workflow:" field. Defaults to filepath.Dir of the parent
	// sub-workflow's StaticWorkflowPath, falling back to WorkflowPath's dir.
	ParentDirOf func(n *StepNode) string
}

// PreviousExecution returns the most recently started terminal leaf before
// selected's latest start. Container lifecycle events are deliberately not
// candidates; calls and skipped leaves are ordinary terminal executions.
func (t *Tree) PreviousExecution(selected *StepNode) *StepNode {
	if t == nil || t.Root == nil || selected == nil || selected.StartOrdinal == 0 {
		return nil
	}
	var previous *StepNode
	var visit func(*StepNode)
	visit = func(node *StepNode) {
		if node == nil {
			return
		}
		if isTerminalExecution(node) && node.StartOrdinal < selected.StartOrdinal &&
			(previous == nil || node.StartOrdinal > previous.StartOrdinal) {
			previous = node
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(t.Root)
	return previous
}

func isTerminalExecution(node *StepNode) bool {
	if node == nil || node.StartOrdinal == 0 || node.IsContainer() {
		return false
	}
	switch node.Type {
	case NodeShell, NodeScript, NodeHeadlessAgent, NodeInteractiveAgent, NodeAgentCall, NodeUI:
		return node.Aborted || node.Status == StatusSuccess || node.Status == StatusWarning || node.Status == StatusFailed || node.Status == StatusSkipped
	default:
		return false
	}
}

// BuildTree constructs a static tree from the top-level workflow.
// workflowPath is the resolved path that produced wf.
func BuildTree(wf *model.Workflow, workflowPath string) *Tree {
	root := &StepNode{
		ID:                 wf.Name,
		Type:               NodeRoot,
		Status:             StatusPending,
		StaticWorkflowPath: workflowPath,
		SubLoaded:          true,
	}
	for i := range wf.Steps {
		child := buildStepNode(&wf.Steps[i], root)
		root.Children = append(root.Children, child)
	}
	return &Tree{
		Root:         root,
		WorkflowPath: workflowPath,
	}
}

// buildStepNode creates a StepNode from a Step, recursively for loops and
// sub-workflow bodies (the latter is left empty for lazy loading).
func buildStepNode(s *model.Step, parent *StepNode) *StepNode {
	n := &StepNode{
		ID:                      s.ID,
		Parent:                  parent,
		Status:                  StatusPending,
		StaticMode:              s.Mode,
		StaticSession:           s.Session,
		StaticSkipIf:            s.SkipIf,
		StaticBreakIf:           s.BreakIf,
		StaticWorkdir:           s.Workdir,
		StaticContinueOnFailure: s.ContinueOnFailure,
		StaticWarnOnFailure:     s.WarnOnFailure,
		StaticCaptureStderr:     s.CaptureStderr,
	}
	switch {
	case s.Command != "":
		n.Type = NodeShell
		n.StaticCommand = s.Command
		n.CaptureName = s.Capture
	case s.Script != "":
		n.Type = NodeScript
		n.StaticScript = s.Script
		n.CaptureName = s.Capture
	case s.Mode == model.ModeUI:
		n.Type = NodeUI
		n.StaticMode = s.Mode
		n.CaptureName = s.Capture
		n.StaticUITitle = s.Title
		n.StaticUIBody = s.Body
		n.StaticUIActions = append([]model.UIAction(nil), s.Actions...)
		n.StaticUIInputs = append([]model.UIInput(nil), s.Inputs...)
	case s.Loop != nil && len(s.Steps) > 0:
		n.Type = NodeLoop
		if s.Loop.Max != nil {
			v := *s.Loop.Max
			n.StaticLoopMax = &v
		}
		n.StaticLoopOver = s.Loop.Over
		n.StaticLoopAs = s.Loop.As
		if s.Loop.RequireMatches != nil {
			v := *s.Loop.RequireMatches
			n.StaticLoopRequireMatches = &v
		}
		for i := range s.Steps {
			n.Body = append(n.Body, buildStepNode(&s.Steps[i], n))
		}
		if len(s.Steps) == 1 && s.Steps[0].Workflow != "" {
			n.AutoFlatten = true
		}
	case s.Workflow != "":
		n.Type = NodeSubWorkflow
		n.StaticWorkflow = s.Workflow
		n.StaticParams = copyParams(s.Params)
		n.CaptureName = s.Capture
	case s.Loop == nil && len(s.Steps) > 0:
		n.Type = NodeGroup
		for i := range s.Steps {
			n.Children = append(n.Children, buildStepNode(&s.Steps[i], n))
		}
	case s.Prompt != "" || s.Agent != "":
		// Agent step. Classify mode statically; audit events may correct it.
		if s.Mode == model.ModeAutonomous {
			n.Type = NodeHeadlessAgent
		} else {
			// Default to interactive when Mode is empty (profile default is
			// resolved at runtime; audit step_start carries the resolved mode).
			n.Type = NodeInteractiveAgent
		}
		n.StaticPrompt = s.Prompt
		n.StaticAgent = s.Agent
		n.StaticMode = s.Mode
		n.StaticCLI = s.CLI
		n.StaticModel = s.Model
		n.CaptureName = s.Capture
	}
	return n
}

func copyParams(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Drilldown returns the node whose children should be displayed when the user
// drills into n. For most containers this is n itself; for auto-flattened
// iterations it returns FlattenTarget.
func (n *StepNode) Drilldown() *StepNode {
	if n == nil {
		return nil
	}
	if n.FlattenTarget != nil {
		return n.FlattenTarget
	}
	return n
}

// IsContainer reports whether the node can be drilled into.
func (n *StepNode) IsContainer() bool {
	if n == nil {
		return false
	}
	switch n.Type {
	case NodeRoot, NodeLoop, NodeSubWorkflow, NodeIteration, NodeGroup:
		return true
	case NodeHeadlessAgent, NodeInteractiveAgent:
		return len(n.Children) > 0
	}
	return false
}

func (n *StepNode) callLabel() string {
	if n == nil || n.Type != NodeAgentCall {
		return ""
	}
	return "call " + n.CallTargetKind + ": " + n.CallTargetName
}

func callChildByID(parent *StepNode, callID string) *StepNode {
	if parent == nil || callID == "" {
		return nil
	}
	for _, child := range parent.Children {
		if child.Type == NodeAgentCall && child.CallID == callID {
			return child
		}
	}
	return nil
}

func (n *StepNode) summaryChildren() []*StepNode {
	if n == nil || (n.Type != NodeHeadlessAgent && n.Type != NodeInteractiveAgent) || len(n.Children) == 0 {
		return nil
	}
	if n.SummaryParentTurn == nil {
		n.SummaryParentTurn = &StepNode{ID: "parent turn", Type: NodeParentTurn, Parent: n}
	}
	turn := n.SummaryParentTurn
	turn.Status = n.Status
	turn.Outcome = n.Outcome
	turn.DurationMs = n.DurationMs
	turn.StartedAt = n.StartedAt
	turn.Aborted = n.Aborted
	turn.Attempts = n.Attempts
	children := make([]*StepNode, 0, len(n.Children)+1)
	children = append(children, turn)
	children = append(children, n.Children...)
	return children
}

// childByID returns the first child whose ID matches, or nil.
func childByID(n *StepNode, id string) *StepNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// findIteration returns the iteration child of a loop matching the given
// 0-based index, or nil if absent.
func findIteration(loop *StepNode, index int) *StepNode {
	if loop == nil {
		return nil
	}
	for _, c := range loop.Children {
		if c.Type == NodeIteration && c.IterationIndex == index {
			return c
		}
	}
	return nil
}

// ensureIteration returns the iteration child at the given index, creating
// it (and seeding its children from the loop's Body template) if necessary.
func ensureIteration(loop *StepNode, index int) *StepNode {
	if existing := findIteration(loop, index); existing != nil {
		return existing
	}
	iter := &StepNode{
		ID:             loop.ID,
		Type:           NodeIteration,
		Status:         StatusPending,
		Parent:         loop,
		IterationIndex: index,
	}
	for _, tpl := range loop.Body {
		iter.Children = append(iter.Children, cloneTemplate(tpl, iter))
	}
	if loop.AutoFlatten && len(iter.Children) == 1 {
		iter.FlattenTarget = iter.Children[0]
	}
	loop.Children = append(loop.Children, iter)
	return iter
}

// cloneTemplate deep-copies a static template subtree (used to seed iteration
// children from a loop's Body). Runtime fields start empty.
func cloneTemplate(src, parent *StepNode) *StepNode {
	dst := &StepNode{
		ID:                      src.ID,
		Type:                    src.Type,
		Status:                  StatusPending,
		Parent:                  parent,
		StaticCommand:           src.StaticCommand,
		StaticScript:            src.StaticScript,
		StaticPrompt:            src.StaticPrompt,
		StaticAgent:             src.StaticAgent,
		StaticMode:              src.StaticMode,
		StaticCLI:               src.StaticCLI,
		StaticModel:             src.StaticModel,
		StaticSession:           src.StaticSession,
		StaticWorkflow:          src.StaticWorkflow,
		StaticWorkflowPath:      src.StaticWorkflowPath,
		StaticLoopOver:          src.StaticLoopOver,
		StaticLoopAs:            src.StaticLoopAs,
		StaticParams:            copyParams(src.StaticParams),
		StaticSkipIf:            src.StaticSkipIf,
		StaticBreakIf:           src.StaticBreakIf,
		StaticWorkdir:           src.StaticWorkdir,
		StaticContinueOnFailure: src.StaticContinueOnFailure,
		StaticCaptureStderr:     src.StaticCaptureStderr,
		StaticUITitle:           src.StaticUITitle,
		StaticUIBody:            src.StaticUIBody,
		StaticUIActions:         append([]model.UIAction(nil), src.StaticUIActions...),
		StaticUIInputs:          append([]model.UIInput(nil), src.StaticUIInputs...),
		CaptureName:             src.CaptureName,
		AutoFlatten:             src.AutoFlatten,
	}
	if src.StaticLoopMax != nil {
		v := *src.StaticLoopMax
		dst.StaticLoopMax = &v
	}
	if src.StaticLoopRequireMatches != nil {
		v := *src.StaticLoopRequireMatches
		dst.StaticLoopRequireMatches = &v
	}
	for _, c := range src.Body {
		dst.Body = append(dst.Body, cloneTemplate(c, dst))
	}
	if src.Type == NodeGroup {
		for _, c := range src.Children {
			dst.Children = append(dst.Children, cloneTemplate(c, dst))
		}
	}
	// Note: Children on a loop template are NOT cloned (iterations are runtime).
	// Children on a sub-workflow template are empty until lazy-load.
	// For shell/agent/sub-workflow steps, Body is empty, so nothing to copy.
	return dst
}

// FindByPrefix locates the StepNode whose audit prefix matches prefix.
// prefix is the full bracketed string produced by audit.BuildPrefix, e.g.
// "[my-step]" or "[loop:0, sub:child, inner]". Returns nil if not found.
func (t *Tree) FindByPrefix(prefix string) *StepNode {
	tokens := parsePrefix(prefix)
	if len(tokens) == 0 {
		return nil
	}
	return t.resolve(tokens, false)
}
