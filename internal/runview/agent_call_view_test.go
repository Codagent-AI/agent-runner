package runview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func TestAgentCallParentDisplaysCount(t *testing.T) {
	tree := agentCallTestTree()
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))
	tree.ApplyEvent(agentCallStartEvent("call-2", "attempt-1", "agent", "reviewer"))

	m := newTestModel(tree, FromInspect)
	plain := tuistyle.Sanitize(strings.Join(m.buildStepRows(tree.Root.Children), "\n"))
	if !strings.Contains(plain, "parent (2 calls)") {
		t.Fatalf("parent row missing call count:\n%s", plain)
	}
}

func TestExpandedAgentCallParentShowsChronologicalExplicitTargets(t *testing.T) {
	tree := agentCallTestTree()
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "session", "implementor-session"))
	tree.ApplyEvent(agentCallStartEvent("call-2", "attempt-1", "agent", "implementor"))

	m := newTestModel(tree, FromInspect)
	rows := m.buildRenderedStepRows(tree.Root.Children)
	plain := tuistyle.Sanitize(strings.Join(rowTexts(rows), "\n"))
	first := strings.Index(plain, "call session: implementor-session")
	second := strings.Index(plain, "call agent: implementor")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("call rows are missing or out of invocation order:\n%s", plain)
	}
	if strings.Count(plain, "↗") != 2 {
		t.Fatalf("call rows should use the agent-call glyph:\n%s", plain)
	}
}

func TestRepeatedAgentCallTargetsRemainDistinct(t *testing.T) {
	tree := agentCallTestTree()
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))
	tree.ApplyEvent(agentCallStartEvent("call-2", "attempt-1", "agent", "implementor"))

	parent := tree.Root.Children[0]
	if len(parent.Children) != 2 || parent.Children[0] == parent.Children[1] {
		t.Fatalf("repeated calls collapsed: %#v", parent.Children)
	}
	if parent.Children[0].NodeKey() == parent.Children[1].NodeKey() {
		t.Fatalf("repeated calls share a node key: %q", parent.Children[0].NodeKey())
	}
}

func TestAgentCallFailureRemainsIndependentOfSuccessfulParent(t *testing.T) {
	tree := agentCallTestTree()
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))
	tree.ApplyEvent(agentCallEndEvent("call-1", "attempt-1", "agent", "implementor", "failed", false, 25, nil, nil))
	tree.ApplyEvent(stepEndMetricsEvent("parent", 1, 60_000, "success", collectedUsageRecord(10, 2), float64Pointer(0.10)))

	parent := tree.Root.Children[0]
	if parent.Status != StatusSuccess || len(parent.Children) != 1 || parent.Children[0].Status != StatusFailed {
		t.Fatalf("parent/call statuses = %v/%#v, want success/failed", parent.Status, parent.Children)
	}
	plain := tuistyle.Sanitize(strings.Join(rowTexts(newTestModel(tree, FromInspect).buildRenderedStepRows(tree.Root.Children)), "\n"))
	if !strings.Contains(plain, "call agent: implementor") || !strings.Contains(plain, "✗") {
		t.Fatalf("failed launch call disappeared from rows:\n%s", plain)
	}
}

func TestInspectReconstructsAgentCallHierarchyWithoutWorkflow(t *testing.T) {
	events := []RawEvent{
		agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"),
		agentCallEndEvent("call-1", "attempt-1", "agent", "implementor", "success", true, 1000, collectedUsageRecord(3, 1), float64Pointer(0.02)),
	}
	root := &StepNode{ID: "missing-workflow", Type: NodeRoot, Status: StatusPending}
	if !reconstructTopLevelStepsFromAudit(root, events) {
		t.Fatal("audit reconstruction did not recover parent step")
	}
	tree := &Tree{Root: root}
	for _, event := range events {
		tree.ApplyEvent(event)
	}
	if len(root.Children) != 1 || root.Children[0].ID != "parent" || len(root.Children[0].Children) != 1 {
		t.Fatalf("reconstructed hierarchy = %#v", root.Children)
	}
}

func TestInspectReconstructsNestedAgentCallHierarchyWithoutWorkflow(t *testing.T) {
	start := agentCallStartEvent("call-1", "attempt-1", "agent", "implementor")
	start.Prefix = "[outer, sub:child-workflow, parent, call:call-1]"
	root := &StepNode{ID: "missing-workflow", Type: NodeRoot, Status: StatusPending}
	if !reconstructTopLevelStepsFromAudit(root, []RawEvent{start}) {
		t.Fatal("audit reconstruction did not recover nested parent path")
	}
	tree := &Tree{Root: root}
	tree.ApplyEvent(start)
	if len(root.Children) != 1 || len(root.Children[0].Children) != 1 || len(root.Children[0].Children[0].Children) != 1 {
		t.Fatalf("nested reconstructed hierarchy = %#v", root.Children)
	}
	parent := root.Children[0].Children[0]
	if parent.ID != "parent" || parent.Children[0].CallID != "call-1" {
		t.Fatalf("nested call attached to wrong parent: %#v", parent)
	}
}

func TestSelectedAgentCallShowsResolvedExecutionDetail(t *testing.T) {
	tree := agentCallTestTree()
	start := agentCallStartEvent("call-1", "attempt-1", "session", "implementor-session")
	start.Data["prompt"] = "review the implementation"
	start.Data["profile"] = "implementor"
	start.Data["cli"] = "codex"
	start.Data["model"] = "gpt-5"
	start.Data["workdir"] = "/repo/packages/api"
	start.Data["session_strategy"] = "implementor-session"
	start.Data["resolved_session_id"] = "child-session"
	start.Data["session_resumed"] = true
	tree.ApplyEvent(start)
	end := agentCallEndEvent("call-1", "attempt-1", "session", "implementor-session", "failed", true, 1500, collectedUsageRecord(12, 4), float64Pointer(0.08))
	end.Data["exit_code"] = float64(7)
	end.Data["usage_error"] = "usage parser failed"
	tree.ApplyEvent(end)
	call := tree.Root.Children[0].Children[0]
	call.ErrorMessage = "child failed"

	lines, _ := buildLogLines([]*StepNode{call}, nil, 100, map[string]bool{call.NodeKey(): true}, 0, false, ResolverConfig{})
	plain := tuistyle.Sanitize(strings.Join(lines, "\n"))
	for _, want := range []string{
		"call session: implementor-session", "call id: call-1", "request id: request-call-1", "parent attempt: attempt-1",
		"target: session", "profile: implementor", "cli: codex", "model: gpt-5", "cli launched: yes", "exit: 7",
		"session: implementor-session", "session id: child-session", "session resumed: yes", "workdir: /repo/packages/api", "review the implementation",
		"failed", "duration: 1.5s", "input 12", "output 4", "cost: $0.08", "usage error: usage parser failed", "child failed",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("call detail missing %q:\n%s", want, plain)
		}
	}
}

func TestAgentCallLoadsPersistedOutputAndIgnoresAuditResponse(t *testing.T) {
	sessionDir := t.TempDir()
	tree := agentCallTestTree()
	start := agentCallStartEvent("call-1", "attempt-1", "agent", "implementor")
	tree.ApplyEvent(start)
	end := agentCallEndEvent("call-1", "attempt-1", "agent", "implementor", "success", true, 1000, nil, nil)
	end.Data["response"] = "audit response must not render"
	end.Data["stdout"] = "audit stdout must not render"
	end.Data["stderr"] = "audit stderr must not render"
	tree.ApplyEvent(end)

	outputDir := filepath.Join(sessionDir, "output")
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		t.Fatal(err)
	}
	prefix := "[parent, call:call-1]"
	if err := os.WriteFile(filepath.Join(outputDir, sanitizeOutputPrefixForTest(prefix)+".out"), []byte("persisted child stdout"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, sanitizeOutputPrefixForTest(prefix)+".err"), []byte{0xff, 'e', 'r', 'r'}, 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(tree, FromInspect)
	m.sessionDir = sessionDir
	m.path = []*StepNode{tree.Root, tree.Root.Children[0]}
	plain := tuistyle.Sanitize(m.View())
	for _, want := range []string{"persisted child stdout", "�err"} {
		if !strings.Contains(plain, want) {
			t.Errorf("persisted call output missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"audit response must not render", "audit stdout must not render", "audit stderr must not render"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("audit metadata rendered as call output %q:\n%s", unwanted, plain)
		}
	}
}

func TestAgentCallResumeRequiresInactiveSuccessfulKnownSession(t *testing.T) {
	tree := agentCallTestTree()
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))
	tree.ApplyEvent(agentCallEndEvent("call-1", "attempt-1", "agent", "implementor", "success", true, 1000, nil, nil))
	call := tree.Root.Children[0].Children[0]
	call.SessionID = "child-session"
	call.AgentCLI = "codex"
	m := newTestModel(tree, FromInspect)

	if !m.canResumeAgentSession(call) {
		t.Fatal("inactive successful call with session should be resumable")
	}
	m.active = true
	if m.canResumeAgentSession(call) {
		t.Fatal("active run exposed call resume")
	}
	m.active = false
	call.SessionID = ""
	if m.canResumeAgentSession(call) {
		t.Fatal("call without session exposed resume")
	}
	call.SessionID = "child-session"
	call.Status = StatusFailed
	if m.canResumeAgentSession(call) {
		t.Fatal("failed call exposed completed-session resume")
	}
}

func TestLiveAgentCallAppearsStreamsSeparatelyAndAutoFollows(t *testing.T) {
	sessionDir := t.TempDir()
	tree := agentCallTestTree()
	parent := tree.Root.Children[0]
	parent.Status = StatusInProgress
	m := newTestModel(tree, FromLiveRun)
	m.sessionDir = sessionDir
	m.running = true
	m.autoFollow = true
	appendAuditTestEvent(t, sessionDir, agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))

	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[parent, call:call-1]"})
	if len(parent.Children) != 1 || parent.Children[0].Status != StatusInProgress {
		t.Fatalf("accepted live call was not inserted: %#v", parent.Children)
	}
	if len(m.path) != 2 || m.selectedNode() != parent.Children[0] {
		t.Fatalf("auto-follow did not enter call: path=%d selected=%#v", len(m.path), m.selectedNode())
	}
	m.Update(liverun.OutputChunkMsg{StepPrefix: "[parent, call:call-1]", Stream: "stdout", Bytes: []byte("child output")})
	if parent.Children[0].Stdout != "child output" || parent.Stdout != "" {
		t.Fatalf("output attribution parent=%q child=%q", parent.Stdout, parent.Children[0].Stdout)
	}

	appendAuditTestEvent(t, sessionDir, agentCallEndEvent("call-1", "attempt-1", "agent", "implementor", "success", true, 1000, nil, nil))
	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[parent]"})
	if len(m.path) != 1 || m.selectedNode() != parent || parent.Children[0].Status != StatusSuccess || parent.Status != StatusInProgress {
		t.Fatalf("auto-follow did not return to active parent independently: path=%d selected=%#v parent=%v child=%v", len(m.path), m.selectedNode(), parent.Status, parent.Children[0].Status)
	}
}

func TestManualNavigationPausesAgentCallAutoFollow(t *testing.T) {
	tree := agentCallTestTree()
	parent := tree.Root.Children[0]
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))
	m := newTestModel(tree, FromLiveRun)
	m.running = true
	m.autoFollow = false
	m.cursor = 0

	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[parent, call:call-1]"})
	if len(m.path) != 1 || m.selectedNode() != parent {
		t.Fatalf("manual selection was overridden: path=%d selected=%#v", len(m.path), m.selectedNode())
	}
}

func TestExpandedParentSuppressesDuplicateRunningIndicatorForCall(t *testing.T) {
	tree := agentCallTestTree()
	tree.Root.Children[0].Status = StatusInProgress
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "agent", "implementor"))
	m := newTestModel(tree, FromLiveRun)
	m.running = true
	m.pulsePhase = 0

	rows := rowTexts(m.buildRenderedStepRows(tree.Root.Children))
	plain := make([]string, len(rows))
	for i := range rows {
		plain[i] = tuistyle.Sanitize(rows[i])
	}
	if len(plain) != 2 || strings.Contains(plain[0], "●") || !strings.Contains(plain[1], "●") {
		t.Fatalf("running indicators should belong only to call child: %#v", plain)
	}
}

func TestAgentCallSummaryRollsUpExactlyOnceWithoutAddingChildDuration(t *testing.T) {
	tree := agentCallTestTree()
	tree.ApplyEvent(agentCallStartEvent("call-launch", "attempt-1", "agent", "reviewer"))
	launchEnd := agentCallEndEvent("call-launch", "attempt-1", "agent", "reviewer", "failed", false, 25, nil, nil)
	launchEnd.Data["error"] = "launch failed"
	tree.ApplyEvent(launchEnd)
	tree.ApplyEvent(agentCallStartEvent("call-1", "attempt-1", "session", "implementor-session"))
	tree.ApplyEvent(agentCallEndEvent("call-1", "attempt-1", "session", "implementor-session", "failed", true, 30_000, collectedUsageRecord(20, 4), float64Pointer(0.20)))
	tree.ApplyEvent(agentCallStartEvent("call-2", "attempt-2", "agent", "implementor"))
	tree.ApplyEvent(agentCallEndEvent("call-2", "attempt-2", "agent", "implementor", "success", true, 10_000, collectedUsageRecord(5, 1), float64Pointer(0.05)))
	tree.ApplyEvent(stepEndMetricsEvent("parent", 1, 40_000, "failed", collectedUsageRecord(10, 2), float64Pointer(0.10)))
	tree.ApplyEvent(stepEndMetricsEvent("parent", 2, 20_000, "success", collectedUsageRecord(7, 3), float64Pointer(0.07)))
	parent := tree.Root.Children[0]

	metrics := aggregateSummaryMetrics(parent, modelTimeZero())
	if metrics.durationMS != 60_000 {
		t.Fatalf("rollup duration = %d, want parent wall time 60000", metrics.durationMS)
	}
	if metrics.tokens[model.TokenInput] != 42 || metrics.tokens[model.TokenOutput] != 10 || metrics.costUSD != 0.42 {
		t.Fatalf("rollup metrics = tokens %#v cost %.2f, want exact-once parent+calls", metrics.tokens, metrics.costUSD)
	}

	m := newTestModel(tree, FromInspect)
	m.showSummary = true
	m.path = []*StepNode{tree.Root, parent}
	plain := tuistyle.Sanitize(m.renderSummary())
	parentTurn := strings.Index(plain, "parent turn")
	launchCall := strings.Index(plain, "call agent: reviewer")
	firstCall := strings.Index(plain, "call session: implementor-session")
	secondCall := strings.Index(plain, "call agent: implementor")
	if parentTurn < 0 || launchCall <= parentTurn || firstCall <= launchCall || secondCall <= firstCall {
		t.Fatalf("summary drill-down order is wrong:\n%s", plain)
	}
	for _, want := range []string{"parent turn", "1m 0s", "call agent: reviewer", "25ms", "call session: implementor-session", "call agent: implementor", "$0.42", "usage complete"} {
		if !strings.Contains(plain, want) {
			t.Errorf("summary missing %q:\n%s", want, plain)
		}
	}
}

func TestAgentWithoutCallsRemainsSummaryLeaf(t *testing.T) {
	tree := agentCallTestTree()
	parent := tree.Root.Children[0]
	if parent.IsContainer() {
		t.Fatal("agent without calls became a container")
	}
	row := makeSummaryRow(parent, false, modelTimeZero())
	if strings.Contains(row.label, "›") {
		t.Fatalf("agent leaf label = %q", row.label)
	}
}

func agentCallTestTree() *Tree {
	wf := model.Workflow{Name: "calls", Steps: []model.Step{{ID: "parent", Prompt: "use call_agent", Mode: model.ModeAutonomous}}}
	return BuildTree(&wf, "")
}

func agentCallStartEvent(callID, attemptID, targetKind, targetName string) RawEvent {
	return RawEvent{
		Timestamp: "2026-07-20T12:00:00Z", Prefix: "[parent, call:" + callID + "]", Type: "agent_call_start",
		Data: map[string]any{
			"call_id": callID, "request_id": "request-" + callID, "parent_attempt_id": attemptID,
			"prompt": "do child work", "target_kind": targetKind, "target_name": targetName,
			"workdir": "/repo", "profile": "implementor", "cli": "claude", "model": "sonnet",
			"session_strategy": "new", "resolved_session_id": "",
		},
	}
}

func agentCallEndEvent(callID, attemptID, targetKind, targetName, outcome string, invoked bool, duration int64, usage *model.UsageRecord, cost *float64) RawEvent {
	data := map[string]any{
		"call_id": callID, "parent_attempt_id": attemptID, "target_kind": targetKind, "target_name": targetName,
		"outcome": outcome, "duration_ms": float64(duration), "cli_launched": invoked,
		"identity": map[string]any{"attempt": float64(1), "kind": "agent-call", "step_type": "agent", "agent_invoked": invoked},
	}
	if invoked {
		data["exit_code"] = float64(0)
		data["discovered_session_id"] = "child-session"
		data["resolved_session_id"] = "child-session"
	}
	if usage != nil {
		data["usage"] = map[string]any{
			"status": string(usage.Status), "reason": string(usage.Reason), "cli": usage.CLI,
			"tokens": tokenCountsAsAny(usage.Tokens), "source": usage.Source, "completeness": string(usage.Completeness),
		}
	}
	if cost != nil {
		data["estimated_api_cost_usd"] = *cost
	}
	return RawEvent{Timestamp: "2026-07-20T12:00:01Z", Prefix: "[parent, call:" + callID + "]", Type: "agent_call_end", Data: data}
}

func appendAuditTestEvent(t *testing.T, sessionDir string, event RawEvent) {
	t.Helper()
	data, err := json.Marshal(event.Data)
	if err != nil {
		t.Fatal(err)
	}
	line := event.Timestamp + " " + event.Prefix + " " + event.Type + " " + string(data) + "\n"
	f, err := os.OpenFile(filepath.Join(sessionDir, "audit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func modelTimeZero() (zeroTime time.Time) { return zeroTime }

func sanitizeOutputPrefixForTest(prefix string) string {
	var b strings.Builder
	for _, ch := range prefix {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '.' || ch == '-' || ch == '_':
			b.WriteRune(ch)
		case ch == '/':
			b.WriteString("--")
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
