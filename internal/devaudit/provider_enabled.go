//go:build dev_audit

package devaudit

import (
	_ "embed"
	"encoding/json"
	"errors"
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
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/runs"
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

// executeAuditWorkflow runs the one injected hidden workflow. Its first
// unavailable stage intentionally fails the linked audit through normal Runner
// execution; completing that diagnostic never changes the source outcome.
func executeAuditWorkflow(request Request) error {
	workflow, err := loader.LoadWorkflow("builtin:audit/run-audit-v1.0.yaml", loader.Options{})
	if err != nil {
		return err
	}
	result, runErr := runner.RunWorkflow(&workflow, map[string]string{"audit_request": filepath.Join(request.AuditSessionDir, "request.json")}, &runner.Options{
		SessionDir: request.AuditSessionDir, WorkflowFile: "builtin:audit/run-audit-v1.0.yaml", WorkingDir: request.AuditSessionDir,
		ProjectRoot: request.AuditSessionDir, ProcessRunner: auditProcessRunner{}, GlobExpander: auditGlobExpander{}, Log: &runner.DiscardLogger{},
	})
	warning := ""
	if runErr != nil {
		warning = runErr.Error()
	} else if result != runner.ResultSuccess {
		warning = "audit stage handlers are not implemented"
	}
	return completeAudit(request, warning)
}

type auditProcessRunner struct{}

func (auditProcessRunner) RunShell(command string, capture bool, workdir string) (iexec.ProcessResult, error) {
	if strings.HasPrefix(command, "audit-stage ") {
		return runAuditStage(strings.TrimSpace(strings.TrimPrefix(command, "audit-stage ")), workdir)
	}
	cmd := exec.Command("sh", "-c", command) // #nosec G204 -- commands are from the injected private audit workflow.
	cmd.Dir = workdir
	var stdout, stderr strings.Builder
	if capture {
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
	}
	err := cmd.Run()
	result := iexec.ProcessResult{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, err
}

func runAuditStage(stage, auditSessionDir string) (iexec.ProcessResult, error) {
	data, err := os.ReadFile(filepath.Join(auditSessionDir, "request.json")) // #nosec G304 -- fixed audit-run request path.
	if err != nil {
		return iexec.ProcessResult{ExitCode: 1, Stderr: err.Error()}, nil
	}
	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		return iexec.ProcessResult{ExitCode: 1, Stderr: fmt.Sprintf("decode audit request: %v", err)}, nil
	}
	var stageErr error
	switch stage {
	case "prepare-evidence":
		_, stageErr = PrepareEvidence(request)
	case "value-audit":
		stageErr = ensureValueOutputs(request)
	case "validate-value":
		stageErr = validateValueStage(request)
	case "correctness-audit":
		stageErr = ensureCorrectnessOutput(request)
	case "validate-publish-correctness":
		stageErr = validatePublishCorrectnessStage(request)
	case "assemble-local-report":
		stageErr = assembleLocalReportStage(request)
	case "report-value-observations":
		stageErr = reportValueObservationsStage(request)
	default:
		stageErr = fmt.Errorf("audit stage %q is not implemented", stage)
	}
	if stageErr != nil {
		if stage == "prepare-evidence" || stage == "value-audit" || stage == "validate-value" {
			_ = stateio.WriteJSONAtomic(filepath.Join(auditSessionDir, "value-diagnostics.json"), map[string]string{"stage": stage, "error": stageErr.Error()})
		}
		return iexec.ProcessResult{ExitCode: 1, Stderr: stageErr.Error()}, nil
	}
	return iexec.ProcessResult{Started: true, ExitCode: 0}, nil
}

func (auditProcessRunner) RunAgent(*iexec.AgentProcessOptions) (iexec.ProcessResult, error) {
	return iexec.ProcessResult{}, errors.New("audit agent stage handler is not implemented")
}

func (auditProcessRunner) RunScript(path string, stdin []byte, capture bool, workdir string) (iexec.ProcessResult, error) {
	return iexec.ProcessResult{}, errors.New("audit script stage handler is not implemented")
}

type auditGlobExpander struct{}

func (auditGlobExpander) Expand(pattern string) ([]string, error) { return filepath.Glob(pattern) }

func completeAudit(request Request, warning string) error {
	lifecyclePath := filepath.Join(request.SourceSessionDir, lifecycleFileName)
	lifecycle, err := ReadLifecycle(lifecyclePath)
	if err != nil {
		return err
	}
	var link *Link
	for index := range lifecycle.Links {
		if lifecycle.Links[index].AuditRunID == request.AuditRunID {
			link = &lifecycle.Links[index]
			break
		}
	}
	if link == nil {
		return fmt.Errorf("audit lifecycle link %q is missing", request.AuditRunID)
	}
	link.State, link.Warning = LaunchCompleted, warning
	if err := writeLifecycle(lifecyclePath, lifecycle); err != nil {
		return err
	}
	if err := appendSourceLink(request.SourceSessionDir, *link); err != nil {
		return err
	}
	if err := completeAuditState(request, *link); err != nil {
		return err
	}
	appendLifecycleEvent(request.SourceSessionDir, audit.EventAuditCompleted, *link)
	return nil
}

func completeAuditState(request Request, link Link) error {
	state, err := stateio.ReadState(filepath.Join(request.AuditSessionDir, "state.json"))
	if err != nil {
		return err
	}
	state.RunKind = "audit"
	state.Audit = &model.AuditMetadata{SourceRunID: request.SourceRunID, SourceSessionID: request.ExecutionSessionID, Trigger: request.Trigger, LifecycleState: link.State, Warning: link.Warning}
	state.Completed = true
	return stateio.WriteState(&state, request.AuditSessionDir)
}

// RecordReportingWarning is available to later delivery stages without
// reopening or changing the source workflow outcome.
func RecordReportingWarning(request Request, warning string) error {
	lifecycle, err := ReadLifecycle(filepath.Join(request.SourceSessionDir, lifecycleFileName))
	if err != nil {
		return err
	}
	for index := range lifecycle.Links {
		if lifecycle.Links[index].AuditRunID == request.AuditRunID {
			lifecycle.Links[index].Warning = warning
			if err := writeLifecycle(filepath.Join(request.SourceSessionDir, lifecycleFileName), lifecycle); err != nil {
				return err
			}
			if err := appendSourceLink(request.SourceSessionDir, lifecycle.Links[index]); err != nil {
				return err
			}
			if err := updateAuditState(request.AuditSessionDir, lifecycle.Links[index], true); err != nil {
				return err
			}
			appendLifecycleEvent(request.SourceSessionDir, audit.EventAuditReportingWarning, lifecycle.Links[index])
			return nil
		}
	}
	return fmt.Errorf("audit lifecycle link %q is missing", request.AuditRunID)
}

func Enabled() bool { return true }

func launchDetached(request Request) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	requestPath := filepath.Join(request.AuditSessionDir, "request.json")
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
		_, _ = fmt.Fprintln(stdout, "Usage: agent-runner audit status <session-dir> | audit replay <session-dir> --session <execution-session-id> | audit setup --client <file> --token <file> --spreadsheet <id> --tab <tab> | audit retry <audit-session-dir>")
		return true, 0
	}
	switch args[1] {
	case "setup":
		return true, handleAuditSetup(args[2:], stdout, stderr)
	case "retry":
		return true, handleAuditRetry(args[2:], stdout, stderr)
	case "status":
		if len(args) != 3 {
			_, _ = fmt.Fprintln(stderr, "agent-runner audit status: expected source session directory")
			return true, 1
		}
		sourceSessionDir, err := resolveRecordedRun(args[2])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner audit status: %v\n", err)
			return true, 1
		}
		lifecycle, err := ReadLifecycle(filepath.Join(sourceSessionDir, lifecycleFileName))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner audit status: %v\n", err)
			return true, 1
		}
		data, _ := json.MarshalIndent(lifecycle, "", "  ")
		_, _ = fmt.Fprintln(stdout, string(data))
		return true, 0
	case "replay":
		if len(args[2:]) == 1 {
			sourceSessionDir, err := resolveRecordedRun(args[2])
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-runner audit replay: %v\n", err)
				return true, 1
			}
			sessions, err := availableExecutionSessions(sourceSessionDir)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-runner audit replay: %v\n", err)
				return true, 1
			}
			_, _ = fmt.Fprintf(stderr, "agent-runner audit replay: execution session is required; available: %s\n", strings.Join(sessions, ", "))
			return true, 1
		}
		sourceRef, sessionID, ok := replayArgs(args[2:])
		if !ok {
			_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit replay <run-id> --session <execution-session-id>")
			return true, 1
		}
		sourceSessionDir, err := resolveRecordedRun(sourceRef)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner audit replay: %v\n", err)
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

func handleAuditRetry(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit retry <audit-session-dir>")
		return 1
	}
	if err := RetryReport(args[0]); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner audit retry: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "agent-runner audit: report delivered")
	return 0
}

func handleAuditSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	client := fs.String("client", "", "installed OAuth client JSON")
	token := fs.String("token", "", "authorized-user OAuth token JSON")
	spreadsheet := fs.String("spreadsheet", "", "existing spreadsheet ID")
	tab := fs.String("tab", "", "existing worksheet tab")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 || *client == "" || *token == "" || *spreadsheet == "" || *tab == "" {
		_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit setup --client <file> --token <file> --spreadsheet <id> --tab <tab>")
		return 1
	}
	if err := (ConnectionStore{}).Import(SetupInput{ClientPath: *client, TokenPath: *token, SpreadsheetID: *spreadsheet, Tab: *tab}); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner audit setup: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "agent-runner audit: Google Sheets connection imported")
	return 0
}

func availableExecutionSessions(sourceSessionDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(sourceSessionDir, metrics.FileName))
	if err != nil {
		return nil, fmt.Errorf("source evidence unavailable: %w", err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("source metrics: %w", err)
	}
	ids := make([]string, 0, len(artifact.Sessions))
	for _, session := range artifact.Sessions {
		ids = append(ids, session.ExecutionSessionID)
	}
	return ids, nil
}

func resolveRecordedRun(ref string) (string, error) {
	if info, err := os.Stat(ref); err == nil && info.IsDir() {
		return filepath.Abs(ref)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	projectDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(cwd))
	entries, err := runs.ListForDir(projectDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.SessionID == ref {
			return entry.SessionDir, nil
		}
	}
	return "", fmt.Errorf("recorded run %q not found", ref)
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
	if err := executeAuditWorkflow(request); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner internal audit-run: %v\n", err)
		return 1
	}
	return 0
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
	workingDir, _ := os.Getwd()
	summary := runner.PostFinalizationSummary{RunID: state.RunID, ExecutionSessionID: executionSessionID, SessionDir: sourceSessionDir, WorkingDir: workingDir, WorkflowFile: state.WorkflowFile, WorkflowName: state.WorkflowName, ProfileSet: state.ProfileSet, TopLevel: true}
	if err := createAuditRun(summary, &link); err != nil {
		return err
	}
	snapshot, err := snapshotEvidenceAt(sourceSessionDir, filepath.Join(auditSessionDir(sourceSessionDir, auditID), "snapshot"))
	if err != nil {
		return err
	}
	if err := exportGitEvidence(summary.WorkingDir, snapshot); err != nil {
		return err
	}
	link.SnapshotPath = snapshot
	lifecycle, err := loadLifecycle(filepath.Join(sourceSessionDir, lifecycleFileName), state.RunID)
	if err != nil {
		return err
	}
	lifecycle.Links = append(lifecycle.Links, link)
	if err := writeLifecycle(filepath.Join(sourceSessionDir, lifecycleFileName), lifecycle); err != nil {
		return err
	}
	if err := appendSourceLink(sourceSessionDir, link); err != nil {
		return err
	}
	appendLifecycleEvent(sourceSessionDir, audit.EventAuditLaunchRequested, link)
	return (Coordinator{Launcher: launch}).launch(summary, &lifecycle, &lifecycle.Links[len(lifecycle.Links)-1], time.Now)
}
