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

// ExecuteCheckStep runs a shell or script check step and, on non-zero exit,
// builds a FailureRecord from the process result plus the scope's guarded
// execution (ctx.LastAgentExecution, or rebuilt from audit when only the
// persisted reference survives an interruption), storing it on
// ctx.LastFailure. Workflows without repair behave exactly as before; this
// only adds richer failure evidence.
func ExecuteCheckStep(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	if step.Command != "" {
		return ExecuteShellStep(step, ctx, runner, log)
	}
	return ExecuteScriptStep(step, ctx, runner, log)
}

// recordCheckFailure updates ctx.LastFailure from a shell or script check's
// process result: on non-zero exit it builds a FailureRecord naming the
// check and its guarded execution (if one ran earlier in this scope); on
// success it clears any earlier failure record from this same check.
func recordCheckFailure(ctx *model.ExecutionContext, step *model.Step, outcome StepOutcome, exitCode int, stdout, stderr string) {
	if outcome != OutcomeFailed {
		ctx.LastFailure = nil
		return
	}
	record := &model.FailureRecord{StepID: step.ID, ExitCode: exitCode, Stdout: stdout, Stderr: stderr}
	record.Guarded = resolveGuardedExecution(ctx)
	ctx.LastFailure = record
}

// resolveGuardedExecution returns the scope's guarded execution, rebuilding
// it from audit when only the persisted reference survived an interruption
// between the agent step and this check (the in-memory record carries no
// response in that case).
func resolveGuardedExecution(ctx *model.ExecutionContext) *model.AgentExecutionRecord {
	guarded := ctx.LastAgentExecution
	if guarded == nil {
		return nil
	}
	if guarded.Response != "" || ctx.SessionDir == "" {
		return guarded
	}
	rebuilt, err := LoadAgentExecution(ctx.SessionDir, guarded.Ref)
	if err != nil {
		return guarded
	}
	return rebuilt
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
		callID, _ := event.Data["call_id"].(string)
		response, _ := event.Data["response"].(string)
		record.CallResponses = append(record.CallResponses, model.CallResponse{CallID: callID, Response: response})
	}
	return record, nil
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
