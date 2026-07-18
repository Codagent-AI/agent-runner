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
	// Compact UTC with fixed nanoseconds is sortable, collision-resistant, and
	// safe on Windows (unlike RFC3339, which contains colons).
	backupTimestampLayout = "20060102T150405.000000000Z"

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

type executionKey struct {
	Prefix    string
	ID        string
	Kind      string
	Iteration int
}

// Collector owns the in-memory projection and cumulative-usage baselines.
type Collector struct {
	mu             sync.Mutex
	path           string
	artifact       Artifact
	attempts       map[executionKey]int
	baselines      map[string]model.TokenCounts
	totalBaselines map[string]model.TokenTotals
	errors         []error
	writeFailures  int
	lastWriteError error
	writeRecovered bool
	now            func() time.Time
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
		attempts:       make(map[executionKey]int),
		baselines:      make(map[string]model.TokenCounts),
		totalBaselines: make(map[string]model.TokenTotals),
		now:            time.Now,
	}
	c.rehydrate(sessionStart)
	return c
}

// Process returns a normalized copy of event and updates the projection.
func (c *Collector) Process(event audit.Event) audit.Event {
	c.mu.Lock()
	defer c.mu.Unlock()

	event.Data = cloneData(event.Data)
	switch event.Type {
	case audit.EventRunStart:
		if at, ok := c.eventTimestamp(event); ok {
			c.openSession(at)
		}
	case audit.EventStepEnd, audit.EventIterationEnd:
		c.processTerminal(&event)
		if at, ok := c.eventTimestamp(event); ok {
			c.observeSession(at, false)
		}
		c.artifact.Totals = c.totalsLocked(false)
		c.persist()
	case audit.EventRunEnd:
		if at, ok := c.eventTimestamp(event); ok {
			c.observeSession(at, true)
		} else {
			// The final event is still terminal even when its timestamp is bad.
			// Close at the last trustworthy observation instead of inventing time.
			c.closeSessionAtLastObservation()
		}
		c.artifact.Totals = c.totalsLocked(false)
		c.persist()
	}
	return event
}

func (c *Collector) eventTimestamp(event audit.Event) (time.Time, bool) {
	at, err := parseTimestamp(event.Timestamp)
	if err != nil {
		c.errors = append(c.errors, fmt.Errorf("run-metrics: invalid %s timestamp %q: %w", event.Type, event.Timestamp, err))
		return time.Time{}, false
	}
	return at, true
}

// Totals returns aggregates through the latest observed terminal event.
func (c *Collector) Totals() model.RunTotals {
	c.mu.Lock()
	defer c.mu.Unlock()
	totals := c.totalsLocked(true)
	return cloneTotals(&totals)
}

// Errors returns a snapshot of all nonfatal recovery and persistence errors.
func (c *Collector) Errors() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	errors := append([]error(nil), c.errors...)
	if c.writeFailures == 0 {
		return errors
	}
	occurrences := "time"
	if c.writeFailures != 1 {
		occurrences = "times"
	}
	recovery := ""
	if c.writeRecovered {
		recovery = "; a subsequent write succeeded"
	}
	return append(errors, fmt.Errorf(
		"write run-metrics artifact failed %d %s (latest: %v)%s",
		c.writeFailures, occurrences, c.lastWriteError, recovery,
	))
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
	priorTotals, totalsFound := c.totalBaselines[key]
	if usage.RawCumulativeTokenTotals != nil {
		c.totalBaselines[key] = *usage.RawCumulativeTokenTotals
	} else {
		// A cumulative report without canonical totals breaks the attribution
		// chain. Keeping the older total baseline would charge the missing
		// invocation's tokens to the next invocation that reports totals.
		delete(c.totalBaselines, key)
	}

	if !sessionWasResumed(identity) {
		usage.Status = model.UsageCollected
		usage.Reason = ""
		usage.Tokens = cloneCounts(raw)
		usage.TokenTotals = cloneTokenTotals(usage.RawCumulativeTokenTotals)
		return usage
	}
	if !found {
		usage.Status = model.UsageUnavailable
		usage.Reason = model.UnavailableNoBaseline
		usage.Tokens = nil
		usage.TokenTotals = nil
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
		usage.TokenTotals = nil
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
	if usage.RawCumulativeTokenTotals != nil && totalsFound {
		current := usage.RawCumulativeTokenTotals
		if current.Input < priorTotals.Input || current.Output < priorTotals.Output || current.Total < priorTotals.Total {
			usage.Status = model.UsageUnavailable
			usage.Reason = model.UnavailableCounterReset
			usage.Tokens = nil
			usage.TokenTotals = nil
			usage.Completeness = ""
			return usage
		}
		usage.TokenTotals = &model.TokenTotals{
			Input: current.Input - priorTotals.Input, Output: current.Output - priorTotals.Output, Total: current.Total - priorTotals.Total,
		}
	}
	return usage
}

func sessionWasResumed(identity *model.ExecutionIdentity) bool {
	return identity.SessionResumed ||
		identity.SessionStrategy == string(model.SessionResume) ||
		identity.SessionStrategy == string(model.SessionInherit)
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
	started, startedErr := parseTimestamp(session.StartedAt)
	lastObserved, observedErr := parseTimestamp(session.LastObservedAt)
	if startedErr != nil || observedErr != nil {
		// Rehydration validates persisted timestamps, and new records are created
		// from parsed event times, so reaching this guard indicates an internal
		// invariant violation. Preserve the last stored values and surface it.
		c.errors = append(c.errors, fmt.Errorf(
			"run-metrics: invalid active session timestamp: started_at=%q (%v), last_observed_at=%q (%v)",
			session.StartedAt, startedErr, session.LastObservedAt, observedErr,
		))
		return
	}
	observed := at
	if lastObserved.After(observed) {
		observed = lastObserved
	}
	if started.After(observed) {
		observed = started
	}
	session.LastObservedAt = observed.UTC().Format(time.RFC3339Nano)
	session.DurationMS = observed.Sub(started).Milliseconds()
	if closeSession {
		session.Status = SessionClosed
		session.EndedAt = session.LastObservedAt
	}
}

func (c *Collector) closeSessionAtLastObservation() {
	if len(c.artifact.Sessions) == 0 {
		return
	}
	session := &c.artifact.Sessions[len(c.artifact.Sessions)-1]
	session.Status = SessionClosed
	session.EndedAt = session.LastObservedAt
}

func (c *Collector) totalsLocked(includeLiveSession bool) model.RunTotals {
	totals := emptyTotals()
	for i, session := range c.artifact.Sessions {
		duration := session.DurationMS
		if includeLiveSession && i == len(c.artifact.Sessions)-1 && session.Status == SessionOpen {
			if started, err := parseTimestamp(session.StartedAt); err == nil {
				liveDuration := c.now().Sub(started).Milliseconds()
				if liveDuration > duration {
					duration = liveDuration
				}
			}
		}
		totals.ActiveDurationMS += duration
	}
	agents := 0
	usageReported := 0
	tokenTotalsReported := 0
	costReported := 0
	var cost float64
	canonicalTotals := model.TokenTotals{}
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
			if step.Usage.TokenTotals != nil {
				tokenTotalsReported++
				canonicalTotals.Input += step.Usage.TokenTotals.Input
				canonicalTotals.Output += step.Usage.TokenTotals.Output
				canonicalTotals.Total += step.Usage.TokenTotals.Total
			}
		}
		if step.EstimatedAPICostUSD != nil {
			costReported++
			cost += *step.EstimatedAPICostUSD
		}
	}
	totals.UsageCoverage = coverage(agents, usageReported)
	totals.TokenTotalCoverage = coverage(agents, tokenTotalsReported)
	if tokenTotalsReported > 0 {
		totals.TokenTotals = &canonicalTotals
	}
	totals.CostCoverage = coverage(agents, costReported)
	if costReported > 0 {
		totals.EstimatedAPICostUSD = &cost
	}
	return totals
}

func (c *Collector) persist() {
	if err := stateio.WriteJSONAtomic(c.path, &c.artifact); err != nil {
		c.writeFailures++
		c.lastWriteError = err
		c.writeRecovered = false
		return
	}
	if c.writeFailures > 0 {
		c.writeRecovered = true
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
	if artifact.RunID != c.artifact.RunID || artifact.Workflow != c.artifact.Workflow {
		c.recoverArtifact(sessionStart, fmt.Errorf(
			"artifact identity %q/%q does not match run %q/%q",
			artifact.RunID, artifact.Workflow, c.artifact.RunID, c.artifact.Workflow,
		))
		return
	}
	if err := validateSessionTimestamps(artifact.Sessions); err != nil {
		c.recoverArtifact(sessionStart, fmt.Errorf("invalid session timestamp: %w", err))
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
			key := baselineKey(record.Usage.CLI, record.SessionID)
			c.baselines[key] = cloneCounts(record.Usage.RawCumulative)
			if record.Usage.RawCumulativeTokenTotals != nil {
				c.totalBaselines[key] = *record.Usage.RawCumulativeTokenTotals
			}
		}
	}
}

func (c *Collector) recoverArtifact(sessionStart time.Time, cause error) {
	stamp := sessionStart.UTC().Format(backupTimestampLayout)
	backup := c.path + ".bak-" + stamp
	for suffix := 2; ; suffix++ {
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			break
		}
		backup = fmt.Sprintf("%s.bak-%s-%d", c.path, stamp, suffix)
	}
	if err := os.Rename(c.path, backup); err != nil {
		c.errors = append(c.errors, fmt.Errorf("run-metrics recovery (%v), preserve artifact: %w", cause, err))
	} else {
		c.errors = append(c.errors, fmt.Errorf("run-metrics recovery: %v; preserved as %s", cause, filepath.Base(backup)))
	}
	c.artifact.HistoryComplete = false
}

func emptyTotals() model.RunTotals {
	return model.RunTotals{Tokens: make(model.TokenCounts), UsageCoverage: model.CoverageNone, TokenTotalCoverage: model.CoverageNone, CostCoverage: model.CoverageNone}
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

func attemptKey(prefix, id, kind string, iteration int) executionKey {
	return executionKey{Prefix: prefix, ID: id, Kind: kind, Iteration: iteration}
}

func recordID(prefix, id, kind string, iteration, attempt int) string {
	escapedPrefix := escapeRecordComponent(prefix)
	escapedID := escapeRecordComponent(id)
	var name string
	if kind == "iteration" {
		parts := []string{"@iteration"}
		if escapedPrefix != "" {
			parts = append(parts, escapedPrefix)
		}
		parts = append(parts, escapedID, fmt.Sprintf("%d", iteration))
		name = strings.Join(parts, "/")
	} else {
		name = escapedID
		if escapedPrefix != "" {
			name = escapedPrefix + "/" + escapedID
		}
	}
	return fmt.Sprintf("%s#%d", name, attempt)
}

func escapeRecordComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var escaped strings.Builder
	for i := 0; i < len(value); i++ {
		char := value[i]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			escaped.WriteByte(char)
			continue
		}
		escaped.WriteByte('%')
		escaped.WriteByte(hex[char>>4])
		escaped.WriteByte(hex[char&0x0f])
	}
	return escaped.String()
}

func baselineKey(cliName, sessionID string) string { return cliName + "\x00" + sessionID }

func parseTimestamp(value string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return at, nil
}

func validateSessionTimestamps(sessions []SessionRecord) error {
	for i := range sessions {
		session := &sessions[i]
		for _, field := range []struct{ name, value string }{
			{name: "started_at", value: session.StartedAt},
			{name: "last_observed_at", value: session.LastObservedAt},
		} {
			if _, err := parseTimestamp(field.value); err != nil {
				return fmt.Errorf("session %d %s %q: %w", i, field.name, field.value, err)
			}
		}
		if session.EndedAt != "" {
			if _, err := parseTimestamp(session.EndedAt); err != nil {
				return fmt.Errorf("session %d ended_at %q: %w", i, session.EndedAt, err)
			}
		}
	}
	return nil
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
	cloned.TokenTotals = cloneTokenTotals(usage.TokenTotals)
	cloned.RawCumulativeTokenTotals = cloneTokenTotals(usage.RawCumulativeTokenTotals)
	return cloned
}

func cloneTokenTotals(totals *model.TokenTotals) *model.TokenTotals {
	if totals == nil {
		return nil
	}
	cloned := *totals
	return &cloned
}

func cloneTotals(input *model.RunTotals) model.RunTotals {
	totals := *input
	totals.Tokens = cloneCounts(input.Tokens)
	totals.TokenTotals = cloneTokenTotals(input.TokenTotals)
	if input.EstimatedAPICostUSD != nil {
		value := *input.EstimatedAPICostUSD
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
