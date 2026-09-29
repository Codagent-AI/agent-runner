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

//nolint:gocognit,funlen // Native projection handles independent availability for each field.
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
	if len(u.Allocations) > 0 {
		n.Version = 2
		n.Allocations = make([]NativeAllocation, 0, len(u.Allocations))
		if u.SubagentCollection != "" {
			n.SubagentCollection = &NativeSubagentCollection{Completeness: u.SubagentCollection, Reason: u.SubagentCollectionReason}
		}
	}
	id := u.Identity
	n.Requested = measurements.Identity{Adapter: nullable(id.RequestedCLI), Model: nullable(id.RequestedModel), Effort: nullable(id.RequestedEffort), Provenance: "configuration"}
	n.Resolved = measurements.Identity{Adapter: nullable(id.EffectiveCLI), Model: nullable(id.EffectiveModel), Provider: nullable(id.EffectiveProvider), Effort: nullable(id.EffectiveEffort), Provenance: "launch_resolution"}
	if id.ModelSource == model.IdentitySourceTelemetry && id.EffectiveModel != "" {
		observedID := "observed-1"
		if len(u.Allocations) > 0 {
			observedID = "observed-main"
		}
		observed := measurements.ObservedIdentity{ID: observedID, Model: id.EffectiveModel, Provenance: "telemetry", Provider: measurements.StringEvidence{Availability: "unavailable", Reason: pointer("not_reported")}, Effort: measurements.StringEvidence{Availability: "unavailable", Reason: pointer("not_reported")}}
		if id.ProviderSource == model.IdentitySourceTelemetry && id.EffectiveProvider != "" {
			observed.Provider = measurements.StringEvidence{Availability: "available", Value: &id.EffectiveProvider}
		}
		if id.EffortSource == model.IdentitySourceTelemetry && id.EffectiveEffort != "" {
			observed.Effort = measurements.StringEvidence{Availability: "available", Value: &id.EffectiveEffort}
		}
		n.Observed = append(n.Observed, observed)
	}
	if u.Status == model.UsageCollected {
		for old, name := range map[string]string{model.TokenCachedInput: "cache_read", model.TokenCacheWrite: "cache_write", model.TokenOutput: "output", model.TokenReasoning: "reasoning"} {
			if count, ok := u.Tokens[old]; ok {
				n.Tokens[name] = nativeValue(count, false)
			}
		}
		if u.TokenTotals != nil {
			n.Tokens["input_total"] = nativeValue(u.TokenTotals.Input, true)
			n.Tokens["output"] = nativeValue(u.TokenTotals.Output, true)
			n.Tokens["normalized_total"] = nativeValue(u.TokenTotals.Total, true)
			for _, name := range []string{"input_total", "output"} {
				v := n.Tokens[name]
				v.IncludedIn = []string{"normalized_total"}
				n.Tokens[name] = v
			}
		}
		// Claude and OpenCode report exclusive input. Codex input_tokens includes
		// cached_input_tokens, so its uncached input is their difference.
		// Existing adapter totals retain cumulative baseline protection.
		n.Tokens["input_uncached"] = nativeUncachedInput(u, legacy)
	}
	if len(u.Allocations) > 0 {
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
					n.Observed = append(n.Observed, measurements.ObservedIdentity{ID: id, Model: a.Model, Provenance: "telemetry", Provider: measurements.StringEvidence{Availability: "unavailable", Reason: pointer("not_reported")}, Effort: measurements.StringEvidence{Availability: "unavailable", Reason: pointer("not_reported")}})
				}
				ref = &id
			}
			entry := NativeAllocation{ID: a.ID, Kind: a.Kind, ObservedIdentityRef: ref, AgentType: a.AgentType, ToolUseID: a.ToolUseID, ParentToolUseID: a.ParentToolUseID, SpawnDepth: a.SpawnDepth, Availability: "available", Tokens: map[string]measurements.Value{}, Cost: missingValue("allocation_cost_not_reported")}
			if a.Status == model.UsageUnavailable {
				entry.Availability = "unavailable"
			} else if a.Completeness == model.CompletenessPartial {
				entry.Availability = "partial"
			}
			if a.Reason != "" {
				entry.Reason = pointer(string(a.Reason))
			}
			for _, field := range CanonicalFields {
				entry.Tokens[field] = missingValue("not_reported")
			}
			for key, name := range map[string]string{model.TokenInput: "input_uncached", model.TokenCachedInput: "cache_read", model.TokenCacheWrite: "cache_write", model.TokenOutput: "output"} {
				if count, ok := a.Tokens[key]; ok {
					entry.Tokens[name] = nativeValue(count, false)
				}
			}
			if a.TokenTotals != nil {
				entry.Tokens["input_total"] = nativeValue(a.TokenTotals.Input, true)
				entry.Tokens["normalized_total"] = nativeValue(a.TokenTotals.Total, true)
			}
			n.Allocations = append(n.Allocations, entry)
		}
	}
	if len(n.Observed) == 0 && len(u.Allocations) == 0 {
		value := n.Tokens["normalized_total"]
		n.UnallocatedUsage = &value
		n.Limitations = append(n.Limitations, "model_allocation_unavailable")
	}
	if u.SubagentCollection == model.CompletenessPartial {
		reason := strings.ReplaceAll(string(u.SubagentCollectionReason), "-", "_")
		for key, value := range n.Tokens {
			if value.Value != nil {
				value.Availability = "partial"
				value.Reason = &reason
				n.Tokens[key] = value
			}
		}
	}
	n.collectReportedCost(step, legacy)
	return n
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
