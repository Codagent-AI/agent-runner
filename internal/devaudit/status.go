//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/stateio"
)

// Audit outcomes reported by "audit status". A lifecycle link can be
// "completed" even when a stage failed and no rows were delivered, so callers
// that must know whether metrics reached the Sheet use the outcome instead.
const (
	OutcomeActive          = "active"
	OutcomeDelivered       = "delivered"
	OutcomePendingDelivery = "pending-delivery"
	OutcomeFailed          = "failed"
)

// LinkStatus is a lifecycle link with its derived delivery outcome.
type LinkStatus struct {
	Link
	AuditSessionDir string `json:"audit_session_dir"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason,omitempty"`
}

// Status is the "audit status" document: the lifecycle with derived outcomes.
type Status struct {
	Lifecycle
	Links []LinkStatus `json:"links"`
}

// ReadStatus derives each linked audit's outcome from its lifecycle state, the
// audit run's local report, and the audit run's recorded failure.
func ReadStatus(sourceSessionDir string) (Status, error) {
	lifecycle, err := ReadLifecycle(filepath.Join(sourceSessionDir, lifecycleFileName))
	if err != nil {
		return Status{}, err
	}
	status := Status{Lifecycle: lifecycle, Links: make([]LinkStatus, 0, len(lifecycle.Links))}
	for index := range lifecycle.Links {
		link := &lifecycle.Links[index]
		dir := auditSessionDir(sourceSessionDir, link.AuditRunID)
		outcome, reason := linkOutcome(link, dir)
		status.Links = append(status.Links, LinkStatus{Link: *link, AuditSessionDir: dir, Outcome: outcome, Reason: reason})
	}
	return status, nil
}

func linkOutcome(link *Link, dir string) (outcome, reason string) {
	switch link.State {
	case LaunchReserved, LaunchLaunching, LaunchStarted:
		return OutcomeActive, ""
	case LaunchFailed:
		return OutcomeFailed, firstNonEmpty(link.Warning, auditFailureReason(dir), "audit launch failed")
	}
	data, err := os.ReadFile(filepath.Join(dir, "local-report.json")) // #nosec G304 -- fixed artifact under the linked audit directory.
	if err == nil {
		var report LocalReport
		if json.Unmarshal(data, &report) == nil {
			switch report.DeliveryState {
			case "delivered":
				return OutcomeDelivered, ""
			case "pending":
				return OutcomePendingDelivery, firstNonEmpty(link.ReportingWarning, report.DeliveryError, "Sheets reporting pending")
			}
		}
	}
	return OutcomeFailed, firstNonEmpty(link.Warning, auditFailureReason(dir), "audit finished without a local report")
}

func auditFailureReason(dir string) string {
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		return ""
	}
	return state.FailureReason
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
