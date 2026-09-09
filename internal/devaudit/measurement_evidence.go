//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"strings"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

// Join by persisted structural ownership. ParentAttemptID is provenance, never
// a foreign key to a step record. Longest matching leaf owns a descendant.
func measurementOwner(prefix, id string, leaves []string) string {
	path := logicalPath(prefix, id)
	owner := ""
	for _, leaf := range leaves {
		if (path == leaf || strings.HasPrefix(path, leaf+"/")) && len(leaf) > len(owner) {
			owner = leaf
		}
	}
	return owner
}

type leafModelEvidence struct {
	Dispatches int                          `json:"model_dispatches"`
	Heads      []*metrics.MeasurementHead   `json:"measurement_heads"`
	Native     []*metrics.NativeMeasurement `json:"native_measurements"`
	Contexts   []*metrics.DeliveryContext   `json:"validator_contexts"`
	Gaps       []string                     `json:"gaps"`
}

func collectLeafModelEvidence(artifact *metrics.Artifact, keys []string, session, path string) leafModelEvidence {
	result := leafModelEvidence{Heads: []*metrics.MeasurementHead{}, Native: []*metrics.NativeMeasurement{}, Contexts: []*metrics.DeliveryContext{}, Gaps: []string{}}
	for i := range artifact.MeasurementHeads {
		head := &artifact.MeasurementHeads[i]
		if head.Attribution.ExecutionSessionID != session {
			continue
		}
		switch measurementOwner(head.Attribution.Prefix, head.Attribution.StepID, keys) {
		case path:
			result.Heads = append(result.Heads, head)
		case "":
			result.Gaps = append(result.Gaps, "measurement_ownership_unavailable")
		}
	}
	for i := range artifact.NativeMeasurements {
		n := &artifact.NativeMeasurements[i]
		if n.Attribution.ExecutionSessionID == session && measurementOwner(n.Attribution.Prefix, n.Attribution.StepID, keys) == path {
			result.Native = append(result.Native, n)
		}
	}
	for i := range artifact.ValidatorContexts {
		ctx := &artifact.ValidatorContexts[i]
		if ctx.Attribution.ExecutionSessionID != session {
			continue
		}
		switch measurementOwner(ctx.Attribution.Prefix, ctx.Attribution.StepID, keys) {
		case path:
			result.Contexts = append(result.Contexts, ctx)
			result.Gaps = append(result.Gaps, ctx.Gaps...)
			if ctx.Collection != "complete" || ctx.Delivery != "complete" {
				result.Gaps = append(result.Gaps, "validator_evidence_incomplete")
			}
		case "":
			result.Gaps = append(result.Gaps, "context_ownership_unavailable")
		}
	}
	return result
}
func leafModelPopulation(artifact *metrics.Artifact, keys []string, session, path string) ([]*metrics.StepRecord, bool) {
	population := []*metrics.StepRecord{}
	hasChildren := false
	for i := range artifact.Steps {
		record := &artifact.Steps[i]
		if record.ExecutionSessionID != session {
			continue
		}
		if valueLeaf(record) {
			if logicalPath(record.Prefix, record.ID) == path {
				population = append(population, record)
			}
			continue
		}
		if measurementOwner(record.Prefix, record.ID, keys) == path {
			population = append(population, record)
			hasChildren = true
		}
	}
	return population, hasChildren
}
func leafModelScalars(population []*metrics.StepRecord, gaps []string) (tokenResult *int64, costResult *float64, dispatches int) {
	var tokens int64
	var cost float64
	dispatches = 0
	tokenKnown, costKnown := len(gaps) == 0, len(gaps) == 0
	for _, record := range population {
		if !record.AgentInvoked || record.Type != "agent" {
			continue
		}
		dispatches++
		if record.Usage == nil || record.Usage.Status != model.UsageCollected || record.Usage.TokenTotals == nil {
			tokenKnown = false
		} else {
			tokens += record.Usage.TokenTotals.Total
		}
		if record.EstimatedAPICostUSD == nil {
			costKnown = false
		} else {
			cost += *record.EstimatedAPICostUSD
		}
	}
	if tokenKnown {
		tokenResult = &tokens
	}
	if costKnown {
		costResult = &cost
	}
	return tokenResult, costResult, dispatches
}
func (e *leafModelEvidence) observedModels() []string {
	models := map[string]struct{}{}
	for _, n := range e.Native {
		for _, observed := range n.Observed {
			models[observed.Model] = struct{}{}
		}
	}
	for _, head := range e.Heads {
		var r measurements.Record
		if json.Unmarshal(head.Record, &r) == nil {
			for _, observed := range r.Payload.Observed {
				models[observed.Model] = struct{}{}
			}
		}
	}
	return sortedKeys(models)
}
func attachMeasurementEvidence(leaf *LeafEvidence, artifact *metrics.Artifact, keys []string, session string) {
	path := leaf.Skeleton.StepID
	evidence := collectLeafModelEvidence(artifact, keys, session, path)
	population, children := leafModelPopulation(artifact, keys, session, path)
	if !children && len(evidence.Heads) == 0 && len(evidence.Native) == 0 && len(evidence.Contexts) == 0 && len(evidence.Gaps) == 0 {
		return
	}
	leaf.Skeleton.Cost.TotalTokens, leaf.Skeleton.Cost.CostUSD, evidence.Dispatches = leafModelScalars(population, evidence.Gaps)
	leaf.Skeleton.Cost.SourceModels = evidence.observedModels()
	detail, _ := json.Marshal(evidence)
	leaf.Evidence = append(leaf.Evidence, EvidenceReference{ID: "measurements-" + leaf.Skeleton.ObservationID, Category: "metrics", Status: "available", ProducerExecutionSession: session, Lineage: leaf.Skeleton.Lineage, Detail: string(detail), LocalPath: metrics.FileName})
}
