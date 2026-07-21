package runview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// RawEvent is a parsed audit log line, decoupled from audit.Event so this
// package doesn't need to share event type constants at the JSON layer.
// Type is kept as a plain string for flexibility.
type RawEvent struct {
	Timestamp string
	Prefix    string // full bracketed form including the [ and ]; empty for root-scoped events
	Type      string
	Data      map[string]any
}

// ParseLine parses one audit log line (without its trailing newline) into a
// RawEvent. Returns an error on malformed lines so callers can skip them.
//
// Line format (produced by internal/audit.Logger):
//
//	TIMESTAMP [prefix-tokens] TYPE JSON-DATA    (with prefix)
//	TIMESTAMP TYPE JSON-DATA                    (root-scoped)
func ParseLine(line string) (RawEvent, error) {
	var ev RawEvent
	if line == "" {
		return ev, errors.New("empty line")
	}
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return ev, fmt.Errorf("no space after timestamp: %q", line)
	}
	ev.Timestamp = line[:sp]
	rest := line[sp+1:]

	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return ev, fmt.Errorf("unclosed prefix: %q", line)
		}
		ev.Prefix = rest[:end+1]
		rest = strings.TrimLeft(rest[end+1:], " ")
	}

	sp = strings.IndexByte(rest, ' ')
	var rawData string
	if sp < 0 {
		ev.Type = rest
	} else {
		ev.Type = rest[:sp]
		rawData = rest[sp+1:]
	}

	ev.Data = map[string]any{}
	if rawData != "" {
		if err := json.Unmarshal([]byte(rawData), &ev.Data); err != nil {
			return ev, fmt.Errorf("decode data: %w", err)
		}
	}
	return ev, nil
}

// prefixToken is one comma-separated unit from a nesting prefix.
type prefixToken struct {
	stepID    string
	iteration *int
	subName   string
	callID    string
}

// parsePrefix splits a bracketed prefix like "[task-loop:2, verify, sub:verify-task, check]"
// into its component tokens. An empty input returns nil.
func parsePrefix(prefix string) []prefixToken {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimPrefix(prefix, "[")
	prefix = strings.TrimSuffix(prefix, "]")
	if prefix == "" {
		return nil
	}
	parts := strings.Split(prefix, ", ")
	tokens := make([]prefixToken, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "sub:") {
			tokens = append(tokens, prefixToken{subName: strings.TrimPrefix(p, "sub:")})
			continue
		}
		if strings.HasPrefix(p, "call:") {
			tokens = append(tokens, prefixToken{callID: strings.TrimPrefix(p, "call:")})
			continue
		}
		if colon := strings.LastIndexByte(p, ':'); colon > 0 {
			if n, err := strconv.Atoi(p[colon+1:]); err == nil {
				v := n
				tokens = append(tokens, prefixToken{stepID: p[:colon], iteration: &v})
				continue
			}
		}
		tokens = append(tokens, prefixToken{stepID: p})
	}
	return tokens
}

// Tailer consumes bytes from an audit log, buffering partial trailing lines
// between calls. It is not goroutine-safe.
type Tailer struct {
	buffer []byte
}

// Apply reads all available bytes from r, parses every \n-terminated line as
// a RawEvent, and invokes onEvent for each. Partial (no trailing \n) bytes
// are carried forward internally; they will be re-tried on the next Apply
// once more bytes arrive. Returns the number of bytes read from r.
func (t *Tailer) Apply(r io.Reader, onEvent func(RawEvent)) (int, error) {
	data, err := io.ReadAll(r)
	consumed := len(data)
	if err != nil && !errors.Is(err, io.EOF) {
		return consumed, err
	}
	if consumed == 0 && len(t.buffer) == 0 {
		return 0, nil
	}

	t.buffer = append(t.buffer, data...)
	combined := t.buffer
	t.buffer = nil

	start := 0
	for {
		nl := bytes.IndexByte(combined[start:], '\n')
		if nl < 0 {
			break
		}
		line := string(combined[start : start+nl])
		start += nl + 1
		if line == "" {
			continue
		}
		if ev, perr := ParseLine(line); perr == nil {
			if onEvent != nil {
				onEvent(ev)
			}
		}
	}
	if start < len(combined) {
		t.buffer = append([]byte(nil), combined[start:]...)
	}
	return consumed, nil
}

// FileTailer wraps Tailer with a file offset so a caller can repeatedly read
// new bytes from a single audit.log file without rereading the whole thing.
type FileTailer struct {
	offset int64
	Tailer
}

// Offset returns the current byte offset within the file.
func (f *FileTailer) Offset() int64 { return f.offset }

// ReadSince reads any bytes appended to audit.log under sessionDir since the
// last call, parses complete lines into RawEvents, and returns them. Missing
// or empty audit logs return (nil, nil) with no error. Truncation (file size
// less than previous offset) is treated as a restart: offset and partial
// buffer are reset and the full file is re-read.
func (f *FileTailer) ReadSince(sessionDir string) ([]RawEvent, error) {
	path := filepath.Join(sessionDir, "audit.log")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if info.Size() < f.offset {
		f.offset = 0
		f.buffer = nil
	}
	if info.Size() == f.offset && len(f.buffer) == 0 {
		return nil, nil
	}

	file, err := os.Open(path) // #nosec G304 -- audit log path derived from session dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	if f.offset > 0 {
		if _, err := file.Seek(f.offset, io.SeekStart); err != nil {
			return nil, err
		}
	}

	var events []RawEvent
	consumed, err := f.Apply(file, func(e RawEvent) {
		events = append(events, e)
	})
	f.offset += int64(consumed)
	return events, err
}

func filterAuditEventsForWorkflowState(events []RawEvent, workflowHash string, root *StepNode, currentStepID string, runCompleted bool) []RawEvent {
	if workflowHash == "" || len(events) == 0 {
		return events
	}

	currentIndex := childIndexByID(root, currentStepID)
	segmentMatches := true
	filtered := make([]RawEvent, 0, len(events))
	for _, event := range events {
		if event.Type == "run_start" {
			hash, _ := stringField(event.Data, "workflow_hash")
			segmentMatches = hash == "" || hash == workflowHash
			if segmentMatches {
				filtered = append(filtered, event)
			}
			continue
		}
		if segmentMatches {
			if currentIndex >= 0 && !runCompleted && (event.Type == "run_end" || eventIsAfterCurrentTopLevelStep(event, root, currentIndex)) {
				continue
			}
			filtered = append(filtered, event)
			continue
		}
		if eventIsBeforeCurrentTopLevelStep(event, root, currentIndex) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func eventIsBeforeCurrentTopLevelStep(event RawEvent, root *StepNode, currentIndex int) bool {
	if currentIndex <= 0 {
		return false
	}
	tokens := parsePrefix(event.Prefix)
	if len(tokens) == 0 || tokens[0].stepID == "" {
		return false
	}
	idx := childIndexByID(root, tokens[0].stepID)
	return idx >= 0 && idx < currentIndex
}

func eventIsAfterCurrentTopLevelStep(event RawEvent, root *StepNode, currentIndex int) bool {
	tokens := parsePrefix(event.Prefix)
	if len(tokens) == 0 || tokens[0].stepID == "" {
		return false
	}
	idx := childIndexByID(root, tokens[0].stepID)
	return idx > currentIndex
}

func childIndexByID(root *StepNode, id string) int {
	if root == nil || id == "" {
		return -1
	}
	for i, child := range root.Children {
		if child.ID == id {
			return i
		}
	}
	return -1
}

// reconstructTopLevelStepsFromAudit recovers the executed portion of a run
// when its workflow file is no longer available. Audit events do not contain
// pending steps, but they carry enough type and lifecycle data to render every
// top-level step that actually started.
func reconstructTopLevelStepsFromAudit(root *StepNode, events []RawEvent) bool {
	if root == nil {
		return false
	}
	for _, event := range events {
		switch event.Type {
		case "step_start", "step_end", "iteration_start", "iteration_end", "sub_workflow_start", "sub_workflow_end", "agent_call_start", "agent_call_end":
		default:
			continue
		}
		tokens := parsePrefix(event.Prefix)
		if len(tokens) == 0 || tokens[0].stepID == "" {
			continue
		}
		if event.Type == "agent_call_start" || event.Type == "agent_call_end" {
			reconstructAgentCallParent(root, tokens)
			continue
		}
		id := tokens[0].stepID
		node := childByID(root, id)
		if node == nil {
			node = &StepNode{ID: id, Type: NodeShell, Status: StatusPending, Parent: root}
			root.Children = append(root.Children, node)
		}
		if nodeType, ok := auditNodeType(event, tokens); ok {
			node.Type = nodeType
		}
	}
	return len(root.Children) > 0
}

func reconstructAgentCallParent(root *StepNode, tokens []prefixToken) {
	if root == nil || len(tokens) < 2 || tokens[len(tokens)-1].callID == "" {
		return
	}
	current := root
	for i, token := range tokens[:len(tokens)-1] {
		switch {
		case token.subName != "":
			current.Type = NodeSubWorkflow
			current.SubLoaded = true
		case token.iteration != nil:
			loop := childByID(current, token.stepID)
			if loop == nil {
				loop = &StepNode{ID: token.stepID, Type: NodeLoop, Status: StatusPending, Parent: current}
				current.Children = append(current.Children, loop)
			}
			current = ensureIteration(loop, *token.iteration)
		default:
			child := childByID(current, token.stepID)
			if child == nil {
				typeHint := NodeGroup
				if i == len(tokens)-2 {
					typeHint = NodeHeadlessAgent
				}
				child = &StepNode{ID: token.stepID, Type: typeHint, Status: StatusPending, Parent: current}
				current.Children = append(current.Children, child)
			}
			current = child
		}
	}
}

func auditNodeType(event RawEvent, tokens []prefixToken) (NodeType, bool) {
	if len(tokens) > 1 {
		switch {
		case tokens[1].subName != "":
			return NodeSubWorkflow, true
		case tokens[0].iteration != nil:
			return NodeLoop, true
		default:
			return NodeGroup, true
		}
	}
	if event.Type == "sub_workflow_start" || event.Type == "sub_workflow_end" {
		return NodeSubWorkflow, true
	}
	if _, ok := event.Data["loop_type"]; ok {
		return NodeLoop, true
	}
	if _, ok := event.Data["command"]; ok {
		return NodeShell, true
	}
	if _, ok := event.Data["script"]; ok {
		return NodeScript, true
	}
	if _, ok := event.Data["title"]; ok {
		return NodeUI, true
	}
	if mode, ok := stringField(event.Data, "mode"); ok {
		switch model.StepMode(mode) {
		case model.ModeAutonomous:
			return NodeHeadlessAgent, true
		case model.ModeInteractive:
			return NodeInteractiveAgent, true
		case model.ModeUI:
			return NodeUI, true
		}
	}
	identity, ok := event.Data["identity"].(map[string]any)
	if !ok {
		return NodeRoot, false
	}
	stepType, _ := stringField(identity, "step_type")
	switch stepType {
	case "shell":
		return NodeShell, true
	case "script":
		return NodeScript, true
	case "ui":
		return NodeUI, true
	case "agent":
		return NodeHeadlessAgent, true
	case "loop":
		return NodeLoop, true
	case "sub-workflow":
		return NodeSubWorkflow, true
	case "group":
		return NodeGroup, true
	default:
		return NodeRoot, false
	}
}

// ApplyEvent mutates the tree according to a single audit event.
// Unknown event types and unresolved prefixes are silently ignored so a
// malformed log doesn't poison the tree.
func (t *Tree) ApplyEvent(e RawEvent) {
	tokens := parsePrefix(e.Prefix)
	if eventCarriesMetrics(e) {
		t.MetricsCaptured = true
	}
	if t.applyAgentCallEvent(e, tokens) {
		return
	}

	switch e.Type {
	case "run_start":
		t.Root.Status = StatusInProgress
		t.Root.Aborted = false
		t.Root.Outcome = ""
		t.Root.ErrorMessage = ""
		t.RunTotals = nil
	case "run_end":
		outcome, _ := stringField(e.Data, "outcome")
		applyOutcome(t.Root, outcome)
		if totals, ok := decodeRunTotals(e.Data); ok {
			t.RunTotals = totals
		}
	case "error":
		target := t.Root
		if len(tokens) > 0 {
			if n := t.resolve(tokens, false); n != nil {
				target = n
			}
		}
		setErrorMessage(target, e.Data)
	case "step_start":
		n := t.resolve(tokens, true)
		if n == nil {
			return
		}
		// Always transition to in-progress — on resume, a step restarted after
		// a prior failed/aborted/skipped/success outcome must lose its stale
		// terminal status so the TUI renders the "running" indicator.
		n.Status = StatusInProgress
		n.Aborted = false
		n.Outcome = ""
		n.StartedAt = parseEventTime(e.Timestamp)
		applyStepStart(n, e.Data)
	case "step_end":
		n := t.resolve(tokens, true)
		if n == nil {
			return
		}
		applyStepEnd(n, e.Data)
	case "iteration_start":
		n := t.resolve(tokens, true)
		if n == nil {
			return
		}
		n.Status = StatusInProgress
		n.Outcome = ""
		n.Aborted = false
		applyIterationStart(n, e.Data)
	case "iteration_end":
		n := t.resolve(tokens, true)
		if n == nil {
			return
		}
		applyIterationEnd(n, e.Data)
	case "sub_workflow_start":
		n := t.resolve(tokens, true)
		if n == nil {
			return
		}
		applySubWorkflowStart(n, e.Data)
	case "sub_workflow_end":
		n := t.resolve(tokens, true)
		if n == nil {
			return
		}
		applySubWorkflowEnd(n, e.Data)
	}
}

func (t *Tree) applyAgentCallEvent(event RawEvent, tokens []prefixToken) bool {
	switch event.Type {
	case "agent_call_start":
		t.applyAgentCallStart(event, tokens)
		return true
	case "agent_call_end":
		t.applyAgentCallEnd(event, tokens)
		return true
	default:
		return false
	}
}

// resolve walks the tree from the root along the given prefix tokens. If
// createIterations is true, missing iteration children are created on the
// fly. sub:NAME tokens trigger lazy loading of the current node's sub-workflow
// body. Returns nil if the path cannot be fully resolved.
func (t *Tree) resolve(tokens []prefixToken, createIterations bool) *StepNode {
	current := t.Root
	for _, tok := range tokens {
		switch {
		case tok.callID != "":
			current = callChildByID(current, tok.callID)
			if current == nil {
				return nil
			}
		case tok.subName != "":
			if err := t.ensureSubWorkflowLoaded(current); err != nil {
				// Lazy-load failure: record on the node so the UI can display it;
				// resolution stays here so the node itself remains targetable, but
				// further descent will yield nil (no children).
				if current.ErrorMessage == "" {
					current.ErrorMessage = err.Error()
				}
			}
			// Stay at the sub-workflow node — its children now hold the body.
		case tok.iteration != nil:
			loop := childByID(current, tok.stepID)
			if loop == nil {
				return nil
			}
			iter := findIteration(loop, *tok.iteration)
			if iter == nil {
				if !createIterations {
					return nil
				}
				iter = ensureIteration(loop, *tok.iteration)
			}
			current = iter
		default:
			child := groupDescendantByID(current, tok.stepID, true)
			if child == nil {
				child = childByID(current, tok.stepID)
			}
			if child == nil {
				child = groupDescendantByID(current, tok.stepID, false)
			}
			if child == nil {
				return nil
			}
			current = child
		}
	}
	return current
}

func (t *Tree) applyAgentCallStart(event RawEvent, tokens []prefixToken) {
	parent, callID := t.resolveAgentCallParent(tokens)
	if parent == nil || callID == "" {
		return
	}
	call := callChildByID(parent, callID)
	if call == nil {
		call = &StepNode{ID: callID, CallID: callID, Type: NodeAgentCall, Status: StatusPending, Parent: parent}
		parent.Children = append(parent.Children, call)
	}
	call.Status = StatusInProgress
	call.Outcome = ""
	call.Aborted = false
	call.StartedAt = parseEventTime(event.Timestamp)
	call.CallOutputPrefix = event.Prefix
	applyAgentCallFields(call, event.Data)
}

func (t *Tree) applyAgentCallEnd(event RawEvent, tokens []prefixToken) {
	parent, callID := t.resolveAgentCallParent(tokens)
	if parent == nil || callID == "" {
		return
	}
	call := callChildByID(parent, callID)
	if call == nil {
		call = &StepNode{ID: callID, CallID: callID, Type: NodeAgentCall, Status: StatusPending, Parent: parent}
		parent.Children = append(parent.Children, call)
	}
	call.CallOutputPrefix = event.Prefix
	applyAgentCallFields(call, event.Data)
	outcome, _ := stringField(event.Data, "outcome")
	applyOutcome(call, outcome)
	if value, ok := intField(event.Data, "exit_code"); ok {
		call.ExitCode = &value
	}
	if value, ok := boolField(event.Data, "cli_launched"); ok {
		call.CallCLILaunched = value
	}
	if value, ok := int64Field(event.Data, "duration_ms"); ok {
		call.DurationMs = &value
	}
	if value, ok := stringField(event.Data, "error"); ok {
		call.ErrorMessage = value
	}
	if value, ok := stringField(event.Data, "usage_error"); ok {
		call.CallUsageError = value
	}
	if value, ok := stringField(event.Data, "resolved_session_id"); ok && value != "" {
		call.SessionID = value
	}
	if value, ok := stringField(event.Data, "discovered_session_id"); ok && value != "" {
		call.SessionID = value
	}
	if !dataCarriesMetrics(event.Data) {
		return
	}
	metrics := AttemptMetrics{Attempt: 1, Outcome: outcome}
	if call.DurationMs != nil {
		value := *call.DurationMs
		metrics.DurationMs = &value
	}
	if identity, ok := event.Data["identity"].(map[string]any); ok {
		if value, found := intField(identity, "attempt"); found && value > 0 {
			metrics.Attempt = value
		}
		if value, found := boolField(identity, "agent_invoked"); found {
			metrics.AgentInvoked = value
		}
	}
	if value, found := boolField(event.Data, "cli_launched"); found {
		metrics.AgentInvoked = value
	}
	if usage, ok := decodeUsageRecord(event.Data); ok {
		metrics.Usage = usage
	}
	if cost, exists := event.Data["estimated_api_cost_usd"]; exists && cost != nil {
		if value, ok := float64FieldValue(cost); ok {
			metrics.CostUSD = &value
		}
	}
	call.Attempts = []AttemptMetrics{metrics}
}

func (t *Tree) resolveAgentCallParent(tokens []prefixToken) (parent *StepNode, callID string) {
	if len(tokens) < 2 || tokens[len(tokens)-1].callID == "" {
		return nil, ""
	}
	return t.resolve(tokens[:len(tokens)-1], true), tokens[len(tokens)-1].callID
}

func applyAgentCallFields(call *StepNode, data map[string]any) {
	if value, ok := stringField(data, "call_id"); ok && value != "" {
		call.CallID = value
		call.ID = value
	}
	if value, ok := stringField(data, "request_id"); ok {
		call.CallRequestID = value
	}
	if value, ok := stringField(data, "parent_attempt_id"); ok {
		call.ParentAttemptID = value
	}
	if value, ok := stringField(data, "target_kind"); ok {
		call.CallTargetKind = value
	}
	if value, ok := stringField(data, "target_name"); ok {
		call.CallTargetName = value
	}
	if value, ok := stringField(data, "prompt"); ok {
		call.InterpolatedPrompt = value
	}
	if value, ok := stringField(data, "profile"); ok {
		call.AgentProfile = value
	}
	if value, ok := stringField(data, "cli"); ok {
		call.AgentCLI = value
	}
	if value, ok := stringField(data, "model"); ok {
		call.AgentModel = value
	}
	if value, ok := stringField(data, "workdir"); ok {
		call.CallWorkdir = value
	}
	if value, ok := stringField(data, "session_strategy"); ok {
		call.CallSession = value
	}
	if value, ok := boolField(data, "session_resumed"); ok {
		call.CallSessionResumed = value
	}
	if value, ok := stringField(data, "resolved_session_id"); ok && value != "" {
		call.SessionID = value
	}
}

// groupDescendantByID adapts the current audit shape for group members. Group
// execution emits a lifecycle event for the group but does not add the group
// ID to each member's prefix, so members must be resolved through the active
// static group subtree. Ambiguous matches are ignored rather than attributed
// to the wrong logical step.
func groupDescendantByID(parent *StepNode, id string, activeOnly bool) *StepNode {
	var match *StepNode
	ambiguous := false
	var visit func(*StepNode)
	visit = func(node *StepNode) {
		if node == nil || ambiguous {
			return
		}
		for _, child := range node.Children {
			if child.Type != NodeGroup {
				continue
			}
			if activeOnly && child.Status != StatusInProgress {
				continue
			}
			if child.ID == id {
				if match != nil && match != child {
					ambiguous = true
					return
				}
				match = child
			}
			for _, member := range child.Children {
				if member.ID == id {
					if match != nil && match != member {
						ambiguous = true
						return
					}
					match = member
				}
			}
			visit(child)
		}
	}
	visit(parent)
	if ambiguous {
		return nil
	}
	return match
}

// EnsureSubWorkflowLoaded is the UI-facing counterpart to the lazy-load done
// by resolve: on Enter into a pending sub-workflow, callers invoke this to
// populate the node's children from the workflow file.
func (t *Tree) EnsureSubWorkflowLoaded(n *StepNode) error {
	return t.ensureSubWorkflowLoaded(n)
}

func (t *Tree) ensureSubWorkflowLoaded(n *StepNode) error {
	if n == nil || n.Type != NodeSubWorkflow || n.SubLoaded {
		return nil
	}
	workflowPath, err := t.resolveSubWorkflowPath(n)
	if err != nil {
		return err
	}
	load := t.SubWorkflowLoader
	if load == nil {
		load = defaultLoadWorkflow
	}
	wf, err := load(workflowPath)
	if err != nil {
		return err
	}
	n.StaticWorkflowPath = workflowPath
	for i := range wf.Steps {
		n.Children = append(n.Children, buildStepNode(&wf.Steps[i], n))
	}
	n.SubLoaded = true
	return nil
}

func defaultLoadWorkflow(path string) (model.Workflow, error) {
	return loader.LoadWorkflow(path, loader.Options{IsSubWorkflow: true})
}

// resolveSubWorkflowPath turns a sub-workflow node's raw "workflow:" field
// into an absolute path, joined against the containing workflow file's dir
// (mirroring exec.resolveWorkflowPath for static tree construction).
//
// Security: the resolved path is checked against a trusted root derived from
// the top-level workflow to prevent path-traversal attacks via malicious
// "../../../etc/passwd" references in untrusted workflow YAML files.
func (t *Tree) resolveSubWorkflowPath(n *StepNode) (string, error) {
	if n == nil || n.StaticWorkflow == "" {
		return "", errors.New("sub-workflow node has no workflow field")
	}
	parentPath := parentWorkflowPath(n, t.WorkflowPath)
	if t.ParentDirOf != nil {
		if d := t.ParentDirOf(n); d != "" {
			parentPath = d + "/placeholder.yaml"
		}
	}
	absPath := loader.ResolveRelativeWorkflowPath(parentPath, n.StaticWorkflow)
	// Builtin workflows are embedded in the binary — skip the filesystem
	// security check since filepath.EvalSymlinks and filepath.Rel cannot
	// operate on the synthetic "builtin:" prefix.
	if builtinworkflows.IsRef(absPath) {
		return absPath, nil
	}
	// Enforce trusted root: the resolved path must not escape the parent of the
	// top-level workflow's containing directory (i.e. the workflows/ root or
	// the repo root for top-level workflows). This prevents a malicious workflow
	// from forcing reads of arbitrary files outside the project tree. We
	// compare real (symlink-resolved) paths so a symlink inside the trusted
	// root cannot be used to point outside of it.
	if t.WorkflowPath != "" {
		trustedRoot := filepath.Dir(filepath.Dir(filepath.Clean(t.WorkflowPath)))
		realTrusted, err := filepath.EvalSymlinks(trustedRoot)
		if err != nil {
			realTrusted = trustedRoot
		}
		realAbs, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			// File may not exist yet; fall back to lexical comparison on the
			// cleaned absolute path.
			realAbs = absPath
		}
		rel, err := filepath.Rel(realTrusted, realAbs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("sub-workflow path %q resolves outside trusted root %q", n.StaticWorkflow, trustedRoot)
		}
	}
	return absPath, nil
}

// parentWorkflowPath walks up the tree to find the nearest ancestor whose
// StaticWorkflowPath is set (root or a loaded sub-workflow) and returns the
// full path. Used by resolveSubWorkflowPath so loader.ResolveRelativeWorkflowPath
// can apply builtin:-aware path joining.
func parentWorkflowPath(n *StepNode, fallback string) string {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.StaticWorkflowPath != "" {
			return p.StaticWorkflowPath
		}
	}
	return fallback
}

// parseEventTime parses an audit-log line timestamp (RFC3339, e.g.
// "2026-07-13T03:23:36Z") into a time.Time. Returns the zero time on any
// parse failure so callers can treat "unknown start" as not-running.
func parseEventTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	// RFC3339Nano parses non-fractional timestamps too (the fractional part is
	// optional), so it subsumes a plain RFC3339 parse.
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t
	}
	return time.Time{}
}

// applyStepStart copies data from a step_start event onto a node.
func applyStepStart(n *StepNode, data map[string]any) {
	if cmd, ok := stringField(data, "command"); ok {
		n.InterpolatedCommand = cmd
	}
	if p, ok := stringField(data, "prompt"); ok {
		n.InterpolatedPrompt = p
	}
	if mode, ok := stringField(data, "mode"); ok {
		switch model.StepMode(mode) {
		case model.ModeAutonomous:
			n.Type = NodeHeadlessAgent
		case model.ModeInteractive:
			n.Type = NodeInteractiveAgent
		}
	}
	if s, ok := stringField(data, "cli"); ok {
		n.AgentCLI = s
	}
	if s, ok := stringField(data, "model"); ok && s != "" {
		n.AgentModel = s
	}
	if s, ok := stringField(data, "resolved_session_id"); ok && s != "" {
		n.SessionID = s
	}
	if s, ok := stringField(data, "loop_type"); ok {
		n.LoopType = s
	}
	if v, ok := intField(data, "max"); ok {
		iv := v
		n.StaticLoopMax = &iv
	}
	if matches, ok := data["resolved_matches"].([]any); ok {
		strs := make([]string, 0, len(matches))
		for _, m := range matches {
			if s, ok := m.(string); ok {
				strs = append(strs, s)
			}
		}
		n.LoopMatches = strs
	}
	if n.Type == NodeLoop {
		preCreateLoopIterations(n)
	}
	// For sub-workflow step_start we only see a context; workflow_name / path
	// arrive on sub_workflow_start.
}

// maxPreCreatedIterations caps pre-allocation of placeholder iteration nodes
// to bound memory/CPU if a workflow declares a pathologically large loop.
// Any remaining iterations beyond this cap are still created on demand when
// their iteration_start event arrives.
const maxPreCreatedIterations = 10000

// preCreateLoopIterations materializes an iteration node for every index the
// loop is known to run, so pending iterations appear in the step list as soon
// as the loop starts. For for-each loops each placeholder is seeded with its
// binding value from LoopMatches; the status stays Pending until an
// iteration_start event arrives and flips it to InProgress. The total is
// clamped to maxPreCreatedIterations to prevent denial-of-service via a
// crafted workflow file.
func preCreateLoopIterations(loop *StepNode) {
	total := 0
	if len(loop.LoopMatches) > 0 {
		total = len(loop.LoopMatches)
	} else if loop.StaticLoopMax != nil {
		total = *loop.StaticLoopMax
	}
	if total < 0 {
		return
	}
	if total > maxPreCreatedIterations {
		total = maxPreCreatedIterations
	}
	for i := 0; i < total; i++ {
		iter := ensureIteration(loop, i)
		if iter.BindingValue == "" && i < len(loop.LoopMatches) {
			iter.BindingValue = loop.LoopMatches[i]
		}
	}
}

// applyStepEnd copies data from a step_end event onto a node.
func applyStepEnd(n *StepNode, data map[string]any) {
	outcome, _ := stringField(data, "outcome")
	applyOutcome(n, outcome)

	if v, ok := intField(data, "exit_code"); ok {
		iv := v
		n.ExitCode = &iv
	}
	if v, ok := int64Field(data, "duration_ms"); ok {
		iv := v
		n.DurationMs = &iv
	}
	if s, ok := stringField(data, "stdout"); ok {
		n.Stdout = s
	}
	if s, ok := stringField(data, "stderr"); ok {
		n.Stderr = s
	}
	if s, ok := stringField(data, "error"); ok {
		n.ErrorMessage = s
	}
	if v, ok := intField(data, "iterations_completed"); ok {
		n.IterationsCompleted = v
	}
	if v, ok := boolField(data, "break_triggered"); ok {
		n.BreakTriggered = v
	}
	if s, ok := stringField(data, "discovered_session_id"); ok && s != "" {
		n.SessionID = s
	}
	if !dataCarriesMetrics(data) {
		return
	}

	attempt := len(n.Attempts) + 1
	agentInvoked := false
	if identity, ok := data["identity"].(map[string]any); ok {
		if v, found := intField(identity, "attempt"); found && v > 0 {
			attempt = v
		}
		if v, found := boolField(identity, "agent_invoked"); found {
			agentInvoked = v
		}
	}
	metrics := AttemptMetrics{Attempt: attempt, Outcome: outcome, AgentInvoked: agentInvoked}
	if n.DurationMs != nil {
		v := *n.DurationMs
		metrics.DurationMs = &v
	}
	if usage, ok := decodeUsageRecord(data); ok {
		metrics.Usage = usage
	}
	if cost, exists := data["estimated_api_cost_usd"]; exists && cost != nil {
		if value, ok := float64FieldValue(cost); ok {
			metrics.CostUSD = &value
		}
	}
	for i, a := range n.Attempts {
		if a.Attempt == attempt {
			n.Attempts[i] = metrics
			return
		}
	}
	n.Attempts = append(n.Attempts, metrics)
}

func eventCarriesMetrics(event RawEvent) bool {
	if event.Type == "run_end" {
		_, ok := event.Data["totals"]
		return ok
	}
	return dataCarriesMetrics(event.Data)
}

func dataCarriesMetrics(data map[string]any) bool {
	for _, key := range []string{"identity", "usage", "estimated_api_cost_usd"} {
		if _, ok := data[key]; ok {
			return true
		}
	}
	return false
}

func decodeUsageRecord(data map[string]any) (*model.UsageRecord, bool) {
	raw, exists := data["usage"]
	if !exists || raw == nil {
		return nil, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var usage model.UsageRecord
	if err := json.Unmarshal(encoded, &usage); err != nil {
		return nil, false
	}
	return &usage, true
}

func decodeRunTotals(data map[string]any) (*model.RunTotals, bool) {
	raw, exists := data["totals"]
	if !exists || raw == nil {
		return nil, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var totals model.RunTotals
	if err := json.Unmarshal(encoded, &totals); err != nil {
		return nil, false
	}
	if totals.Tokens == nil {
		totals.Tokens = model.TokenCounts{}
	}
	return &totals, true
}

func float64FieldValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func applyIterationStart(n *StepNode, data map[string]any) {
	if loopVar, ok := data["loop_var"].(map[string]any); ok {
		// Prefer the statically-known binding name from the parent loop node
		// to avoid non-deterministic map iteration when loop_var has >1 key.
		key := ""
		if n.Parent != nil {
			key = n.Parent.StaticLoopAs
		}
		if key != "" {
			if s, ok := loopVar[key].(string); ok {
				n.BindingValue = s
				return
			}
		}
		// Fallback: single-entry maps are deterministic.
		for _, v := range loopVar {
			if s, ok := v.(string); ok {
				n.BindingValue = s
				break
			}
		}
	}
}

func applyIterationEnd(n *StepNode, data map[string]any) {
	outcome, _ := stringField(data, "outcome")
	applyOutcome(n, outcome)
	if v, ok := int64Field(data, "duration_ms"); ok {
		iv := v
		n.DurationMs = &iv
	}
}

func applySubWorkflowStart(n *StepNode, data map[string]any) {
	// Only set StaticWorkflowPath if ensureSubWorkflowLoaded hasn't already done
	// so: events emit the executor-side (possibly relative) path, while the
	// lazy-load resolves an absolute path. Keeping the absolute one is necessary
	// for descendants' parentWorkflowPath walk to produce an absolute dir.
	if n.StaticWorkflowPath == "" {
		if s, ok := stringField(data, "workflow_path"); ok && s != "" {
			n.StaticWorkflowPath = s
		}
	}
	// The context snapshot carries the resolved (interpolated) params — these
	// are shown in the sub-workflow header. Extract a plain string map.
	if ctx, ok := data["context"].(map[string]any); ok {
		if params, ok := ctx["params"].(map[string]any); ok {
			n.InterpolatedParams = make(map[string]string, len(params))
			for k, v := range params {
				if s, ok := v.(string); ok {
					n.InterpolatedParams[k] = s
				} else {
					n.InterpolatedParams[k] = fmt.Sprint(v)
				}
			}
		}
	}
}

func applySubWorkflowEnd(n *StepNode, data map[string]any) {
	outcome, _ := stringField(data, "outcome")
	applyOutcome(n, outcome)
	if v, ok := int64Field(data, "duration_ms"); ok {
		iv := v
		n.DurationMs = &iv
	}
}

// applyOutcome sets node Status from an audit outcome string. "aborted" sets
// the Aborted flag and leaves Status at in-progress so the UI can render the
// step as still-running (without a blink, when no run is active). Outcomes
// arrive only on end events, so absence means nothing happens here.
func applyOutcome(n *StepNode, outcome string) {
	if outcome != "" {
		n.Outcome = outcome
	}
	switch outcome {
	case "success", "exhausted":
		n.Status = StatusSuccess
	case "failed":
		n.Status = StatusFailed
	case "skipped":
		n.Status = StatusSkipped
	case "aborted":
		n.Status = StatusInProgress
		n.Aborted = true
	}
}

func setErrorMessage(n *StepNode, data map[string]any) {
	if s, ok := stringField(data, "message"); ok {
		n.ErrorMessage = s
		return
	}
	if s, ok := stringField(data, "error"); ok {
		n.ErrorMessage = s
	}
}

// stringField returns a string value from data if present and of type string.
func stringField(data map[string]any, key string) (string, bool) {
	v, ok := data[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func intField(data map[string]any, key string) (int, bool) {
	v, ok := data[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	}
	return 0, false
}

func int64Field(data map[string]any, key string) (int64, bool) {
	v, ok := data[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int:
		return int64(t), true
	case int64:
		return t, true
	}
	return 0, false
}

func boolField(data map[string]any, key string) (val, ok bool) {
	v, found := data[key]
	if !found {
		return false, false
	}
	val, ok = v.(bool)
	return val, ok
}
