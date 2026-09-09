package metrics

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

// Attribution is Runner-owned metadata, separate from immutable producer bytes.
// Prefix and StepID identify the structural owning execution, not an opaque FK.
type Attribution struct {
	RunID              string `json:"run_id"`
	ExecutionSessionID string `json:"execution_session_id"`
	ParentAttemptID    string `json:"parent_attempt_id"`
	StepID             string `json:"step_id"`
	Prefix             string `json:"prefix"`
	ContextID          string `json:"context_id,omitempty"`
}
type MeasurementHead struct {
	Key         string          `json:"key"`
	StoreID     string          `json:"store_id"`
	Attribution Attribution     `json:"attribution"`
	Record      json.RawMessage `json:"record"`
	Status      string          `json:"status"`
}
type DeliveryContext struct {
	Attribution   Attribution `json:"attribution"`
	StoreID       string      `json:"store_id,omitempty"`
	EvidenceState string      `json:"evidence_state"`
	Delivery      string      `json:"delivery"`
	Collection    string      `json:"collection"`
	History       string      `json:"history"`
	Gaps          []string    `json:"gaps"`
	Generation    int64       `json:"generation"`
	ScopeComplete bool        `json:"scope_complete"`
}
type FieldAggregate struct {
	KnownSubtotal float64 `json:"known_subtotal"`
	Availability  string  `json:"availability"`
	Precision     string  `json:"precision"`
	Contributing  int     `json:"contributing_attempts"`
	Partial       int     `json:"partial_attempts"`
	Missing       int     `json:"missing_attempts"`
}

// IncorporateValidator is the acknowledgment durability boundary. The void
// audit stream cannot authorize acknowledgment. This operation can.
//
//nolint:gocritic // Attribution and delivery are immutable transaction snapshots.
func (c *Collector) IncorporateValidator(attr Attribution, store string, raws []json.RawMessage, delivery DeliveryContext) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var records []measurements.Record
	for _, raw := range raws {
		record, err := measurements.ValidateRecord(raw, "agent-runner", attr.ContextID)
		if err != nil {
			return err
		}
		records = append(records, record)
	}
	var conflict bool
	for n := range records {
		record := &records[n]
		key := store + "/" + record.Type + "/" + record.ID
		found := -1
		for i := range c.artifact.MeasurementHeads {
			h := &c.artifact.MeasurementHeads[i]
			if h.Key == key {
				found = i
				break
			}
		}
		head := MeasurementHead{Key: key, StoreID: store, Attribution: attr, Record: append(json.RawMessage(nil), raws[n]...), Status: "supported"}
		if found < 0 {
			c.artifact.MeasurementHeads = append(c.artifact.MeasurementHeads, head)
			continue
		}
		previous := &c.artifact.MeasurementHeads[found]
		var old measurements.Record
		if json.Unmarshal(previous.Record, &old) != nil {
			return fmt.Errorf("invalid_stored_head")
		}
		if record.Revision < old.Revision {
			continue
		}
		if record.Revision == old.Revision {
			if record.Digest.Value != old.Digest.Value {
				previous.Status = "conflicting"
				conflict = true
			}
			continue
		}
		// Producer identity remains attached to its original execution on recovery.
		head.Attribution = previous.Attribution
		c.artifact.MeasurementHeads[found] = head
	}
	delivery.Attribution = attr
	delivery.StoreID = store
	if conflict {
		delivery.Delivery = "blocked"
		delivery.Gaps = append(delivery.Gaps, "conflicting_revision")
	}
	c.upsertDeliveryLocked(delivery)
	c.refreshAggregatesLocked()
	if err := stateio.WriteJSONDurable(c.path, c.artifact); err != nil {
		return fmt.Errorf("persist_measurements: %w", err)
	}
	if conflict {
		return fmt.Errorf("conflicting_revision")
	}
	return nil
}

//nolint:gocritic // Preserve the collector transaction value boundary.
func (p *Pipeline) IncorporateValidator(attr Attribution, store string, records []json.RawMessage, delivery DeliveryContext) error {
	return p.collector.IncorporateValidator(attr, store, records, delivery)
}

//nolint:gocritic // The collector owns a copied delivery snapshot.
func (c *Collector) upsertDeliveryLocked(value DeliveryContext) {
	for i := range c.artifact.ValidatorContexts {
		old := &c.artifact.ValidatorContexts[i]
		if old.Attribution.ContextID == value.Attribution.ContextID {
			c.artifact.ValidatorContexts[i] = value
			return
		}
	}
	c.artifact.ValidatorContexts = append(c.artifact.ValidatorContexts, value)
}
func (c *Collector) refreshMeasurementsLocked() {
	// Replace compatibility views from authoritative current heads, never append
	// a revision as another dispatch. Preserve unrelated native/legacy records.
	kept := c.artifact.Steps[:0]
	for i := range c.artifact.Steps {
		step := &c.artifact.Steps[i]
		if step.MeasurementKey == "" {
			kept = append(kept, *step)
		}
	}
	c.artifact.Steps = kept
	fields := map[string]FieldAggregate{}
	for i := range c.artifact.MeasurementHeads {
		head := &c.artifact.MeasurementHeads[i]
		var record measurements.Record
		if json.Unmarshal(head.Record, &record) != nil || record.Type != "model_attempt" {
			continue
		}
		p := record.Payload
		if p.Lifecycle.State == "prepared" && p.Lifecycle.StartedAt == nil {
			continue
		}
		usable := head.Status == "supported"
		for i := range c.artifact.ValidatorContexts {
			ctx := &c.artifact.ValidatorContexts[i]
			if ctx.Attribution.ContextID == head.Attribution.ContextID && slices.Contains(ctx.Gaps, "unsupported_measurement_version") {
				head.Status = "unsupported_current_scope"
			}
		}
		if head.Status == "unsupported_current_scope" {
			continue
		}
		step := validatorStepProjection(head, &record, usable)
		c.artifact.Steps = append(c.artifact.Steps, step)
		accumulateFields(fields, attemptAggregateTokens(&p), usable)
	}
	c.refreshNativeMeasurementsLocked()
	for i := range c.artifact.NativeMeasurements {
		n := &c.artifact.NativeMeasurements[i]
		accumulateFields(fields, n.Tokens, true)
	}
	for name, a := range fields {
		a.Availability = "available"
		if a.Contributing == 0 {
			a.Availability = "unavailable"
		} else if a.Partial > 0 || a.Missing > 0 {
			a.Availability = "partial"
		}
		if a.Precision == "" {
			a.Precision = "exact"
		}
		fields[name] = a
	}
	c.artifact.MeasurementTotals = fields
	c.reconcileContextsLocked()
}

var CanonicalFields = []string{"input_total", "input_uncached", "cache_read", "cache_write", "output", "reasoning", "provider_total", "normalized_total"}

func CompleteUSD(costs []measurements.Cost) *float64 {
	if len(costs) != 1 {
		return nil
	}
	cost := costs[0]
	if cost.Scope != "attempt" || cost.Coverage != "full" || cost.Overlap != "established" || cost.Currency.Value == nil || *cost.Currency.Value != "USD" || cost.Amount.Availability != "available" || cost.Amount.Value == nil {
		return nil
	}
	value := *cost.Amount.Value
	return &value
}
func (c *Collector) reconcileContextsLocked() {
	if len(c.artifact.ValidatorContexts) == 0 {
		return
	}
	summary := &ValidatorDeliveryState{HistoryCoverage: "complete", Collection: "complete", Delivery: "complete"}
	for i := range c.artifact.ValidatorContexts {
		ctx := &c.artifact.ValidatorContexts[i]
		c.reconcileContext(ctx)
		if ctx.Collection != "complete" {
			summary.Collection = "partial"
		}
		if ctx.Delivery != "complete" || !ctx.ScopeComplete || len(ctx.Gaps) > 0 {
			summary.Delivery = "partial"
		}
		if ctx.History != "complete" {
			summary.HistoryCoverage = "partial"
		}
		for _, gap := range ctx.Gaps {
			if !slices.Contains(summary.Gaps, gap) {
				summary.Gaps = append(summary.Gaps, gap)
			}
		}
	}
	c.artifact.ValidatorDelivery = summary
}

// FilterMeasurementSessions is used by explicit audit replay. It does no
// producer I/O and does not modify an already sealed snapshot.
func FilterMeasurementSessions(artifact *Artifact, allowed map[string]struct{}) {
	heads := []MeasurementHead{}
	for i := range artifact.MeasurementHeads {
		h := &artifact.MeasurementHeads[i]
		if _, ok := allowed[h.Attribution.ExecutionSessionID]; ok {
			heads = append(heads, *h)
		}
	}
	artifact.MeasurementHeads = heads
	contexts := []DeliveryContext{}
	for i := range artifact.ValidatorContexts {
		ctx := &artifact.ValidatorContexts[i]
		if _, ok := allowed[ctx.Attribution.ExecutionSessionID]; ok {
			contexts = append(contexts, *ctx)
		}
	}
	artifact.ValidatorContexts = contexts
	native := []NativeMeasurement{}
	for i := range artifact.NativeMeasurements {
		n := &artifact.NativeMeasurements[i]
		if _, ok := allowed[n.Attribution.ExecutionSessionID]; ok {
			native = append(native, *n)
		}
	}
	artifact.NativeMeasurements = native
	artifact.ValidatorDelivery = nil
	c := Collector{artifact: *artifact}
	c.refreshMeasurementsLocked()
	*artifact = c.artifact
}

func validatorStepProjection(head *MeasurementHead, record *measurements.Record, usable bool) StepRecord {
	p := &record.Payload
	usage := &model.UsageRecord{Status: model.UsageUnavailable, Reason: model.UnavailableNestedMetricsInvalid, CLI: "agent-validator", Source: "agent-validator:metrics", Tokens: model.TokenCounts{}}
	if usable {
		usage.Status = model.UsageCollected
		usage.Reason = ""
		usage.Completeness = model.CompletenessPartial
		for source, target := range map[string]string{"input_total": model.TokenInput, "cache_read": model.TokenCachedInput, "cache_write": model.TokenCacheWrite, "output": model.TokenOutput, "reasoning": model.TokenReasoning} {
			v := p.Tokens[source]
			if v.Value != nil && v.Availability != "unavailable" {
				usage.Tokens[target] = int64(*v.Value)
			}
		}
		if v := p.Tokens["normalized_total"]; v.Availability == "available" && v.Value != nil && p.Completeness["collection"] == "complete" {
			usage.TokenTotals = &model.TokenTotals{Total: int64(*v.Value)}
			if v := p.Tokens["input_total"]; v.Availability == "available" && v.Value != nil {
				usage.TokenTotals.Input = int64(*v.Value)
			}
			if v := p.Tokens["output"]; v.Availability == "available" && v.Value != nil {
				usage.TokenTotals.Output = int64(*v.Value)
			}
		}
	}
	step := StepRecord{RecordID: "measurement/" + head.Key, MeasurementKey: head.Key, ID: record.ID, Prefix: strings.Trim(head.Attribution.Prefix+"/"+head.Attribution.StepID, "/"), Kind: "nested-agent", Type: "agent", AgentInvoked: true, Attempt: 1, Outcome: p.Outcome, SessionID: p.SessionID, ExecutionSessionID: head.Attribution.ExecutionSessionID, ParentAttemptID: head.Attribution.ParentAttemptID, InvocationID: p.InvocationID, Role: "implementation-validator", Tool: "agent-validator", Usage: usage}
	if usable {
		step.EstimatedAPICostUSD = CompleteUSD(p.Costs)
	}
	if p.Lifecycle.StartedAt != nil && p.Lifecycle.EndedAt != nil {
		start, e1 := time.Parse(time.RFC3339Nano, *p.Lifecycle.StartedAt)
		end, e2 := time.Parse(time.RFC3339Nano, *p.Lifecycle.EndedAt)
		if e1 == nil && e2 == nil && end.After(start) {
			step.DurationMS = end.Sub(start).Milliseconds()
		}
	}
	return step
}

func (c *Collector) reconcileContext(ctx *DeliveryContext) {
	invocations := map[string]*measurements.Record{}
	attempts := map[string]*measurements.Record{}
	for i := range c.artifact.MeasurementHeads {
		head := &c.artifact.MeasurementHeads[i]
		if head.Attribution.ContextID != ctx.Attribution.ContextID || head.Status != "supported" {
			continue
		}
		var r measurements.Record
		if json.Unmarshal(head.Record, &r) != nil {
			continue
		}
		if r.Type == "invocation" {
			invocations[r.ID] = &r
		} else {
			attempts[r.ID] = &r
		}
	}
	// Recompute collection, but keep producer delivery gaps independent.
	ctx.Collection = "complete"
	if len(invocations) == 0 {
		ctx.Collection = "unavailable"
	}
	for _, inv := range invocations {
		p := inv.Payload
		if p.Lifecycle.State == "running" || len(p.AttemptIDs) == 0 && !p.ZeroDispatch || p.ZeroDispatch && len(p.AttemptIDs) != 0 {
			ctx.Collection = "partial"
		}
		for _, id := range p.AttemptIDs {
			attempt, ok := attempts[id]
			if !ok || attempt.Payload.Lifecycle.State == "prepared" || attempt.Payload.Lifecycle.State == "running" {
				ctx.Collection = "partial"
			}
		}
	}
	for _, attempt := range attempts {
		if attempt.Payload.Completeness["collection"] != "complete" {
			ctx.Collection = "partial"
		}
		if attempt.Payload.Completeness["history"] != "complete" {
			ctx.History = "partial"
		}
		inv, ok := invocations[attempt.Payload.InvocationID]
		if !ok || !slices.Contains(inv.Payload.AttemptIDs, attempt.ID) {
			ctx.Collection = "partial"
		}
	}
}

func (c *Collector) ValidatorContextComplete(contextID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.artifact.ValidatorContexts {
		ctx := &c.artifact.ValidatorContexts[i]
		if ctx.Attribution.ContextID == contextID {
			return ctx.Collection == "complete" && ctx.History == "complete" && ctx.Delivery == "complete" && ctx.ScopeComplete && len(ctx.Gaps) == 0
		}
	}
	return false
}
func (p *Pipeline) ValidatorContextComplete(contextID string) bool {
	return p.collector.ValidatorContextComplete(contextID)
}

func attemptAggregateTokens(p *measurements.Payload) map[string]measurements.Value {
	if p.Completeness["collection"] == "complete" {
		return p.Tokens
	}
	tokens := map[string]measurements.Value{}
	for name, value := range p.Tokens {
		if value.Availability == "available" {
			value.Availability = "partial"
		}
		tokens[name] = value
	}
	return tokens
}
