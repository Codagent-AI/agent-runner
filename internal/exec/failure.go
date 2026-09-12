package exec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
)

// rawAuditEvent is a minimal decode of one audit.log line, just enough to
// rebuild agent execution evidence. internal/runview.ParseLine performs the
// same parse for the run view, but exec cannot import runview: its tests
// import exec, and internal/exec importing internal/runview back would form
// an import cycle when the runview test binary is built.
type rawAuditEvent struct {
	Prefix string
	Type   string
	Data   map[string]any
}

func parseAuditLine(line string) (rawAuditEvent, bool) {
	var ev rawAuditEvent
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return ev, false
	}
	rest := line[sp+1:]
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return ev, false
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
			return ev, false
		}
	}
	return ev, true
}

// recordCheckFailure updates ctx.LastFailure from a shell or script check's
// process result: on non-zero exit it builds a FailureRecord naming the
// check (by its own audit prefix and attempt) and its guarded execution (if
// one ran earlier in this scope); on success it clears any earlier failure
// record from this same check. prefix and attempt are the check's own audit
// identity, matching the prefix and identity.attempt on its step_end.
//
// Returns an error when a guarded execution reference exists but could not
// be rebuilt from audit: repair evidence would otherwise be materially
// incomplete (an empty guarded response), so the caller must not proceed as
// if nothing was guarding this check.
func recordCheckFailure(ctx *model.ExecutionContext, step *model.Step, outcome StepOutcome, prefix string, attempt, exitCode int, stdout, stderr string, log Logger) error {
	if outcome != OutcomeFailed {
		ctx.LastFailure = nil
		return nil
	}
	record := &model.FailureRecord{
		StepID: step.ID, Prefix: prefix, Attempt: attempt,
		ExitCode: exitCode, Stdout: stdout, Stderr: stderr,
	}
	guarded, err := resolveGuardedExecution(ctx, log)
	if err != nil {
		return err
	}
	record.Guarded = guarded
	ctx.LastFailure = record
	return nil
}

// resolveGuardedExecution returns the scope's guarded execution, rebuilding
// it from audit when only the persisted reference survived an interruption
// between the agent step and this check (the in-memory record carries no
// response in that case). When reconstruction fails, the caller must not
// silently proceed with an empty guarded response: durable evidence for the
// declared guarded execution is missing, which would make any repair attempt
// act on incomplete information.
func resolveGuardedExecution(ctx *model.ExecutionContext, log Logger) (*model.AgentExecutionRecord, error) {
	guarded := ctx.LastAgentExecution
	if guarded == nil {
		return nil, nil
	}
	if guarded.Response != "" || ctx.SessionDir == "" {
		return guarded, nil
	}
	rebuilt, err := LoadAgentExecution(ctx.SessionDir, guarded.Ref)
	if err != nil {
		wrapped := fmt.Errorf("rebuild guarded execution %s attempt %d: %w", guarded.Ref.Prefix, guarded.Ref.Attempt, err)
		if log != nil {
			log.Errorf("agent-runner: %v\n", wrapped)
		}
		return nil, wrapped
	}
	return rebuilt, nil
}

// addGuardedLinkage adds guarded_prefix and guarded_attempt to a failed
// shell/script step_end's data when an agent execution ran earlier in the
// same scope. It is written regardless of whether the step declares repair.
func addGuardedLinkage(data map[string]any, outcome StepOutcome, ctx *model.ExecutionContext) {
	if outcome != OutcomeFailed || ctx.LastAgentExecution == nil {
		return
	}
	data["guarded_prefix"] = ctx.LastAgentExecution.Ref.Prefix
	data["guarded_attempt"] = ctx.LastAgentExecution.Ref.Attempt
}

// LoadAgentExecution rebuilds an AgentExecutionRecord from the audit log at
// sessionDir by exact prefix and attempt: the agent's own step_end (stdout is
// its final response) plus any agent_call_end events nested under its
// prefix (their response field, labeled by call_id).
func LoadAgentExecution(sessionDir string, ref model.ExecutionRef) (*model.AgentExecutionRecord, error) {
	events, err := readAuditEvents(sessionDir)
	if err != nil {
		return nil, err
	}

	var record *model.AgentExecutionRecord
	for _, event := range events {
		if event.Type != "step_end" || event.Prefix != ref.Prefix {
			continue
		}
		identity, _ := event.Data["identity"].(map[string]any)
		if intFromAny(identity["attempt"]) != ref.Attempt {
			continue
		}
		response, _ := event.Data["stdout"].(string)
		record = &model.AgentExecutionRecord{Ref: ref, Response: response}
	}
	if record == nil {
		return nil, fmt.Errorf("no step_end found in audit log for prefix %q attempt %d", ref.Prefix, ref.Attempt)
	}

	callPrefix := strings.TrimSuffix(ref.Prefix, "]") + ", call:"
	for _, event := range events {
		if event.Type != "agent_call_end" || !strings.HasPrefix(event.Prefix, callPrefix) {
			continue
		}
		// parent_execution_attempt attributes this call to one specific
		// parent execution: without it, a resumed repair could pick up call
		// evidence from an earlier or later attempt of the same-prefix step.
		if intFromAny(event.Data["parent_execution_attempt"]) != ref.Attempt {
			continue
		}
		callID, _ := event.Data["call_id"].(string)
		response, _ := event.Data["response"].(string)
		record.CallResponses = append(record.CallResponses, model.CallResponse{CallID: callID, Response: response})
	}
	return record, nil
}

// RestoreLastFailureForResume rebuilds ctx.LastFailure from the audit log for
// an open repair frame just restored on resume. ctx.LastFailure itself is
// never persisted to state.json, but the repair evidence block (the check's
// stdout/stderr and the guarded agent's response) is required again whenever
// a resumed run re-enters the cycle: at the check for the inline and
// checking/repairing phases, and at the rerun target for the replaying
// phase. The check's own stdout/stderr/exit code are rebuilt from the most
// recent repair_attempt_start event under the check's own owning audit
// prefix, when one exists. A check that was blocked on its very first
// failure never reaches the repair budget loop, so no such event exists; in
// that case the output is taken from the check's own most recent failed
// step_end, which carries the same fields. The guarded execution named by
// frame.Guarded is always rebuilt when present, since it is the evidence a
// resumed rerun target's prompt actually needs.
func RestoreLastFailureForResume(ctx *model.ExecutionContext) error {
	frame := ctx.RepairFrame
	if frame == nil {
		return nil
	}
	owningPrefix := auditBuildPrefixForOwningCheck(ctx, frame.CheckID)

	events, err := readAuditEvents(ctx.SessionDir)
	if err != nil {
		return fmt.Errorf("rebuild repair evidence for resumed check %q: %w", frame.CheckID, err)
	}
	record := &model.FailureRecord{StepID: frame.CheckID, Prefix: owningPrefix}
	found := false
	for _, event := range events {
		if event.Type != "repair_attempt_start" || event.Prefix != owningPrefix {
			continue
		}
		found = true
		record.ExitCode = intFromAny(event.Data["exit_code"])
		record.Stdout = stringFromAny(event.Data["stdout"])
		record.Stderr = stringFromAny(event.Data["stderr"])
	}
	if !found {
		for _, event := range events {
			if event.Type != "step_end" || event.Prefix != owningPrefix || stringFromAny(event.Data["outcome"]) != "failed" {
				continue
			}
			record.ExitCode = intFromAny(event.Data["exit_code"])
			record.Stdout = stringFromAny(event.Data["stdout"])
			record.Stderr = stringFromAny(event.Data["stderr"])
		}
	}
	if frame.Guarded != nil {
		guarded, err := LoadAgentExecution(ctx.SessionDir, *frame.Guarded)
		if err != nil {
			return fmt.Errorf("rebuild guarded execution for resumed repair: %w", err)
		}
		record.Guarded = guarded
	}
	ctx.LastFailure = record
	return nil
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func readAuditEvents(sessionDir string) ([]rawAuditEvent, error) {
	path := filepath.Join(sessionDir, "audit.log")
	data, err := os.ReadFile(path) // #nosec G304 -- session dir is Runner-owned
	if err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	var events []rawAuditEvent
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		event, ok := parseAuditLine(line)
		if !ok {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

// ClassifyFailure renders the classified root failure reason for a failing
// check: the step ID, then "failed:", then the first non-empty line of
// stderr (or the exit code when stderr is empty), then a blocked clause and
// a repair-attempts clause when the repair executor recorded them.
func ClassifyFailure(record *model.FailureRecord) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s failed: %s", record.StepID, failureDetail(record))
	if record.Blocked {
		fmt.Fprintf(&sb, "; blocked: %s", firstLine(record.BlockedBy))
	}
	if record.RepairAttempts > 0 {
		fmt.Fprintf(&sb, " after %d repair attempts", record.RepairAttempts)
	}
	return sb.String()
}

func failureDetail(record *model.FailureRecord) string {
	if line := firstLine(record.Stderr); line != "" {
		return line
	}
	return fmt.Sprintf("with exit code %d", record.ExitCode)
}

// firstLine returns the first non-empty (after trimming) line of s, skipping
// any leading blank lines. Returns "" when every line is blank.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
