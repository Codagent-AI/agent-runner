// Package runs discovers and describes workflow runs from session directories.
package runs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
)

// Status represents the current state of a workflow run.
type Status int

const (
	StatusActive    Status = iota // lock file present, PID is alive
	StatusInactive                // stale lock or state file present without lock
	StatusCompleted               // no lock and no state file
)

// RunInfo holds the displayable information for a single workflow run.
type RunInfo struct {
	SessionID    string
	SessionDir   string
	WorkflowName string
	CurrentStep  string // empty when status is Completed
	Status       Status
	StartTime    time.Time
}

// projectMeta is the JSON structure of meta.json.
type projectMeta struct {
	Path string `json:"path"`
}

// timestampRe matches the start of an RFC3339 timestamp in a session ID
// where colons and dots have been replaced with hyphens.
var timestampRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}`)

// ListForDir reads all session directories under projectDir/runs/ and returns
// RunInfo for each, sorted most recent first.
func ListForDir(projectDir string) ([]RunInfo, error) {
	runsDir := filepath.Join(projectDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []RunInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		sessionDir := filepath.Join(runsDir, sessionID)

		info := RunInfo{
			SessionID:  sessionID,
			SessionDir: sessionDir,
		}

		// Determine status from lock and state file.
		lockStatus := runlock.Check(sessionDir)
		stateExists := fileExists(filepath.Join(sessionDir, "state.json"))

		switch {
		case lockStatus == runlock.LockActive:
			info.Status = StatusActive
		case lockStatus == runlock.LockStale:
			info.Status = StatusInactive
		case stateExists:
			info.Status = StatusInactive
		default:
			info.Status = StatusCompleted
		}

		// Read workflow name and current step from state.json if available.
		if stateExists {
			state, readErr := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
			if readErr == nil {
				info.WorkflowName = state.WorkflowName
				info.CurrentStep = currentStepID(&state)
			}
		}

		// Fallback: parse workflow name from session ID.
		if info.WorkflowName == "" {
			info.WorkflowName = parseWorkflowName(sessionID)
		}

		// Parse start time from session ID.
		info.StartTime = parseStartTime(sessionID)

		results = append(results, info)
	}

	// Sort most recent first.
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartTime.After(results[j].StartTime)
	})

	return results, nil
}

// ReadProjectPath returns the stored path from meta.json, or the encoded
// directory name if meta.json does not exist.
func ReadProjectPath(projectDir string) string {
	data, err := os.ReadFile(filepath.Join(projectDir, "meta.json")) // #nosec G304 -- project dir is from internal state tracking
	if err != nil {
		return filepath.Base(projectDir)
	}
	var meta projectMeta
	if err := json.Unmarshal(data, &meta); err != nil || meta.Path == "" {
		return filepath.Base(projectDir)
	}
	return meta.Path
}

// parseWorkflowName extracts the workflow name from a session ID by removing
// the timestamp suffix.
func parseWorkflowName(sessionID string) string {
	loc := timestampRe.FindStringIndex(sessionID)
	if loc == nil {
		return sessionID
	}
	name := sessionID[:loc[0]]
	return strings.TrimRight(name, "-")
}

// parseStartTime extracts the timestamp from a session ID and parses it.
func parseStartTime(sessionID string) time.Time {
	loc := timestampRe.FindStringIndex(sessionID)
	if loc == nil {
		return time.Time{}
	}

	// Extract the full timestamp portion (everything from the match to end).
	tsPart := sessionID[loc[0]:]

	// The timestamp has colons and dots replaced with hyphens. We need to
	// restore them for parsing. The format is like:
	// 2026-04-11T09-14-00-000000000Z  (from RFC3339Nano with replacements)
	// We need to restore: T09-14-00 → T09:14:00
	// The date part (2026-04-11) uses real hyphens, so we only fix the time part.
	if len(tsPart) >= 19 {
		ts := []byte(tsPart)
		if ts[13] == '-' && ts[16] == '-' {
			ts[13] = ':'
			ts[16] = ':'
		}
		tsPart = string(ts)
	}

	// Try parsing with nanoseconds first (the nano portion has hyphens
	// replacing the dot, e.g., -000000000Z). Restore the dot.
	if len(tsPart) > 19 && tsPart[19] == '-' {
		withDot := tsPart[:19] + "." + tsPart[20:]
		if t, err := time.Parse(time.RFC3339Nano, withDot); err == nil {
			return t
		}
	}

	// Try plain RFC3339.
	if t, err := time.Parse(time.RFC3339, tsPart); err == nil {
		return t
	}

	return time.Time{}
}

// currentStepID extracts the leaf step ID from a RunState.
func currentStepID(state *model.RunState) string {
	if state.CurrentStep.Nested != nil {
		return leafStepID(state.CurrentStep.Nested)
	}
	return state.CurrentStep.StepID
}

// leafStepID walks nested state to find the innermost step ID.
func leafStepID(n *model.NestedStepState) string {
	if n.Child != nil {
		return leafStepID(n.Child)
	}
	return n.StepID
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
