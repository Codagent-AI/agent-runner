package metrics

import (
	"fmt"
	"strings"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
)

// NativeMeasurement is Runner's independently versioned envelope. It uses the
// common value vocabulary without claiming to be a Validator-produced record.
type NativeMeasurement struct {
	Allocations        []NativeAllocation              `json:"allocations,omitempty"`
	SubagentCollection *NativeSubagentCollection       `json:"subagent_collection,omitempty"`
	Version            int                             `json:"native_measurement_schema_version"`
	Key                string                          `json:"key"`
	Attribution        Attribution                     `json:"attribution"`
	Producer           string                          `json:"producer"`
	Provenance         string                          `json:"provenance"`
	SourceFormat       string                          `json:"source_format"`
	Requested          measurements.Identity           `json:"requested_identity"`
	Resolved           measurements.Identity           `json:"resolved_identity"`
	Observed           []measurements.ObservedIdentity `json:"observed_identities"`
	Tokens             map[string]measurements.Value   `json:"tokens"`
	UnallocatedUsage   *measurements.Value             `json:"unallocated_usage"`
	Costs              []measurements.Cost             `json:"provider_reported_costs"`
	Limitations        []string                        `json:"limitations"`
}

type NativeSubagentCollection struct {
	Completeness model.Completeness      `json:"completeness"`
	Reason       model.UnavailableReason `json:"reason,omitempty"`
}

type NativeAllocation struct {
	ID                  string                        `json:"allocation_id"`
	Kind                string                        `json:"kind"`
	ObservedIdentityRef *string                       `json:"observed_identity_ref"`
	AgentType           string                        `json:"agent_type,omitempty"`
	ToolUseID           string                        `json:"tool_use_id,omitempty"`
	ParentToolUseID     string                        `json:"parent_tool_use_id,omitempty"`
	SpawnDepth          int                           `json:"spawn_depth,omitempty"`
	Availability        string                        `json:"availability"`
	Reason              *string                       `json:"reason"`
	Tokens              map[string]measurements.Value `json:"tokens"`
	Cost                measurements.Value            `json:"cost"`
}

func pointer[T any](value T) *T { return &value }
func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func missingValue(reason string) measurements.Value {
	return measurements.Value{Availability: "unavailable", Reason: &reason}
}
func nativeValue(count int64, derived bool) measurements.Value {
	if count < 0 || count > 9007199254740991 {
		return missingValue("unsafe_token_integer")
	}
	value := measurements.Value{Availability: "available", Value: pointer(float64(count)), Source: pointer("provider_usage"), Origin: pointer("observed"), Precision: pointer("exact")}
	if derived {
		value.Source = pointer("runner_derivation")
		value.Origin = pointer("derived")
		value.Derivation = pointer("adapter_normalization")
	}
	return value
}
func nativeDerivedValue(count int64, derivation string) measurements.Value {
	value := nativeValue(count, true)
	if value.Availability == "available" {
		value.Derivation = pointer(derivation)
	}
	return value
}
func nativeUncachedInput(u *model.UsageRecord, legacy bool) measurements.Value {
	if legacy {
		return missingValue("not_reported")
	}
	switch u.CLI {
	case "claude", "opencode":
		if input, ok := u.Tokens[model.TokenInput]; ok {
			return nativeValue(input, false)
		}
	case "codex":
		input, hasInput := u.Tokens[model.TokenInput]
		cached, hasCached := u.Tokens[model.TokenCachedInput]
		if hasInput && hasCached && input >= 0 && cached >= 0 && cached <= input {
			return nativeDerivedValue(input-cached, "codex_input_total_minus_cache_read")
		}
	}
	return missingValue("not_reported")
}

func telemetryObservedIdentity(id, observedModel string) measurements.ObservedIdentity {
	return measurements.ObservedIdentity{ID: id, Model: observedModel, Provenance: "telemetry", Provider: missingString("not_reported"), Effort: missingString("not_reported")}
}
func missingString(reason string) measurements.StringEvidence {
	return measurements.StringEvidence{Availability: "unavailable", Reason: &reason}
}

// nativeTokenFields projects a usage record's token categories and canonical
// totals onto the common field vocabulary.
func nativeTokenFields(u *model.UsageRecord, legacy bool) map[string]measurements.Value {
	tokens := map[string]measurements.Value{}
	for _, name := range CanonicalFields {
		tokens[name] = missingValue("not_reported")
	}
	if u.Status != model.UsageCollected {
		return tokens
	}
	for old, name := range map[string]string{model.TokenCachedInput: "cache_read", model.TokenCacheWrite: "cache_write", model.TokenOutput: "output", model.TokenReasoning: "reasoning"} {
		if count, ok := u.Tokens[old]; ok {
			tokens[name] = nativeValue(count, false)
		}
	}
	if u.TokenTotals != nil {
		tokens["input_total"] = nativeValue(u.TokenTotals.Input, true)
		tokens["output"] = nativeValue(u.TokenTotals.Output, true)
		tokens["normalized_total"] = nativeValue(u.TokenTotals.Total, true)
		for _, name := range []string{"input_total", "output"} {
			v := tokens[name]
			v.IncludedIn = []string{"normalized_total"}
			tokens[name] = v
		}
	}
	// Claude and OpenCode report exclusive input. Codex input_tokens includes
	// cached_input_tokens, so its uncached input is their difference.
	// Existing adapter totals retain cumulative baseline protection.
	tokens["input_uncached"] = nativeUncachedInput(u, legacy)
	return tokens
}

// nativeAllocation projects one main-thread or subagent allocation. Its
// tokens are evidence only; aggregates read the attempt-level fields.
func nativeAllocation(a *model.UsageAllocation, cliName string, legacy bool, identityRef *string) NativeAllocation {
	entry := NativeAllocation{ID: a.ID, Kind: a.Kind, ObservedIdentityRef: identityRef, AgentType: a.AgentType, ToolUseID: a.ToolUseID, ParentToolUseID: a.ParentToolUseID, SpawnDepth: a.SpawnDepth, Availability: "available", Cost: missingValue("allocation_cost_not_reported")}
	switch {
	case a.Status == model.UsageUnavailable:
		entry.Availability = "unavailable"
	case a.Completeness == model.CompletenessPartial:
		entry.Availability = "partial"
	}
	if a.Reason != "" {
		entry.Reason = pointer(string(a.Reason))
	}
	entry.Tokens = nativeTokenFields(&model.UsageRecord{CLI: cliName, Status: a.Status, Tokens: a.Tokens, TokenTotals: a.TokenTotals}, legacy)
	return entry
}

func nativeMeasurement(runID string, step *StepRecord, legacy bool) NativeMeasurement {
	n := NativeMeasurement{Version: 1, Key: step.RecordID, Attribution: Attribution{RunID: runID, ExecutionSessionID: step.ExecutionSessionID, StepID: step.ID, Prefix: step.Prefix, ParentAttemptID: step.ParentAttemptID}, Producer: "agent-runner", Provenance: "native", Observed: []measurements.ObservedIdentity{}, Tokens: map[string]measurements.Value{}, Costs: []measurements.Cost{}, Limitations: []string{}}
	for _, name := range CanonicalFields {
		n.Tokens[name] = missingValue("not_reported")
	}
	if legacy {
		n.Provenance = "legacy"
		n.Limitations = append(n.Limitations, "legacy_token_relationships_unknown")
	}
	u := step.Usage
	if u == nil {
		return n
	}
	n.SourceFormat = u.Source
	allocated := len(u.Allocations) > 0
	id := u.Identity
	n.Requested = measurements.Identity{Adapter: nullable(id.RequestedCLI), Model: nullable(id.RequestedModel), Effort: nullable(id.RequestedEffort), Provenance: "configuration"}
	n.Resolved = measurements.Identity{Adapter: nullable(id.EffectiveCLI), Model: nullable(id.EffectiveModel), Provider: nullable(id.EffectiveProvider), Effort: nullable(id.EffectiveEffort), Provenance: "launch_resolution"}
	if id.ModelSource == model.IdentitySourceTelemetry && id.EffectiveModel != "" {
		observedID := "observed-1"
		if allocated {
			observedID = "observed-main"
		}
		observed := telemetryObservedIdentity(observedID, id.EffectiveModel)
		if id.ProviderSource == model.IdentitySourceTelemetry && id.EffectiveProvider != "" {
			observed.Provider = measurements.StringEvidence{Availability: "available", Value: &id.EffectiveProvider}
		}
		if id.EffortSource == model.IdentitySourceTelemetry && id.EffectiveEffort != "" {
			observed.Effort = measurements.StringEvidence{Availability: "available", Value: &id.EffectiveEffort}
		}
		n.Observed = append(n.Observed, observed)
	}
	n.Tokens = nativeTokenFields(u, legacy)
	if allocated {
		n.addAllocations(u, legacy)
	} else if len(n.Observed) == 0 {
		value := n.Tokens["normalized_total"]
		n.UnallocatedUsage = &value
		n.Limitations = append(n.Limitations, "model_allocation_unavailable")
	}
	n.collectReportedCost(step, legacy)
	return n
}

// addAllocations records per-thread allocations, giving each distinct model
// one observed identity, and marks attempt fields partial when subagent
// collection was incomplete.
func (n *NativeMeasurement) addAllocations(u *model.UsageRecord, legacy bool) {
	n.Version = 2
	n.Allocations = make([]NativeAllocation, 0, len(u.Allocations))
	if u.SubagentCollection != "" {
		n.SubagentCollection = &NativeSubagentCollection{Completeness: u.SubagentCollection, Reason: u.SubagentCollectionReason}
	}
	identityRefs := map[string]string{}
	for _, observed := range n.Observed {
		identityRefs[observed.Model] = observed.ID
	}
	for i := range u.Allocations {
		a := &u.Allocations[i]
		var ref *string
		if a.Model != "" {
			id := identityRefs[a.Model]
			if id == "" {
				id = fmt.Sprintf("observed-%d", len(n.Observed)+1)
				identityRefs[a.Model] = id
				n.Observed = append(n.Observed, telemetryObservedIdentity(id, a.Model))
			}
			ref = &id
		}
		n.Allocations = append(n.Allocations, nativeAllocation(a, u.CLI, legacy, ref))
	}
	if u.SubagentCollection != model.CompletenessPartial {
		return
	}
	reason := strings.ReplaceAll(string(u.SubagentCollectionReason), "-", "_")
	for key, value := range n.Tokens {
		if value.Value != nil {
			value.Availability = "partial"
			value.Reason = &reason
			n.Tokens[key] = value
		}
	}
}
func (c *Collector) refreshNativeMeasurementsLocked() {
	old := map[string]NativeMeasurement{}
	for i := range c.artifact.NativeMeasurements {
		n := &c.artifact.NativeMeasurements[i]
		old[n.Key] = *n
	}
	c.artifact.NativeMeasurements = nil
	for i := range c.artifact.Steps {
		step := &c.artifact.Steps[i]
		if step.MeasurementKey != "" || step.Type != "agent" || !step.AgentInvoked {
			continue
		}
		n, exists := old[step.RecordID]
		if !exists {
			n = nativeMeasurement(c.artifact.RunID, step, step.LegacyMeasurement)
		}
		c.artifact.NativeMeasurements = append(c.artifact.NativeMeasurements, n)
	}
}
func accumulateFields(fields map[string]FieldAggregate, tokens map[string]measurements.Value, usable bool) {
	for _, name := range CanonicalFields {
		v := tokens[name]
		a := fields[name]
		if !usable || v.Value == nil {
			a.Missing++
		} else {
			a.KnownSubtotal += *v.Value
			a.Contributing++
			if v.Availability == "partial" {
				a.Partial++
			}
			if v.Precision != nil && *v.Precision == "approximate" {
				a.Precision = "approximate"
			}
		}
		fields[name] = a
	}
}

func (n *NativeMeasurement) collectReportedCost(step *StepRecord, legacy bool) {
	// These adapters obtain this compatibility field directly from their
	// structured provider result; Runner does no price estimation. Legacy or
	// unknown sources retain uncertainty instead of acquiring a new cost scope.
	if step.EstimatedAPICostUSD != nil {
		if !legacy && (n.SourceFormat == "claude:result-event" || n.SourceFormat == "opencode:step_finish") {
			n.Costs = append(n.Costs, measurements.Cost{ID: "reported-cost", Scope: "attempt", Coverage: "full", Overlap: "established", Source: "provider_usage", Currency: measurements.StringEvidence{Availability: "available", Value: pointer("USD")}, Amount: measurements.Value{Availability: "available", Value: pointer(*step.EstimatedAPICostUSD), Source: pointer("provider_usage"), Origin: pointer("observed"), Precision: pointer("exact")}})
		} else {
			n.Limitations = append(n.Limitations, "legacy_cost_scope_unavailable")
		}
	}
}
