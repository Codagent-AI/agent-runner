//go:build dev_audit

// Package devaudit provides the private, development-only audit lifecycle.
package devaudit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

const lifecycleFileName = "audit-lifecycle.json"

const (
	LaunchReserved  = "reserved"
	LaunchLaunching = "launching"
	LaunchStarted   = "started"
	LaunchFailed    = "failed"
	LaunchCompleted = "completed"
)

// Request is the immutable child-launch request.
type Request struct {
	AuditRunID         string           `json:"audit_run_id"`
	AuditSessionDir    string           `json:"audit_session_dir"`
	SourceSessionDir   string           `json:"source_session_dir"`
	SourceRunID        string           `json:"source_run_id"`
	ExecutionSessionID string           `json:"execution_session_id"`
	Trigger            string           `json:"trigger"`
	SnapshotPath       string           `json:"snapshot_path"`
	ProfileSet         string           `json:"profile_set,omitempty"`
	SourceWorkflow     string           `json:"source_workflow"`
	RunnerSource       SourceProvenance `json:"runner_source"`
	Crosscheck         AgentProvenance  `json:"crosscheck"`
}

// AgentProvenance freezes the resolved definition actually used by audit
// model stages; the profile set name alone is intentionally not sufficient.
type AgentProvenance struct {
	CLI    string `json:"cli,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"reasoning_effort,omitempty"`
}

// SourceProvenance records injected build provenance and the audited checkout
// snapshot. Missing source coverage degrades only the linked audit.
type SourceProvenance struct {
	BuildRoot      string `json:"build_root,omitempty"`
	BuildRevision  string `json:"build_revision,omitempty"`
	BuildDirty     string `json:"build_dirty,omitempty"`
	LaunchRoot     string `json:"launch_root,omitempty"`
	LaunchRevision string `json:"launch_revision,omitempty"`
	LaunchDirty    string `json:"launch_dirty,omitempty"`
	SnapshotPath   string `json:"snapshot_path,omitempty"`
	Verified       bool   `json:"verified"`
	Diagnostic     string `json:"diagnostic,omitempty"`
}

// Link is the durable source-side lifecycle record.
type Link struct {
	AuditRunID         string `json:"audit_run_id"`
	ExecutionSessionID string `json:"execution_session_id"`
	Trigger            string `json:"trigger"`
	State              string `json:"state"`
	SnapshotPath       string `json:"snapshot_path,omitempty"`
	RequestedAt        string `json:"requested_at"`
	StartedAt          string `json:"started_at,omitempty"`
	FailedAt           string `json:"failed_at,omitempty"`
	Warning            string `json:"warning,omitempty"`
}

// Lifecycle is deliberately source-local so an untagged binary can safely
// ignore it without losing its normal run history behavior.
type Lifecycle struct {
	Version     int    `json:"version"`
	SourceRunID string `json:"source_run_id"`
	Links       []Link `json:"links"`
}

func ReadLifecycle(path string) (Lifecycle, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- lifecycle path is derived from a run directory.
	if err != nil {
		return Lifecycle{}, err
	}
	var lifecycle Lifecycle
	if err := json.Unmarshal(data, &lifecycle); err != nil {
		return Lifecycle{}, fmt.Errorf("decode audit lifecycle: %w", err)
	}
	return lifecycle, nil
}

// Coordinator persists a reservation and immutable snapshot while the runner
// still owns the source lock, then asks a launcher to start the detached child.
type Coordinator struct {
	Launcher func(Request) error
	Now      func() time.Time
}

func Eligible(summary runner.PostFinalizationSummary) bool {
	if !summary.TopLevel || summary.ExecutionSessionID == "" {
		return false
	}
	switch summary.Result {
	case runner.ResultSuccess, runner.ResultFailed, runner.ResultStopped:
	default:
		return false
	}
	ref := strings.TrimPrefix(summary.WorkflowFile, "builtin:")
	return strings.HasPrefix(ref, "openspec/") || strings.HasPrefix(ref, "spec-driven/")
}

func (c Coordinator) AfterFinalization(summary runner.PostFinalizationSummary) error {
	if !Eligible(summary) {
		return nil
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	path := filepath.Join(summary.SessionDir, lifecycleFileName)
	lifecycle, err := loadLifecycle(path, summary.RunID)
	if err != nil {
		return err
	}
	link := findLink(lifecycle.Links, summary.ExecutionSessionID, "automatic")
	if link == nil {
		auditID, err := newAuditID()
		if err != nil {
			return err
		}
		snapshot, err := snapshotEvidenceForProject(summary.SessionDir, auditID, summary.WorkingDir)
		if err != nil {
			return c.persistFailure(summary, &lifecycle, auditID, err, now())
		}
		lifecycle.Links = append(lifecycle.Links, Link{
			AuditRunID: auditID, ExecutionSessionID: summary.ExecutionSessionID, Trigger: "automatic",
			State: LaunchReserved, SnapshotPath: snapshot, RequestedAt: now().UTC().Format(time.RFC3339Nano),
		})
		if err := writeLifecycle(path, lifecycle); err != nil {
			return err
		}
		if err := appendSourceLink(summary.SessionDir, lifecycle.Links[len(lifecycle.Links)-1]); err != nil {
			return err
		}
		if err := createAuditRun(summary, &lifecycle.Links[len(lifecycle.Links)-1]); err != nil {
			return c.persistFailure(summary, &lifecycle, auditID, err, now())
		}
		appendLifecycleEvent(summary.SessionDir, audit.EventAuditLaunchRequested, lifecycle.Links[len(lifecycle.Links)-1])
		link = &lifecycle.Links[len(lifecycle.Links)-1]
	}
	if link.State == LaunchLaunching || link.State == LaunchStarted || link.State == LaunchFailed || link.State == LaunchCompleted {
		return nil
	}
	return c.launch(summary, &lifecycle, link, now)
}

func (c Coordinator) launch(summary runner.PostFinalizationSummary, lifecycle *Lifecycle, link *Link, now func() time.Time) error {
	crosscheck, err := resolveCrosscheck(summary)
	if err != nil {
		return c.persistFailure(summary, lifecycle, link.AuditRunID, err, now())
	}
	request := Request{AuditRunID: link.AuditRunID, AuditSessionDir: auditSessionDir(summary.SessionDir, link.AuditRunID), SourceSessionDir: summary.SessionDir, SourceRunID: summary.RunID, ExecutionSessionID: summary.ExecutionSessionID, Trigger: link.Trigger, SnapshotPath: link.SnapshotPath, ProfileSet: summary.ProfileSet, SourceWorkflow: summary.WorkflowFile, RunnerSource: snapshotRunnerSource(link.SnapshotPath), Crosscheck: crosscheck}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "request.json"), request); err != nil {
		return c.persistFailure(summary, lifecycle, link.AuditRunID, err, now())
	}
	if err := sealSnapshot(request.SnapshotPath); err != nil {
		return c.persistFailure(summary, lifecycle, link.AuditRunID, err, now())
	}
	if err := transition(summary, lifecycle, link, LaunchLaunching, "", now()); err != nil {
		return err
	}
	if c.Launcher != nil {
		if err := c.Launcher(request); err != nil {
			return c.persistFailure(summary, lifecycle, link.AuditRunID, err, now())
		}
	}
	if err := transition(summary, lifecycle, link, LaunchStarted, "", now()); err != nil {
		// A durable launching claim prevents a duplicate child on retry.
		return err
	}
	appendLifecycleEvent(summary.SessionDir, audit.EventAuditLaunched, *link)
	return nil
}

func (c Coordinator) persistFailure(summary runner.PostFinalizationSummary, lifecycle *Lifecycle, auditID string, launchErr error, at time.Time) error {
	link := findLink(lifecycle.Links, summary.ExecutionSessionID, "automatic")
	if link == nil {
		lifecycle.Links = append(lifecycle.Links, Link{AuditRunID: auditID, ExecutionSessionID: summary.ExecutionSessionID, Trigger: "automatic", State: LaunchFailed, RequestedAt: at.UTC().Format(time.RFC3339Nano), FailedAt: at.UTC().Format(time.RFC3339Nano), Warning: launchErr.Error()})
		link = &lifecycle.Links[len(lifecycle.Links)-1]
	} else {
		link.State, link.FailedAt, link.Warning = LaunchFailed, at.UTC().Format(time.RFC3339Nano), launchErr.Error()
	}
	if err := writeLifecycle(filepath.Join(summary.SessionDir, lifecycleFileName), *lifecycle); err != nil {
		return err
	}
	_ = appendSourceLink(summary.SessionDir, *link)
	_ = updateAuditState(auditSessionDir(summary.SessionDir, link.AuditRunID), *link, false)
	appendLifecycleEvent(summary.SessionDir, audit.EventAuditLaunchFailed, *link)
	return nil
}

func transition(summary runner.PostFinalizationSummary, lifecycle *Lifecycle, link *Link, state, warning string, at time.Time) error {
	link.State = state
	link.Warning = warning
	stamp := at.UTC().Format(time.RFC3339Nano)
	if state == LaunchStarted {
		link.StartedAt = stamp
	}
	if state == LaunchFailed {
		link.FailedAt = stamp
	}
	// The source and audit mirrors are the preconditions for a durable launch
	// claim. Write them first: if either fails, lifecycle remains reserved and a
	// later finalization attempt can safely retry before any child exists.
	if err := appendSourceLink(summary.SessionDir, *link); err != nil {
		return err
	}
	if err := updateAuditState(auditSessionDir(summary.SessionDir, link.AuditRunID), *link, false); err != nil {
		return err
	}
	return writeLifecycle(filepath.Join(summary.SessionDir, lifecycleFileName), *lifecycle)
}

func updateAuditState(sessionDir string, link Link, completed bool) error {
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		return err
	}
	if state.Audit == nil {
		state.Audit = &model.AuditMetadata{}
	}
	state.Audit.LifecycleState, state.Audit.Warning = link.State, link.Warning
	if completed {
		state.Completed = true
	}
	return stateio.WriteState(&state, sessionDir)
}

func resolveCrosscheck(summary runner.PostFinalizationSummary) (AgentProvenance, error) {
	projectConfig := filepath.Join(summary.WorkingDir, ".agent-runner", "config.yaml")
	override := config.ProfileOverride{}
	if summary.ProfileSet != "" {
		override = config.ProfileOverride{Name: summary.ProfileSet, Origin: config.OriginState}
	}
	profiles, err := config.LoadWithProfile(projectConfig, override)
	if err != nil {
		return AgentProvenance{}, fmt.Errorf("resolve audit profile: %w", err)
	}
	agent, err := profiles.Resolve("crosscheck")
	if err != nil {
		return AgentProvenance{}, fmt.Errorf("resolve crosscheck: %w", err)
	}
	return AgentProvenance{CLI: agent.CLI, Model: agent.Model, Effort: agent.Effort}, nil
}

func loadLifecycle(path, sourceRunID string) (Lifecycle, error) {
	lifecycle, err := ReadLifecycle(path)
	if os.IsNotExist(err) {
		return Lifecycle{Version: 1, SourceRunID: sourceRunID, Links: []Link{}}, nil
	}
	return lifecycle, err
}

func writeLifecycle(path string, lifecycle Lifecycle) error {
	return stateio.WriteJSONAtomic(path, lifecycle)
}

func findLink(links []Link, executionID, trigger string) *Link {
	for index := range links {
		if links[index].ExecutionSessionID == executionID && links[index].Trigger == trigger {
			return &links[index]
		}
	}
	return nil
}

func newAuditID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate audit ID: %w", err)
	}
	return "audit-" + hex.EncodeToString(bytes), nil
}

func snapshotEvidence(sessionDir, auditID string) (string, error) {
	dir := filepath.Join(sessionDir, "audit-snapshots", auditID)
	return snapshotEvidenceAt(sessionDir, dir)
}

// snapshotEvidenceForProject records Git facts while the source run still
// owns its launch boundary. The export is optional evidence: a non-Git source
// remains auditable with explicit unavailable attribution.
func snapshotEvidenceForProject(sessionDir, auditID, projectRoot string) (string, error) {
	dir := filepath.Join(sessionDir, "audit-snapshots", auditID)
	if _, err := snapshotEvidenceAt(sessionDir, dir); err != nil {
		return "", err
	}
	if err := exportGitEvidence(projectRoot, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func snapshotEvidenceAt(sessionDir, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := copyEvidenceTree(sessionDir, dir); err != nil {
		return "", err
	}
	return dir, nil
}

type snapshottedGitCommit struct {
	SHA          string   `json:"sha"`
	Subject      string   `json:"subject"`
	Paths        []string `json:"paths"`
	FilesChanged int64    `json:"files_changed"`
	LinesAdded   int64    `json:"lines_added"`
	LinesDeleted int64    `json:"lines_deleted"`
}

type snapshottedGitEvidence struct {
	Available bool                   `json:"available"`
	Reason    string                 `json:"reason,omitempty"`
	Commits   []snapshottedGitCommit `json:"commits"`
}

const maxSnapshottedGitCommits = 128

func exportGitEvidence(projectRoot, snapshotDir string) error {
	evidence := snapshottedGitEvidence{Commits: []snapshottedGitCommit{}}
	if strings.TrimSpace(projectRoot) == "" {
		evidence.Reason = "project root unavailable"
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	metricData, err := os.ReadFile(filepath.Join(snapshotDir, metrics.FileName)) // #nosec G304 -- fixed run artifact below the launch snapshot.
	if err != nil {
		evidence.Reason = "snapshotted Git boundaries are unavailable"
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(metricData, &artifact); err != nil {
		evidence.Reason = "snapshotted Git boundaries are invalid"
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	shas := map[string]struct{}{}
	for _, step := range artifact.Steps {
		if step.GitEnd == nil {
			continue
		}
		for _, sha := range step.GitEnd.Commits {
			shas[sha] = struct{}{}
		}
	}
	if len(shas) == 0 {
		evidence.Available = true
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	ordered := make([]string, 0, len(shas))
	for sha := range shas {
		ordered = append(ordered, sha)
	}
	sort.Strings(ordered)
	if len(ordered) > maxSnapshottedGitCommits {
		evidence.Reason = fmt.Sprintf("Git boundary commit count exceeds %d", maxSnapshottedGitCommits)
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	args := append([]string{"-C", projectRoot, "log", "--no-walk=sorted", "--format=%H%x09%s", "--"}, ordered...)
	output, err := exec.Command("git", args...).Output() // #nosec G204 -- bounded SHAs are read from snapshotted step boundaries.
	if err != nil {
		evidence.Reason = "git commit metadata unavailable"
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		sha, subject, ok := strings.Cut(line, "\t")
		if !ok || sha == "" {
			continue
		}
		evidence.Commits = append(evidence.Commits, snapshottedGitCommit{SHA: sha, Subject: subject, Paths: []string{}})
	}
	statsArgs := append([]string{"-C", projectRoot, "show", "--no-renames", "--format=%H", "--numstat", "--"}, ordered...)
	stats, err := exec.Command("git", statsArgs...).Output() // #nosec G204 -- same bounded boundary SHAs.
	if err != nil {
		evidence.Available = false
		evidence.Reason = "Git commit statistics unavailable"
		return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
	}
	bySHA := map[string]*snapshottedGitCommit{}
	for i := range evidence.Commits {
		bySHA[evidence.Commits[i].SHA] = &evidence.Commits[i]
	}
	var current *snapshottedGitCommit
	for _, line := range strings.Split(strings.TrimSpace(string(stats)), "\n") {
		if commit, ok := bySHA[line]; ok {
			current = commit
			continue
		}
		fields := strings.Split(line, "\t")
		if current == nil || len(fields) < 3 {
			continue
		}
		var added, deleted int64
		if _, err := fmt.Sscan(fields[0], &added); err != nil {
			evidence.Available = false
			evidence.Reason = "Git commit statistics are invalid"
			return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
		}
		if _, err := fmt.Sscan(fields[1], &deleted); err != nil {
			evidence.Available = false
			evidence.Reason = "Git commit statistics are invalid"
			return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
		}
		current.FilesChanged++
		current.LinesAdded += added
		current.LinesDeleted += deleted
		current.Paths = append(current.Paths, fields[2])
	}
	for i := range evidence.Commits {
		sort.Strings(evidence.Commits[i].Paths)
	}
	evidence.Available = true
	return stateio.WriteJSONAtomic(filepath.Join(snapshotDir, "git-evidence.json"), evidence)
}

func copyEvidenceTree(source, destination string) error {
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() && entry.Name() == "audit-snapshots" {
			return filepath.SkipDir
		}
		if entry.Name() == "lock" {
			return nil
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyIfExists(path, target)
	}); err != nil {
		return fmt.Errorf("copy durable evidence: %w", err)
	}
	return nil
}

func snapshotRunnerSource(snapshotDir string) SourceProvenance {
	provenance := SourceProvenance{BuildRoot: BuildRoot, BuildRevision: BuildRevision, BuildDirty: BuildDirty}
	root := strings.TrimSpace(BuildRoot)
	if root == "" {
		provenance.Diagnostic = "development build did not inject an Agent Runner checkout"
		return provenance
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		provenance.Diagnostic = fmt.Sprintf("resolve injected checkout: %v", err)
		return provenance
	}
	module, err := os.ReadFile(filepath.Join(absRoot, "go.mod")) // #nosec G304 -- injected root is captured by supported local build paths.
	if err != nil || !strings.Contains(string(module), "module github.com/codagent/agent-runner") {
		provenance.Diagnostic = "injected checkout is unavailable or is not the Agent Runner module"
		return provenance
	}
	provenance.LaunchRoot = absRoot
	provenance.LaunchRevision = gitOutput(absRoot, "rev-parse", "HEAD")
	if gitOutput(absRoot, "status", "--porcelain") != "" {
		provenance.LaunchDirty = "true"
	}
	destination := filepath.Join(snapshotDir, "runner-source")
	if err := copySourceTree(absRoot, destination); err != nil {
		provenance.Diagnostic = fmt.Sprintf("snapshot injected checkout: %v", err)
		return provenance
	}
	provenance.SnapshotPath = destination
	provenance.Verified = true
	return provenance
}

func gitOutput(root string, args ...string) string {
	command := exec.Command("git", append([]string{"-C", root}, args...)...) // #nosec G204 -- fixed git provenance argv for injected local checkout.
	data, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func copySourceTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(destination, 0o700)
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "worktrees") {
			return filepath.SkipDir
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if err := copyIfExists(path, target); err != nil {
			return err
		}
		return os.Chmod(target, 0o400)
	})
}

func sealSnapshot(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o500)
		}
		if entry.Type().IsRegular() {
			return os.Chmod(path, 0o400)
		}
		return nil
	})
}

func auditSessionDir(sourceSessionDir, auditID string) string {
	return filepath.Join(filepath.Dir(sourceSessionDir), auditID)
}

func createAuditRun(summary runner.PostFinalizationSummary, link *Link) error {
	dir := auditSessionDir(summary.SessionDir, link.AuditRunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	statePath := filepath.Join(dir, "state.json")
	if _, err := os.Stat(statePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return stateio.WriteState(&model.RunState{
		RunID: link.AuditRunID, WorkflowFile: "builtin:audit/run-audit-v1.0.yaml", WorkflowName: "run-audit", RunKind: "audit",
		Audit: &model.AuditMetadata{SourceRunID: summary.RunID, SourceSessionID: summary.ExecutionSessionID, Trigger: link.Trigger, LifecycleState: LaunchReserved},
	}, dir)
}

func copyIfExists(source, target string) error {
	in, err := os.Open(source) // #nosec G304 -- fixed evidence file beneath a run directory.
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- fixed snapshot file beneath a run directory.
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	return errorsJoin(copyErr, closeErr)
}

func appendSourceLink(sessionDir string, link Link) error {
	path := filepath.Join(sessionDir, "state.json")
	state, err := stateio.ReadState(path)
	if err != nil {
		return err
	}
	if state.Audit == nil {
		state.Audit = &model.AuditMetadata{}
	}
	for index := range state.Audit.Links {
		if state.Audit.Links[index].AuditRunID == link.AuditRunID {
			state.Audit.Links[index] = model.AuditLink{AuditRunID: link.AuditRunID, ExecutionSessionID: link.ExecutionSessionID, Trigger: link.Trigger, State: link.State, SnapshotPath: link.SnapshotPath, RequestedAt: link.RequestedAt, StartedAt: link.StartedAt, FailedAt: link.FailedAt, Warning: link.Warning}
			return stateio.WriteState(&state, sessionDir)
		}
	}
	state.Audit.Links = append(state.Audit.Links, model.AuditLink{AuditRunID: link.AuditRunID, ExecutionSessionID: link.ExecutionSessionID, Trigger: link.Trigger, State: link.State, SnapshotPath: link.SnapshotPath, RequestedAt: link.RequestedAt, StartedAt: link.StartedAt, FailedAt: link.FailedAt, Warning: link.Warning})
	return stateio.WriteState(&state, sessionDir)
}

func appendLifecycleEvent(sessionDir string, typ audit.EventType, link Link) {
	logger, err := audit.NewLogger(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		return
	}
	defer logger.Close()
	logger.Emit(audit.Event{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Type: typ, Data: map[string]any{"audit_run_id": link.AuditRunID, "execution_session_id": link.ExecutionSessionID, "audit_launch_state": link.State, "warning": link.Warning}})
}

func errorsJoin(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
