package model

import "sync"

type FailureKind string

const (
	FailureInfrastructure FailureKind = "infrastructure"
	FailureStep           FailureKind = "step"
)

type StepFailure struct {
	Kind   FailureKind
	Origin *CrashRecord
}

type PreviousStepRecord struct {
	Outcome       string      `json:"outcome"`
	FailureKind   FailureKind `json:"failureKind,omitempty"`
	CrashObserved bool        `json:"crashObserved,omitempty"`
}

type CrashRecord struct {
	StepID   string           `json:"stepId"`
	Prefix   string           `json:"prefix"`
	Path     []NestingSegment `json:"path"`
	Attempt  int              `json:"attempt"`
	ExitCode *int             `json:"exitCode,omitempty"`
	Error    string           `json:"error,omitempty"`
	Stderr   string           `json:"stderr,omitempty"`
	// ExecutionSessionID is the Agent Runner execution (one per start or resume),
	// not the agent CLI session; AgentSessionID carries that when known.
	ExecutionSessionID string `json:"executionSessionId"`
	AgentSessionID     string `json:"agentSessionId,omitempty"`
}

type CrashLedger struct {
	mu      sync.Mutex
	records []CrashRecord
}

func NewCrashLedger() *CrashLedger { return &CrashLedger{} }

func (l *CrashLedger) Add(record *CrashRecord) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, *record)
}

func (l *CrashLedger) Restore(records []CrashRecord) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append([]CrashRecord(nil), records...)
}

func (l *CrashLedger) Records() []CrashRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CrashRecord(nil), l.records...)
}

func (l *CrashLedger) ObservedUnder(path []NestingSegment) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.records {
		if crashPathMatches(l.records[i].Path, path) {
			return true
		}
	}
	return false
}

func (l *CrashLedger) PruneReexecuted(path []NestingSegment, sessionID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.records[:0]
	for i := range l.records {
		if l.records[i].ExecutionSessionID == sessionID || !crashPathMatches(l.records[i].Path, path) {
			kept = append(kept, l.records[i])
		}
	}
	l.records = kept
}

func (l *CrashLedger) FindPrefix(prefix string) *CrashRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.records) - 1; i >= 0; i-- {
		if l.records[i].Prefix == prefix {
			record := l.records[i]
			return &record
		}
	}
	return nil
}

func crashPathMatches(recordPath, query []NestingSegment) bool {
	if len(recordPath) < len(query) {
		return false
	}
	for i, want := range query {
		got := recordPath[i]
		if got.StepID != want.StepID {
			return false
		}
		if want.Iteration != nil && (got.Iteration == nil || *got.Iteration != *want.Iteration) {
			return false
		}
		if want.RepairAttempt != nil && (got.RepairAttempt == nil || *got.RepairAttempt != *want.RepairAttempt) {
			return false
		}
		if want.SubWorkflowName != "" && got.SubWorkflowName != want.SubWorkflowName {
			return false
		}
	}
	return true
}
