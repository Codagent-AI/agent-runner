// Package runview builds the data layer for the single-run detail view:
// a step tree merged from workflow YAML and an incremental audit-log tailer.
// This package contains no TUI rendering code.
package runview

import (
	"strconv"

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
)

// NodeStatus is the visual status of a StepNode.
type NodeStatus int

// Status constants.
const (
	StatusPending NodeStatus = iota
	StatusInProgress
	StatusSuccess
	StatusFailed
	StatusSkipped
)

// StepNode is one node in the run-view tree. The tree is built from a
// workflow YAML (static structure) and mutated by audit events. Iteration
// nodes are created lazily as iteration_start events arrive. Sub-workflow
// bodies are loaded lazily on sub_workflow_start or on first drill-in.
type StepNode struct {
	ID       string
	Type     NodeType
	Status   NodeStatus
	Parent   *StepNode
	Children []*StepNode

	// Body is the static template for a loop's inner steps. Used to seed
	// iteration children when iteration_start events arrive; also used as
	// the apparent children of a pending loop iteration the user drills into.
	Body []*StepNode

	// Static fields (populated from workflow YAML).
	StaticCommand            string
	StaticPrompt             string
	StaticAgent              string
	StaticMode               model.StepMode
	StaticCLI                string
	StaticModel              string
	StaticSession            model.SessionStrategy
	StaticWorkflow           string // raw "workflow:" field value, e.g. "../core/implement-task.yaml"
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
	StaticCaptureStderr      bool

	// Runtime fields (populated from audit events).
	InterpolatedCommand string
	InterpolatedPrompt  string
	InterpolatedParams  map[string]string
	ExitCode            *int
	DurationMs          *int64
	Stdout              string
	Stderr              string
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
		StaticSession:           s.Session,
		StaticSkipIf:            s.SkipIf,
		StaticBreakIf:           s.BreakIf,
		StaticWorkdir:           s.Workdir,
		StaticContinueOnFailure: s.ContinueOnFailure,
		StaticCaptureStderr:     s.CaptureStderr,
	}
	switch {
	case s.Command != "":
		n.Type = NodeShell
		n.StaticCommand = s.Command
		n.CaptureName = s.Capture
	case s.Script != "":
		n.Type = NodeScript
		n.StaticCommand = s.Script
		n.CaptureName = s.Capture
	case s.Mode == model.ModeUI:
		n.Type = NodeUI
		n.StaticMode = s.Mode
		n.CaptureName = s.Capture
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
	case s.Prompt != "" || s.Agent != "":
		// Agent step. Classify mode statically; audit events may correct it.
		if s.Mode == model.ModeHeadless {
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

// IsContainer reports whether the node can be drilled into: loops,
// sub-workflows, iterations, and the root all qualify.
func (n *StepNode) IsContainer() bool {
	if n == nil {
		return false
	}
	switch n.Type {
	case NodeRoot, NodeLoop, NodeSubWorkflow, NodeIteration:
		return true
	}
	return false
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
