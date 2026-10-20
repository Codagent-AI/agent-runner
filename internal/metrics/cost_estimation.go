package metrics

import (
	"math/big"
	"slices"
	"strconv"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pricing"
)

// CostEstimate retains compact provenance; individual requests remain transient.
type CostEstimate struct {
	Policy              string   `json:"policy"`
	Model               string   `json:"model,omitempty"`
	Requests            int      `json:"requests"`
	LongContextRequests int      `json:"long_context_requests"`
	Limitations         []string `json:"limitations,omitempty"`
	PartialSubtotalUSD  *float64 `json:"partial_subtotal_usd,omitempty"`
}
type costOutcome struct {
	amount   *float64
	reason   model.UnavailableReason
	version  string
	estimate *CostEstimate
}

func attributionComplete(u *model.UsageRecord) bool {
	return u.Completeness != model.CompletenessPartial || u.Reason == ""
}
func estimateCost(u *model.UsageRecord) costOutcome {
	fail := func(reason model.UnavailableReason) costOutcome { return costOutcome{reason: reason} }
	if u.Status != model.UsageCollected {
		return fail(model.UnavailableUsageUnavailable)
	}
	if !attributionComplete(u) {
		return fail(model.UnavailableUsageIncomplete)
	}
	for _, category := range []string{model.TokenInput, model.TokenCachedInput, model.TokenOutput} {
		if _, ok := u.Tokens[category]; !ok {
			return fail(model.UnavailableRequiredCategoryMissing)
		}
	}
	name := u.Identity.EffectiveModel
	if name == "" {
		name = u.Model
	}
	if name == "" {
		return fail(model.UnavailableModelUnknown)
	}
	table, err := pricing.Load()
	if err != nil {
		panic(err)
	} // Embedded data is validated by package tests.
	provider := u.Identity.EffectiveProvider
	if provider == "" {
		provider = u.Provider
	}
	if provider == "" {
		provider = "openai"
	}
	entry, ok := table.Lookup(provider, name)
	if !ok {
		return fail(model.UnavailableModelUnpriced)
	}
	evidence := u.RequestEvidence
	if evidence == nil || evidence.Status != model.UsageCollected || len(evidence.Requests) == 0 {
		return fail(model.UnavailableContextBandUnknown)
	}
	var input, cached, output int64
	for _, r := range evidence.Requests {
		input += r.Input
		cached += r.Cached
		output += r.Output
	}
	if input != u.Tokens[model.TokenInput] || cached != u.Tokens[model.TokenCachedInput] || output != u.Tokens[model.TokenOutput] {
		return fail(model.UnavailableRequestEvidenceMismatch)
	}
	result := pricing.Estimate(entry, evidence.Requests)
	if result.Reason != "" {
		return fail(result.Reason)
	}
	return costOutcome{amount: &result.AmountUSD, version: table.Version, estimate: &CostEstimate{Policy: table.Policy, Model: name, Requests: result.Requests, LongContextRequests: result.LongContextRequests, Limitations: result.Limitations}}
}
func estimateTurns(u *model.UsageRecord) costOutcome {
	if u.Status != model.UsageCollected {
		return costOutcome{reason: model.UnavailableUsageUnavailable}
	}
	table, err := pricing.Load()
	if err != nil {
		panic(err)
	}
	result := costOutcome{version: table.Version, estimate: &CostEstimate{Policy: table.Policy}}
	// Attribution failure outranks category or model failures on any other turn.
	for i := range u.Turns {
		if u.Turns[i].Status != model.UsageCollected {
			result.reason = model.UnavailableTurnUnattributed
			break
		}
	}
	sum := new(big.Rat)
	eligible := 0
	for i := range u.Turns {
		turn := estimateCost(&u.Turns[i])
		if turn.amount == nil {
			if result.reason == "" {
				result.reason = turn.reason
			}
			continue
		}
		eligible++
		value, _ := new(big.Rat).SetString(strconv.FormatFloat(*turn.amount, 'f', -1, 64))
		sum.Add(sum, value)
		e := result.estimate
		e.Requests += turn.estimate.Requests
		e.LongContextRequests += turn.estimate.LongContextRequests
		if eligible == 1 {
			e.Model = turn.estimate.Model
		} else if e.Model != turn.estimate.Model {
			e.Model = "mixed"
		}
		for _, limitation := range turn.estimate.Limitations {
			if !slices.Contains(e.Limitations, limitation) {
				e.Limitations = append(e.Limitations, limitation)
			}
		}
	}
	subtotal, _ := sum.Float64()
	if result.reason == "" {
		result.amount = &subtotal
	} else if eligible > 0 {
		result.estimate.PartialSubtotalUSD = &subtotal
	}
	return result
}

func (c *Collector) resolveCost(event *audit.Event, record *StepRecord, usage *model.UsageRecord) {
	if cost, ok := event.Data[DataEstimatedAPICostUSD].(*float64); ok && cost != nil {
		value := *cost
		record.EstimatedAPICostUSD = &value
		record.CostBasis = model.CostBasisReported
	} else if usage.CLI == "codex" && event.Type != audit.EventNestedAgentEnd {
		outcome := estimateCost(usage)
		if len(usage.Turns) > 0 {
			outcome = estimateTurns(usage)
		}
		record.EstimatedAPICostUSD = outcome.amount
		record.CostUnavailableReason = outcome.reason
		record.PriceTableVersion = outcome.version
		record.CostEstimate = outcome.estimate
		if outcome.amount != nil {
			record.CostBasis = model.CostBasisEstimated
		}
	}
	event.Data[DataEstimatedAPICostUSD] = record.EstimatedAPICostUSD
	if record.CostBasis != "" {
		event.Data["cost_basis"] = record.CostBasis
	}
	if record.CostUnavailableReason != "" {
		event.Data["cost_unavailable_reason"] = record.CostUnavailableReason
	}
	if record.PriceTableVersion != "" {
		event.Data["price_table_version"] = record.PriceTableVersion
	}
	if record.CostEstimate != nil {
		event.Data["cost_estimate"] = record.CostEstimate
	}
}
