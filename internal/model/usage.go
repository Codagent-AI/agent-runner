package model

// TokenCounts maps canonical token categories to counts. Categories that a
// source does not report are omitted.
type TokenCounts map[string]int64

const (
	TokenInput       = "input"
	TokenCachedInput = "cached_input"
	TokenCacheWrite  = "cache_write"
	TokenOutput      = "output"
	TokenReasoning   = "reasoning"
)

// UsageStatus describes whether usage was measured for an invocation.
type UsageStatus string

const (
	UsageCollected   UsageStatus = "collected"
	UsageUnavailable UsageStatus = "unavailable"
)

// Completeness describes whether a collected usage record includes every
// category exposed by its source.
type Completeness string

const (
	CompletenessComplete Completeness = "complete"
	CompletenessPartial  Completeness = "partial"
)

// UnavailableReason explains why usage could not be attributed.
type UnavailableReason string

const (
	UnavailablePTYContext         UnavailableReason = "pty-context"
	UnavailableParseFailure       UnavailableReason = "parse-failure"
	UnavailableNoUsageEvent       UnavailableReason = "no-usage-event"
	UnavailableNoBaseline         UnavailableReason = "no-baseline"
	UnavailableCounterReset       UnavailableReason = "counter-reset"
	UnavailableUnsupportedAdapter UnavailableReason = "unsupported-adapter"
)

// UsageRecord is the typed usage value passed through audit events and stored
// in the metrics artifact.
type UsageRecord struct {
	Status        UsageStatus       `json:"status"`
	Reason        UnavailableReason `json:"reason,omitempty"`
	CLI           string            `json:"cli"`
	Provider      string            `json:"provider,omitempty"`
	Model         string            `json:"model,omitempty"`
	Tokens        TokenCounts       `json:"tokens,omitempty"`
	RawCumulative TokenCounts       `json:"raw_cumulative,omitempty"`
	Source        string            `json:"source"`
	Completeness  Completeness      `json:"completeness,omitempty"`
}

// ExecutionIdentity identifies one terminal step or loop-iteration event.
// Attempt is assigned by the metrics collector.
type ExecutionIdentity struct {
	StepID          string `json:"step_id"`
	Prefix          string `json:"prefix"`
	StepType        string `json:"step_type"`
	Kind            string `json:"kind"`
	Attempt         int    `json:"attempt"`
	Iteration       int    `json:"iteration,omitempty"`
	CLI             string `json:"cli,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	SessionStrategy string `json:"session_strategy,omitempty"`
	AgentInvoked    bool   `json:"agent_invoked"`
}

// Coverage describes how much of the eligible agent-step population reported
// a metric.
type Coverage string

const (
	CoverageComplete Coverage = "complete"
	CoveragePartial  Coverage = "partial"
	CoverageNone     Coverage = "none"
)

// RunTotals contains aggregate metrics for all attempts known to a run.
type RunTotals struct {
	ActiveDurationMS    int64       `json:"active_duration_ms"`
	Tokens              TokenCounts `json:"tokens"`
	UsageCoverage       Coverage    `json:"usage_coverage"`
	EstimatedAPICostUSD *float64    `json:"estimated_api_cost_usd"`
	CostCoverage        Coverage    `json:"cost_coverage"`
}
