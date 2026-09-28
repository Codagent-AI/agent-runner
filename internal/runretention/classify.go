package runretention

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/stateio"
)

type Class string

const (
	Active      Class = "active"
	LockUnknown Class = "lock-unknown"
	Damaged     Class = "damaged"
	Finished    Class = "finished"
	Resumable   Class = "resumable"
	Stateless   Class = "stateless"
)

type Run struct {
	ID           string
	Dir          string
	Class        Class
	LastActivity time.Time
	StartTime    time.Time
	SourceRunID  string
	AuditRunIDs  []string
}

func Classify(dir string, ownLock bool) (Run, error) {
	id := filepath.Base(dir)
	r := Run{ID: id, Dir: dir, StartTime: runs.StartTimeFromID(id), Class: Stateless}
	r.LastActivity = runs.LastActivity(dir, r.StartTime)
	status, _, err := runlock.Inspect(dir)
	if err != nil {
		r.Class = LockUnknown
		return r, err
	}
	if status == runlock.LockActive && !ownLock {
		r.Class = Active
		return r, nil
	}
	stateInfo, statErr := os.Lstat(filepath.Join(dir, "state.json"))
	if errors.Is(statErr, os.ErrNotExist) {
		return r, nil
	} else if statErr != nil {
		r.Class = Damaged
		return r, statErr
	}
	if !stateInfo.Mode().IsRegular() {
		r.Class = Damaged
		return r, fmt.Errorf("state file is not a regular file in %s", dir)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		r.Class = Damaged
		return r, err
	}
	if state.WorkflowFile == "" || (state.RunKind == "audit" && (state.Audit == nil || !validRunID(state.Audit.SourceRunID))) {
		r.Class = Damaged
		return r, fmt.Errorf("inconsistent state in %s", dir)
	}
	if state.Completed {
		r.Class = Finished
	} else {
		r.Class = Resumable
	}
	if state.Audit != nil {
		if state.Audit.SourceRunID != "" && !validRunID(state.Audit.SourceRunID) {
			r.Class = Damaged
			return r, fmt.Errorf("invalid audit source in %s", dir)
		}
		r.SourceRunID = state.Audit.SourceRunID
		for i := range state.Audit.Links {
			linkID := state.Audit.Links[i].AuditRunID
			if !validRunID(linkID) {
				r.Class = Damaged
				return r, fmt.Errorf("invalid audit link in %s", dir)
			}
			r.AuditRunIDs = append(r.AuditRunIDs, linkID)
		}
	}
	return r, nil
}

func validRunID(id string) bool {
	return id != "" && !strings.HasPrefix(id, ".") && id == filepath.Base(id) && !strings.ContainsAny(id, "/\\")
}
