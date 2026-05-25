package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/stateio"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

const debugSummaryCapBytes = 64 * 1024

func routeDebugCommand(args []string, stdout, stderr io.Writer) (handled bool, code int) {
	if len(args) == 0 || args[0] != "debug" {
		return false, 0
	}
	return true, handleDebug(args[1:], stdout, stderr)
}

func handleDebug(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("debug", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateRunID := fs.String("state", "", "Print run state JSON")
	stateSessionDir := fs.String("state-dir", "", "Print run state JSON from a session directory")
	auditRunID := fs.String("audit-summary", "", "Print a redacted audit summary")
	auditSessionDir := fs.String("audit-summary-dir", "", "Print a redacted audit summary from a session directory")
	workflowRef := fs.String("show-workflow", "", "Print workflow YAML")
	stateSet, stateDirSet, auditSet, auditDirSet, workflowSet := debugOpFlags(args)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug: unexpected argument: %s\n", fs.Arg(0))
		return 1
	}

	ops := 0
	for _, set := range []bool{stateSet, stateDirSet, auditSet, auditDirSet, workflowSet} {
		if set {
			ops++
		}
	}
	if ops != 1 {
		_, _ = fmt.Fprintln(stderr, "agent-runner debug: specify exactly one of --state, --state-dir, --audit-summary, --audit-summary-dir, or --show-workflow")
		return 1
	}

	switch {
	case stateSet:
		return debugState(*stateRunID, stdout, stderr)
	case stateDirSet:
		return debugStateDir(*stateSessionDir, stdout, stderr)
	case auditSet:
		return debugAuditSummary(*auditRunID, stdout, stderr)
	case auditDirSet:
		return debugAuditSummaryDir(*auditSessionDir, stdout, stderr)
	default:
		return debugShowWorkflow(*workflowRef, stdout, stderr)
	}
}

func debugOpFlags(args []string) (state, stateDir, auditSummary, auditSummaryDir, showWorkflow bool) {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--state":
			state = true
		case "--state-dir":
			stateDir = true
		case "--audit-summary":
			auditSummary = true
		case "--audit-summary-dir":
			auditSummaryDir = true
		case "--show-workflow":
			showWorkflow = true
		}
	}
	return state, stateDir, auditSummary, auditSummaryDir, showWorkflow
}

func debugState(runID string, stdout, stderr io.Writer) int {
	sessionDir, _, err := resolveInspectSession(runID)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --state: %v\n", err)
		return 1
	}
	return debugStateFromSessionDir(sessionDir, "agent-runner debug --state", stdout, stderr)
}

func debugStateDir(sessionDir string, stdout, stderr io.Writer) int {
	return debugStateFromSessionDir(sessionDir, "agent-runner debug --state-dir", stdout, stderr)
}

func debugStateFromSessionDir(sessionDir, label string, stdout, stderr io.Writer) int {
	sessionDir, err := cleanDebugSessionDir(sessionDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return 1
	}
	statePath := filepath.Join(sessionDir, "state.json")
	if _, err := stateio.ReadState(statePath); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return 1
	}
	data, err := os.ReadFile(statePath) // #nosec G304 -- state path is resolved from a known run session dir.
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: read state: %v\n", label, err)
		return 1
	}
	if _, err := stdout.Write(data); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: write output: %v\n", label, err)
		return 1
	}
	return 0
}

func debugAuditSummary(runID string, stdout, stderr io.Writer) int {
	sessionDir, _, err := resolveInspectSession(runID)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --audit-summary: %v\n", err)
		return 1
	}
	return debugAuditSummaryFromSessionDir(sessionDir, "agent-runner debug --audit-summary", stdout, stderr)
}

func debugAuditSummaryDir(sessionDir string, stdout, stderr io.Writer) int {
	return debugAuditSummaryFromSessionDir(sessionDir, "agent-runner debug --audit-summary-dir", stdout, stderr)
}

func debugAuditSummaryFromSessionDir(sessionDir, label string, stdout, stderr io.Writer) int {
	sessionDir, err := cleanDebugSessionDir(sessionDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return 1
	}
	projectDir, err := debugSessionProjectDir(sessionDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return 1
	}
	auditPath, err := filepath.Abs(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return 1
	}
	file, err := os.Open(auditPath) // #nosec G304 -- audit path is resolved from a known run session dir.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			summary := audit.Summary{
				Path:         auditPath,
				SessionDir:   sessionDir,
				ProjectDir:   projectDir,
				Steps:        []audit.StepBoundary{},
				SubWorkflows: []audit.SubWorkflowBoundary{},
				Failures:     []audit.FailureEvent{},
				Errors:       []audit.ErrorEvent{},
			}
			if err := json.NewEncoder(stdout).Encode(summary); err != nil {
				_, _ = fmt.Fprintf(stderr, "%s: write output: %v\n", label, err)
				return 1
			}
			return 0
		}
		_, _ = fmt.Fprintf(stderr, "%s: open %s: %v\n", label, auditPath, err)
		return 1
	}
	defer func() { _ = file.Close() }()

	summary, err := audit.BuildSummary(file, debugSummaryCapBytes)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return 1
	}
	summary.Path = auditPath
	summary.SessionDir = sessionDir
	summary.ProjectDir = projectDir
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: write output: %v\n", label, err)
		return 1
	}
	return 0
}

func cleanDebugSessionDir(sessionDir string) (string, error) {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return "", fmt.Errorf("session dir is required")
	}
	if strings.ContainsRune(sessionDir, '\x00') {
		return "", fmt.Errorf("session dir contains NUL")
	}
	abs, err := filepath.Abs(sessionDir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("session dir is not a directory: %s", abs)
	}
	return abs, nil
}

func debugSessionProjectDir(sessionDir string) (string, error) {
	runsDir := filepath.Dir(sessionDir)
	if filepath.Base(runsDir) != "runs" {
		return "", fmt.Errorf("session dir is not under agent-runner run storage: %s", sessionDir)
	}
	storageProjectDir := filepath.Dir(runsDir)
	projectDir := runs.ReadProjectPath(storageProjectDir)
	if strings.HasPrefix(projectDir, "? ") {
		return "", nil
	}
	return projectDir, nil
}

func debugShowWorkflow(ref string, stdout, stderr io.Writer) int {
	data, err := readDebugWorkflowRef(ref)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --show-workflow: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(data); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --show-workflow: write output: %v\n", err)
		return 1
	}
	return 0
}

func readDebugWorkflowRef(ref string) ([]byte, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("parse workflow ref: empty ref")
	}
	if strings.ContainsRune(ref, '\x00') {
		return nil, fmt.Errorf("parse workflow ref %q: contains NUL", ref)
	}
	if builtinworkflows.IsRef(ref) {
		data, err := builtinworkflows.ReadFile(ref)
		if err != nil {
			return nil, fmt.Errorf("workflow ref %q not found: %w", ref, err)
		}
		return data, nil
	}
	if strings.Contains(ref, ":") && !looksLikeWindowsDrivePath(ref) {
		resolved, err := builtinworkflows.Resolve(ref)
		if err != nil {
			return nil, err
		}
		return builtinworkflows.ReadFile(resolved)
	}
	path, err := expandDebugWorkflowPath(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- debug command intentionally reads caller-provided workflow refs.
	if err != nil {
		return nil, fmt.Errorf("workflow ref %q not found: %w", ref, err)
	}
	return data, nil
}

func expandDebugWorkflowPath(ref string) (string, error) {
	if ref == "~" {
		home, err := userHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(ref, "~/") {
		home, err := userHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		return filepath.Join(home, ref[2:]), nil
	}
	return ref, nil
}

func looksLikeWindowsDrivePath(ref string) bool {
	return len(ref) >= 2 && ref[1] == ':'
}
