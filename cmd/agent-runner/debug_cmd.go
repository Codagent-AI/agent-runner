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
	auditRunID := fs.String("audit-summary", "", "Print a redacted audit summary")
	workflowRef := fs.String("show-workflow", "", "Print workflow YAML")
	stateSet, auditSet, workflowSet := debugOpFlags(args)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug: unexpected argument: %s\n", fs.Arg(0))
		return 1
	}

	ops := 0
	for _, set := range []bool{stateSet, auditSet, workflowSet} {
		if set {
			ops++
		}
	}
	if ops != 1 {
		_, _ = fmt.Fprintln(stderr, "agent-runner debug: specify exactly one of --state, --audit-summary, or --show-workflow")
		return 1
	}

	switch {
	case stateSet:
		return debugState(*stateRunID, stdout, stderr)
	case auditSet:
		return debugAuditSummary(*auditRunID, stdout, stderr)
	default:
		return debugShowWorkflow(*workflowRef, stdout, stderr)
	}
}

func debugOpFlags(args []string) (state, auditSummary, showWorkflow bool) {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--state":
			state = true
		case "--audit-summary":
			auditSummary = true
		case "--show-workflow":
			showWorkflow = true
		}
	}
	return state, auditSummary, showWorkflow
}

func debugState(runID string, stdout, stderr io.Writer) int {
	sessionDir, _, err := resolveInspectSession(runID)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --state: %v\n", err)
		return 1
	}
	statePath := filepath.Join(sessionDir, "state.json")
	state, err := stateio.ReadState(statePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --state: %v\n", err)
		return 1
	}
	data, err := json.Marshal(state)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --state: marshal state: %v\n", err)
		return 1
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --state: marshal state: %v\n", err)
		return 1
	}
	if state.Completed {
		out["status"] = "completed"
	} else {
		out["status"] = "started"
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --state: write output: %v\n", err)
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
	auditPath, err := filepath.Abs(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --audit-summary: %v\n", err)
		return 1
	}
	file, err := os.Open(auditPath) // #nosec G304 -- audit path is resolved from a known run session dir.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			summary := audit.Summary{
				Path:         auditPath,
				Steps:        []audit.StepBoundary{},
				SubWorkflows: []audit.SubWorkflowBoundary{},
				Errors:       []audit.ErrorEvent{},
			}
			if err := json.NewEncoder(stdout).Encode(summary); err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-runner debug --audit-summary: write output: %v\n", err)
				return 1
			}
			return 0
		}
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --audit-summary: open %s: %v\n", auditPath, err)
		return 1
	}
	defer func() { _ = file.Close() }()

	summary, err := audit.BuildSummary(file, debugSummaryCapBytes)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --audit-summary: %v\n", err)
		return 1
	}
	summary.Path = auditPath
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner debug --audit-summary: write output: %v\n", err)
		return 1
	}
	return 0
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
