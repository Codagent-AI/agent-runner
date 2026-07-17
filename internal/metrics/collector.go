// Package metrics normalizes terminal audit events into durable run metrics.
package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	SchemaVersion = 1
	FileName      = "run-metrics.json"

	DataIdentity            = "identity"
	DataUsage               = "usage"
	DataEstimatedAPICostUSD = "estimated_api_cost_usd"
	DataTotals              = "totals"

	SessionOpen   = "open"
	SessionClosed = "closed"
)

// Artifact is the stable run-metrics.json schema.
type Artifact struct {
	SchemaVersion   int             `json:"schema_version"`
	RunID           string          `json:"run_id"`
	Workflow        string          `json:"workflow"`
	HistoryComplete bool            `json:"history_complete"`
	Sessions        []SessionRecord `json:"sessions"`
	Steps           []StepRecord    `json:"steps"`
	Totals          model.RunTotals `json:"totals"`
}

type SessionRecord struct {
	StartedAt      string `json:"started_at"`
	LastObservedAt string `json:"last_observed_at"`
	EndedAt        string `json:"ended_at,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	Status         string `json:"status"`
}

type StepRecord struct {
	RecordID            string             `json:"record_id"`
	Prefix              string             `json:"prefix"`
	ID                  string             `json:"id"`
	Kind                string             `json:"kind"`
	Type                string             `json:"type"`
	Attempt             int                `json:"attempt"`
	Iteration           *int               `json:"iteration"`
	Outcome             string             `json:"outcome"`
	DurationMS          int64              `json:"duration_ms"`
	SessionID           string             `json:"session_id,omitempty"`
	AgentInvoked        bool               `json:"agent_invoked"`
	Usage               *model.UsageRecord `json:"usage"`
	EstimatedAPICostUSD *float64           `json:"estimated_api_cost_usd"`
}

// Collector owns the in-memory projection and cumulative-usage baselines.
type Collector struct {
	mu        sync.Mutex
	path      string
	artifact  Artifact
	attempts  map[string]int
	baselines map[string]model.TokenCounts
	errors    []error
	now       func() time.Time
}

// NewCollector creates a collector and rehydrates an existing artifact when
// present. Recovery warnings are retained by Errors rather than returned.
func NewCollector(sessionDir, runID, workflow string, sessionStart time.Time) *Collector {
	c := &Collector{
		path: filepath.Join(sessionDir, FileName),
		artifact: Artifact{
			SchemaVersion: SchemaVersion, RunID: runID, Workflow: workflow, HistoryComplete: true,
			Sessions: []SessionRecord{}, Steps: []StepRecord{}, Totals: emptyTotals(),
		},
		attempts:  make(map[string]int),
		baselines: make(map[string]model.TokenCounts),
		now:       time.Now,
	}
	c.rehydrate(sessionStart)
	return c
}

// Process returns a normalized copy of event and updates the projection.
func (c *Collector) Process(event audit.Event) audit.Event {
	c.mu.Lock()
	defer c.mu.Unlock()

	event.Data = cloneData(event.Data)
	at := parseTimestamp(event.Timestamp)
	switch event.Type {
	case audit.EventRunStart:
		c.openSession(at)
	case audit.EventStepEnd, audit.EventIterationEnd:
		c.processTerminal(&event)
		c.observeSession(at, false)
		c.artifact.Totals = c.totalsLocked(false)
		c.persist()
	case audit.EventRunEnd:
		c.observeSession(at, true)
		c.artifact.Totals = c.totalsLocked(false)
		c.persist()
	}
	return event
}

// Totals returns aggregates through the latest observed terminal event.
func (c *Collector) Totals() model.RunTotals {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneTotals(c.totalsLocked(true))
}

// Errors returns a snapshot of all nonfatal recovery and persistence errors.
func (c *Collector) Errors() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]error(nil), c.errors...)
}

func (c *Collector) processTerminal(event *audit.Event) {
	identity, ok := event.Data[DataIdentity].(model.ExecutionIdentity)
	if !ok {
		c.errors = append(c.errors, fmt.Errorf("run-metrics: %s missing typed execution identity", event.Type))
		return
	}
	key := attemptKey(identity.Prefix, identity.StepID, identity.Kind, identity.Iteration)
	c.attempts[key]++
	identity.Attempt = c.attempts[key]
	event.Data[DataIdentity] = identity

	record := StepRecord{
		RecordID: recordID(identity.Prefix, identity.StepID, identity.Kind, identity.Iteration, identity.Attempt), Prefix: identity.Prefix,
		ID: identity.StepID, Kind: identity.Kind, Type: identity.StepType, Attempt: identity.Attempt,
		Outcome: stringValue(event.Data["outcome"]), DurationMS: int64Value(event.Data["duration_ms"]),
		SessionID: identity.SessionID, AgentInvoked: identity.AgentInvoked,
	}
	if identity.Kind == "iteration" {
		iteration := identity.Iteration
		record.Iteration = &iteration
	} else if usage, ok := event.Data[DataUsage].(model.UsageRecord); ok {
		usage = c.attribute(&identity, &usage)
		event.Data[DataUsage] = usage
		record.Usage = &usage
		if cost, ok := event.Data[DataEstimatedAPICostUSD].(*float64); ok && cost != nil {
			value := *cost
			record.EstimatedAPICostUSD = &value
		}
	}
	c.artifact.Steps = append(c.artifact.Steps, record)
}

func (c *Collector) attribute(identity *model.ExecutionIdentity, input *model.UsageRecord) model.UsageRecord {
	usage := cloneUsage(input)
	if len(usage.RawCumulative) == 0 {
		return usage
	}
	raw := cloneCounts(usage.RawCumulative)
	usage.RawCumulative = raw
	key := baselineKey(identity.CLI, identity.SessionID)
	prior, found := c.baselines[key]
	c.baselines[key] = cloneCounts(raw)

	if identity.SessionStrategy == string(model.SessionNew) {
		usage.Status = model.UsageCollected
		usage.Reason = ""
		usage.Tokens = cloneCounts(raw)
		return usage
	}
	if !found {
		usage.Status = model.UsageUnavailable
		usage.Reason = model.UnavailableNoBaseline
		usage.Tokens = nil
		usage.Completeness = ""
		return usage
	}
	for category, current := range raw {
		baseline, ok := prior[category]
		if !ok || current >= baseline {
			continue
		}
		usage.Status = model.UsageUnavailable
		usage.Reason = model.UnavailableCounterReset
		usage.Tokens = nil
		usage.Completeness = ""
		return usage
	}
	delta := make(model.TokenCounts)
	for category, current := range raw {
		delta[category] = current - prior[category]
	}
	usage.Status = model.UsageCollected
	usage.Reason = ""
	usage.Tokens = delta
	return usage
}

func (c *Collector) openSession(at time.Time) {
	for i := range c.artifact.Sessions {
		if c.artifact.Sessions[i].Status == SessionOpen {
			c.artifact.Sessions[i].Status = SessionClosed
			c.artifact.Sessions[i].EndedAt = c.artifact.Sessions[i].LastObservedAt
		}
	}
	stamp := at.UTC().Format(time.RFC3339Nano)
	c.artifact.Sessions = append(c.artifact.Sessions, SessionRecord{
		StartedAt: stamp, LastObservedAt: stamp, Status: SessionOpen,
	})
}

func (c *Collector) observeSession(at time.Time, closeSession bool) {
	if len(c.artifact.Sessions) == 0 {
		return
	}
	session := &c.artifact.Sessions[len(c.artifact.Sessions)-1]
	started := parseTimestamp(session.StartedAt)
	if at.Before(started) {
		at = started
	}
	session.LastObservedAt = at.UTC().Format(time.RFC3339Nano)
	session.DurationMS = at.Sub(started).Milliseconds()
	if closeSession {
		session.Status = SessionClosed
		session.EndedAt = session.LastObservedAt
	}
}

func (c *Collector) totalsLocked(includeLiveSession bool) model.RunTotals {
	totals := emptyTotals()
	for i, session := range c.artifact.Sessions {
		duration := session.DurationMS
		if includeLiveSession && i == len(c.artifact.Sessions)-1 && session.Status == SessionOpen {
			liveDuration := c.now().Sub(parseTimestamp(session.StartedAt)).Milliseconds()
			if liveDuration > duration {
				duration = liveDuration
			}
		}
		totals.ActiveDurationMS += duration
	}
	agents := 0
	usageReported := 0
	costReported := 0
	var cost float64
	for i := range c.artifact.Steps {
		step := &c.artifact.Steps[i]
		if step.Usage != nil && step.Usage.Status == model.UsageCollected {
			for category, count := range step.Usage.Tokens {
				totals.Tokens[category] += count
			}
		}
		if step.Type != "agent" || !step.AgentInvoked {
			continue
		}
		agents++
		if step.Usage != nil && step.Usage.Status == model.UsageCollected {
			usageReported++
		}
		if step.EstimatedAPICostUSD != nil {
			costReported++
			cost += *step.EstimatedAPICostUSD
		}
	}
	totals.UsageCoverage = coverage(agents, usageReported)
	totals.CostCoverage = coverage(agents, costReported)
	if costReported > 0 {
		totals.EstimatedAPICostUSD = &cost
	}
	return totals
}

func (c *Collector) persist() {
	if err := stateio.WriteJSONAtomic(c.path, &c.artifact); err != nil {
		c.errors = append(c.errors, fmt.Errorf("write run-metrics artifact: %w", err))
	}
}

func (c *Collector) rehydrate(sessionStart time.Time) {
	data, err := os.ReadFile(c.path) // #nosec G304 -- path is the run directory's fixed metrics filename
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		c.recoverArtifact(sessionStart, fmt.Errorf("read existing artifact: %w", err))
		return
	}
	var artifact Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		c.recoverArtifact(sessionStart, fmt.Errorf("parse existing artifact: %w", err))
		return
	}
	if artifact.SchemaVersion != SchemaVersion {
		c.recoverArtifact(sessionStart, fmt.Errorf("unsupported schema version %d", artifact.SchemaVersion))
		return
	}
	if artifact.Sessions == nil {
		artifact.Sessions = []SessionRecord{}
	}
	if artifact.Steps == nil {
		artifact.Steps = []StepRecord{}
	}
	c.artifact = artifact
	for i := range artifact.Steps {
		record := &artifact.Steps[i]
		iteration := 0
		if record.Iteration != nil {
			iteration = *record.Iteration
		}
		key := attemptKey(record.Prefix, record.ID, record.Kind, iteration)
		if record.Attempt > c.attempts[key] {
			c.attempts[key] = record.Attempt
		}
		if record.SessionID != "" && record.Usage != nil && len(record.Usage.RawCumulative) > 0 {
			c.baselines[baselineKey(record.Usage.CLI, record.SessionID)] = cloneCounts(record.Usage.RawCumulative)
		}
	}
}

func (c *Collector) recoverArtifact(sessionStart time.Time, cause error) {
	backup := c.path + ".bak-" + sessionStart.UTC().Format(time.RFC3339)
	for suffix := 2; ; suffix++ {
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			break
		}
		backup = fmt.Sprintf("%s.bak-%s-%d", c.path, sessionStart.UTC().Format(time.RFC3339), suffix)
	}
	if err := os.Rename(c.path, backup); err != nil {
		c.errors = append(c.errors, fmt.Errorf("run-metrics recovery (%v), preserve artifact: %w", cause, err))
	} else {
		c.errors = append(c.errors, fmt.Errorf("run-metrics recovery: %v; preserved as %s", cause, filepath.Base(backup)))
	}
	c.artifact.HistoryComplete = false
}

func emptyTotals() model.RunTotals {
	return model.RunTotals{Tokens: make(model.TokenCounts), UsageCoverage: model.CoverageNone, CostCoverage: model.CoverageNone}
}

func coverage(eligible, reported int) model.Coverage {
	if eligible == 0 || reported == 0 {
		return model.CoverageNone
	}
	if eligible == reported {
		return model.CoverageComplete
	}
	return model.CoveragePartial
}

func logicalKey(prefix, id string) string { return prefix + "\x00" + id }

func attemptKey(prefix, id, kind string, iteration int) string {
	if kind == "iteration" {
		id = fmt.Sprintf("%s:%d", id, iteration)
	}
	return logicalKey(prefix, id)
}

func recordID(prefix, id, kind string, iteration, attempt int) string {
	if kind == "iteration" {
		id = fmt.Sprintf("%s:%d", id, iteration)
	}
	name := id
	if prefix != "" {
		name = strings.TrimSuffix(prefix, "/") + "/" + id
	}
	return fmt.Sprintf("%s#%d", name, attempt)
}

func baselineKey(cliName, sessionID string) string { return cliName + "\x00" + sessionID }

func parseTimestamp(value string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().UTC()
	}
	return at
}

func cloneData(data map[string]any) map[string]any {
	cloned := make(map[string]any, len(data))
	for key, value := range data {
		cloned[key] = value
	}
	return cloned
}

func cloneCounts(counts model.TokenCounts) model.TokenCounts {
	if counts == nil {
		return nil
	}
	cloned := make(model.TokenCounts, len(counts))
	for category, count := range counts {
		cloned[category] = count
	}
	return cloned
}

func cloneUsage(usage *model.UsageRecord) model.UsageRecord {
	cloned := *usage
	cloned.Tokens = cloneCounts(usage.Tokens)
	cloned.RawCumulative = cloneCounts(usage.RawCumulative)
	return cloned
}

func cloneTotals(totals model.RunTotals) model.RunTotals {
	totals.Tokens = cloneCounts(totals.Tokens)
	if totals.EstimatedAPICostUSD != nil {
		value := *totals.EstimatedAPICostUSD
		totals.EstimatedAPICostUSD = &value
	}
	return totals
}

func stringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func int64Value(value any) int64 {
	switch n := value.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
