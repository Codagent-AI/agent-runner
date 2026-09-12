package exec

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
)

func TestRepairBlocked(t *testing.T) {
	cases := []struct {
		name     string
		response string
		want     bool
	}{
		{"exact marker", "REPAIR_BLOCKED", true},
		{"marker on last non-empty line", "I looked into it.\n\nREPAIR_BLOCKED", true},
		{"marker with trailing blank lines", "REPAIR_BLOCKED\n\n\n", true},
		{"marker not on last line", "REPAIR_BLOCKED\nAll done.", false},
		{"no marker", "Fixed the problem.", false},
		{"empty response", "", false},
		{"marker as substring only", "Not REPAIR_BLOCKED right now", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repairBlocked(tc.response); got != tc.want {
				t.Fatalf("repairBlocked(%q) = %v, want %v", tc.response, got, tc.want)
			}
		})
	}
}

func TestBuildRepairEvidenceBlock(t *testing.T) {
	block := buildRepairEvidenceBlock("stdout stuff", "stderr stuff", "agent response")

	if !strings.Contains(block, "<repair-evidence>") || !strings.Contains(block, "</repair-evidence>") {
		t.Fatalf("expected evidence tags, got: %s", block)
	}
	if !strings.Contains(block, "stdout stuff") || !strings.Contains(block, "stderr stuff") || !strings.Contains(block, "agent response") {
		t.Fatalf("expected evidence values present, got: %s", block)
	}
	if !strings.Contains(block, "untrusted data") {
		t.Fatalf("expected untrusted-input notice, got: %s", block)
	}
}

func TestExecuteCheckStepInlineRepairRecoversFailedCheck(t *testing.T) {
	auditLog := &mockAuditLogger{}
	ctx := makeCtx()
	ctx.AuditLogger = auditLog
	maxAttempts := 1
	step := model.Step{
		ID: "verify-draft-pr", Command: "check-pr",
		Repair: &model.Repair{Prompt: "please fix it", Agent: "implementor", Max: &maxAttempts},
	}
	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 1, Stderr: "no open PR"},                     // first check run: fails
		{ExitCode: 0, Stdout: claudeUsageOutput("fixed it", 0)}, // inline repair agent
		{ExitCode: 0, Stdout: "pr opened"},                      // rerun check: passes
	}}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected success, got %q", outcome)
	}
	if ctx.RepairFrame != nil {
		t.Fatalf("expected frame cleared on success, got %+v", ctx.RepairFrame)
	}
	if ctx.LastFailure != nil {
		t.Fatalf("expected no failure record on success, got %+v", ctx.LastFailure)
	}

	starts := findAuditEvents(auditLog.events, audit.EventStepStart)
	ends := findAuditEvents(auditLog.events, audit.EventStepEnd)
	checkStarts, checkEnds := 0, 0
	for _, e := range starts {
		if e.Prefix == "[verify-draft-pr]" {
			checkStarts++
		}
	}
	for _, e := range ends {
		if e.Prefix == "[verify-draft-pr]" {
			checkEnds++
		}
	}
	if checkStarts != 1 || checkEnds != 1 {
		t.Fatalf("expected exactly one step_start/step_end for the check, got starts=%d ends=%d", checkStarts, checkEnds)
	}

	attemptStart := findAuditEvent(auditLog.events, audit.EventRepairAttemptStart)
	if attemptStart.Data["attempt"] != 1 || attemptStart.Data["form"] != "inline" {
		t.Fatalf("repair_attempt_start data = %+v", attemptStart.Data)
	}
	// The run view renders "(repaired N/M)" from audit alone, so the budget
	// must be durable on the attempt event.
	if attemptStart.Data["max"] != 1 {
		t.Fatalf("repair_attempt_start missing budget: %+v", attemptStart.Data)
	}
	attemptEnd := findAuditEvent(auditLog.events, audit.EventRepairAttemptEnd)
	if attemptEnd.Data["attempt"] != 1 || attemptEnd.Data["outcome"] != "success" {
		t.Fatalf("repair_attempt_end data = %+v", attemptEnd.Data)
	}

	var finalEnd *audit.Event
	for i := range ends {
		if ends[i].Prefix == "[verify-draft-pr]" {
			finalEnd = &ends[i]
		}
	}
	if finalEnd == nil {
		t.Fatal("expected a step_end for the owning check")
	}
	if finalEnd.Data["repair_attempts"] != 1 || finalEnd.Data["repair_blocked"] != false {
		t.Fatalf("final step_end repair data = %+v", finalEnd.Data)
	}
}

func TestExecuteCheckStepInlineRepairExhaustsBudget(t *testing.T) {
	auditLog := &mockAuditLogger{}
	ctx := makeCtx()
	ctx.AuditLogger = auditLog
	maxAttempts := 3
	step := model.Step{
		ID: "check", Command: "check-it",
		Repair: &model.Repair{Prompt: "fix it", Agent: "implementor", Max: &maxAttempts},
	}
	results := []ProcessResult{{ExitCode: 1, Stderr: "still broken"}}
	for range 3 {
		results = append(results,
			ProcessResult{ExitCode: 0, Stdout: claudeUsageOutput("tried", 0)},
			ProcessResult{ExitCode: 1, Stderr: "still broken"},
		)
	}
	runner := &mockRunner{results: results}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeFailed {
		t.Fatalf("expected failed, got %q", outcome)
	}
	if ctx.RepairFrame == nil || ctx.RepairFrame.Phase != model.RepairPhaseFailed {
		t.Fatalf("expected frame retained with phase failed, got %+v", ctx.RepairFrame)
	}
	if ctx.LastFailure == nil || ctx.LastFailure.RepairAttempts != 3 {
		t.Fatalf("expected 3 repair attempts recorded, got %+v", ctx.LastFailure)
	}

	attemptEnds := findAuditEvents(auditLog.events, audit.EventRepairAttemptEnd)
	if len(attemptEnds) != 3 {
		t.Fatalf("expected 3 repair_attempt_end events, got %d", len(attemptEnds))
	}
}

func TestExecuteCheckStepGuardedBlockedBeforeAnyRepair(t *testing.T) {
	auditLog := &mockAuditLogger{}
	ctx := makeCtx()
	ctx.AuditLogger = auditLog
	ctx.LastAgentExecution = &model.AgentExecutionRecord{
		Ref: model.ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1}, Response: "no PR could be opened.\nREPAIR_BLOCKED",
	}
	maxAttempts := 3
	step := model.Step{
		ID: "verify-draft-pr", Command: "check-pr",
		Repair: &model.Repair{Prompt: "fix it", Agent: "implementor", Max: &maxAttempts},
	}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stderr: "no open PR"}}}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeFailed {
		t.Fatalf("expected failed, got %q", outcome)
	}
	if ctx.LastFailure == nil || !ctx.LastFailure.Blocked {
		t.Fatalf("expected blocked failure record, got %+v", ctx.LastFailure)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected no repair to run, got %d calls", len(runner.calls))
	}
	blocked := findAuditEvent(auditLog.events, audit.EventRepairBlocked)
	if blocked == nil {
		t.Fatal("expected a repair_blocked audit event")
	}
	end := findAuditEvent(auditLog.events, audit.EventStepEnd)
	if end.Data["repair_blocked"] != true {
		t.Fatalf("expected repair_blocked=true on step_end, got %+v", end.Data)
	}
}

func TestExecuteCheckStepInlineRepairAgentDeclaresBlocked(t *testing.T) {
	auditLog := &mockAuditLogger{}
	ctx := makeCtx()
	ctx.AuditLogger = auditLog
	maxAttempts := 3
	step := model.Step{
		ID: "check", Command: "check-it",
		Repair: &model.Repair{Prompt: "fix it", Agent: "implementor", Max: &maxAttempts},
	}
	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 1, Stderr: "broken"},
		{ExitCode: 0, Stdout: claudeUsageOutput("cannot fix this.\nREPAIR_BLOCKED", 0)},
	}}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeFailed {
		t.Fatalf("expected failed, got %q", outcome)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected no further attempts after blocked, got %d calls", len(runner.calls))
	}
	if ctx.LastFailure == nil || !ctx.LastFailure.Blocked {
		t.Fatalf("expected blocked failure record, got %+v", ctx.LastFailure)
	}
}

func TestExecuteCheckStepMarkerOnPassingCheckHasNoEffect(t *testing.T) {
	ctx := makeCtx()
	ctx.LastAgentExecution = &model.AgentExecutionRecord{
		Ref: model.ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1}, Response: "done.\nREPAIR_BLOCKED",
	}
	maxAttempts := 1
	step := model.Step{
		ID: "verify-draft-pr", Command: "check-pr",
		Repair: &model.Repair{Prompt: "fix it", Agent: "implementor", Max: &maxAttempts},
	}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess {
		t.Fatalf("ExecuteCheckStep() = (%q, %v), want success", outcome, err)
	}
}

// TestExecuteCheckStepInlineRepairResolvesProfileWithoutExplicitSession
// covers the real invocation path (a configured *config.Config profile
// store, as every actual run has via PrepareRun's config.LoadWithProfile
// fallback): the inline repair block only names "agent" (repair.validate
// forbids "session: new" for repair, directing callers to "agent" for a
// fresh session instead), so the synthesized repair agent step must resolve
// its profile as session:new, not fall through to the resume/inherit branch
// (which requires a session-originating step that a synthesized step never
// has).
func TestExecuteCheckStepInlineRepairResolvesProfileWithoutExplicitSession(t *testing.T) {
	ctx := makeCtx()
	ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{
		"implementor": {CLI: "claude"},
	}}
	maxAttempts := 1
	step := model.Step{
		ID: "verify", Command: "exit 1",
		Repair: &model.Repair{Prompt: "please fix it", Agent: "implementor", Max: &maxAttempts},
	}
	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 1, Stderr: "no open PR"},
		{ExitCode: 0, Stdout: claudeUsageOutput("fixed it", 0)},
		{ExitCode: 0, Stdout: "pr opened"},
	}}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected success, got %q", outcome)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("expected 3 process calls (check, repair agent, rerun check), got %d: %v", len(runner.calls), runner.calls)
	}
}

func TestExecuteCheckStepWithoutRepairDelegatesUnchanged(t *testing.T) {
	ctx := makeCtx()
	step := model.Step{ID: "s", Command: "echo hi"}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}

	outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess {
		t.Fatalf("ExecuteCheckStep() = (%q, %v), want success", outcome, err)
	}
}

func findAuditEvents(events []audit.Event, t audit.EventType) []audit.Event {
	var out []audit.Event
	for _, e := range events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}
