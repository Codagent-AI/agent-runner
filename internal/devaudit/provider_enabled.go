//go:build dev_audit

package devaudit

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// BuildRoot, BuildRevision, and BuildDirty are injected only by supported
// local development build paths. They are diagnostic provenance, never config.
var (
	BuildRoot     string
	BuildRevision string
	BuildDirty    string
)

//go:embed workflows/audit/run-audit-v1.0.yaml
var auditWorkflow []byte

func init() {
	builtinworkflows.RegisterBuiltinAsset("audit/run-audit-v1.0.yaml", auditWorkflow)
	// Package tests exercise the coordinator directly. Registering the default
	// self-exec hook in a test binary would recursively execute that test binary
	// for any eligible fixture, which is neither a production path nor useful
	// lifecycle coverage.
	if strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	runner.SetDefaultPostFinalizationHook(Coordinator{Launcher: launchDetached}.AfterFinalization)
}

func Enabled() bool { return true }

func launchDetached(request Request) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	requestPath := filepath.Join(request.SnapshotPath, "request.json")
	cmd := exec.Command(self, "internal", "audit-run", "--request", requestPath) // #nosec G204 -- self and fixed argv are owned by Agent Runner.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	configureDetachedProcess(cmd.SysProcAttr)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start linked audit: %w", err)
	}
	return nil
}

// HandleCommand owns the tagged-only command surface. It is called before the
// ordinary CLI parser, ensuring production help and command routing contain no
// audit option at all.
func HandleCommand(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) >= 2 && args[0] == "internal" && args[1] == "audit-run" {
		return true, handleInternalAudit(args[2:], stderr)
	}
	if len(args) == 0 || args[0] != "audit" {
		return false, 0
	}
	if len(args) == 1 || args[1] == "help" || args[1] == "--help" {
		_, _ = fmt.Fprintln(stdout, "Usage: agent-runner audit status <session-dir> | audit replay <session-dir> --session <execution-session-id> | audit setup")
		return true, 0
	}
	switch args[1] {
	case "setup":
		_, _ = fmt.Fprintln(stderr, "agent-runner audit setup: reporting integration setup is not implemented in this delivery")
		return true, 1
	case "status":
		if len(args) != 3 {
			_, _ = fmt.Fprintln(stderr, "agent-runner audit status: expected source session directory")
			return true, 1
		}
		lifecycle, err := ReadLifecycle(filepath.Join(args[2], lifecycleFileName))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner audit status: %v\n", err)
			return true, 1
		}
		data, _ := json.MarshalIndent(lifecycle, "", "  ")
		_, _ = fmt.Fprintln(stdout, string(data))
		return true, 0
	case "replay":
		sourceSessionDir, sessionID, ok := replayArgs(args[2:])
		if !ok {
			_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit replay <session-dir> --session <execution-session-id>")
			return true, 1
		}
		if err := Replay(sourceSessionDir, sessionID, launchDetached); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner audit replay: %v\n", err)
			return true, 1
		}
		return true, 0
	default:
		_, _ = fmt.Fprintf(stderr, "agent-runner audit: unknown command %q\n", args[1])
		return true, 1
	}
}

func replayArgs(args []string) (sourceSessionDir, executionSessionID string, ok bool) {
	for index := 0; index < len(args); index++ {
		if args[index] == "--session" && index+1 < len(args) {
			executionSessionID = strings.TrimSpace(args[index+1])
			args = append(append([]string{}, args[:index]...), args[index+2:]...)
			break
		}
	}
	if len(args) != 1 || executionSessionID == "" {
		return "", "", false
	}
	return args[0], executionSessionID, true
}

func handleInternalAudit(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("internal audit-run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	requestPath := fs.String("request", "", "immutable audit request")
	if err := fs.Parse(args); err != nil || *requestPath == "" {
		_, _ = fmt.Fprintln(stderr, "agent-runner internal audit-run: --request is required")
		return 1
	}
	data, err := os.ReadFile(*requestPath) // #nosec G304 -- request path was created by the parent process.
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner internal audit-run: %v\n", err)
		return 1
	}
	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner internal audit-run: invalid request: %v\n", err)
		return 1
	}
	if err := finishDiagnosticAudit(request); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner internal audit-run: %v\n", err)
		return 1
	}
	return 1
}

// finishDiagnosticAudit preserves the explicit stage contract until its
// handlers arrive. It mutates only the linked audit state, never the source.
func finishDiagnosticAudit(request Request) error {
	state, err := stateio.ReadState(filepath.Join(request.AuditSessionDir, "state.json"))
	if err != nil {
		return err
	}
	if state.Audit == nil {
		state.Audit = &model.AuditMetadata{}
	}
	state.Audit.LifecycleState = "failed"
	state.Audit.Warning = "audit stage handlers are not implemented"
	state.Completed = true
	if err := stateio.WriteState(&state, request.AuditSessionDir); err != nil {
		return err
	}
	logger, err := audit.NewLogger(filepath.Join(request.AuditSessionDir, "audit.log"))
	if err == nil {
		logger.Emit(audit.Event{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Type: audit.EventAuditCompleted, Data: map[string]any{"outcome": "failed", "diagnostic": state.Audit.Warning}})
		logger.Close()
	}
	if request.Trigger == "automatic" {
		appendLifecycleEvent(request.SourceSessionDir, audit.EventAuditCompleted, Link{AuditRunID: request.AuditRunID, ExecutionSessionID: request.ExecutionSessionID, Trigger: request.Trigger, State: "completed", Warning: state.Audit.Warning})
	}
	return nil
}

// Replay creates a new append-only audit identity for exactly one durable
// execution session; it never starts or resumes the source workflow.
func Replay(sourceSessionDir, executionSessionID string, launch func(Request) error) error {
	state, err := stateio.ReadState(filepath.Join(sourceSessionDir, "state.json"))
	if err != nil {
		return err
	}
	metricData, err := os.ReadFile(filepath.Join(sourceSessionDir, metrics.FileName)) // #nosec G304 -- fixed file under source run directory.
	if err != nil {
		return fmt.Errorf("source evidence unavailable: %w", err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(metricData, &artifact); err != nil {
		return fmt.Errorf("source metrics: %w", err)
	}
	found := false
	for _, session := range artifact.Sessions {
		if session.ExecutionSessionID == executionSessionID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("execution session %q is unavailable", executionSessionID)
	}
	auditID, err := newAuditID()
	if err != nil {
		return err
	}
	link := Link{AuditRunID: auditID, ExecutionSessionID: executionSessionID, Trigger: "replay", State: LaunchReserved, RequestedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	summary := runner.PostFinalizationSummary{RunID: state.RunID, ExecutionSessionID: executionSessionID, SessionDir: sourceSessionDir, WorkflowFile: state.WorkflowFile, WorkflowName: state.WorkflowName, ProfileSet: state.ProfileSet, TopLevel: true}
	if err := createAuditRun(summary, &link); err != nil {
		return err
	}
	snapshot, err := snapshotEvidenceAt(sourceSessionDir, filepath.Join(auditSessionDir(sourceSessionDir, auditID), "snapshot"))
	if err != nil {
		return err
	}
	link.SnapshotPath = snapshot
	request := Request{AuditRunID: auditID, AuditSessionDir: auditSessionDir(sourceSessionDir, auditID), SourceSessionDir: sourceSessionDir, SourceRunID: state.RunID, ExecutionSessionID: executionSessionID, Trigger: "replay", SnapshotPath: snapshot, ProfileSet: state.ProfileSet, SourceWorkflow: state.WorkflowFile, RunnerSource: snapshotRunnerSource(snapshot)}
	if err := stateio.WriteJSONAtomic(filepath.Join(snapshot, "request.json"), request); err != nil {
		return err
	}
	if err := launch(request); err != nil {
		return err
	}
	return nil
}
