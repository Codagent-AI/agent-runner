package model

// CodexRateLimitEvidence contains only Codex-reported quota state. AccountScope
// is a run-local digest; the underlying account identifier is never persisted.
type CodexRateLimitEvidence struct {
	Status           string             `json:"status"`
	Reason           string             `json:"reason,omitempty"`
	Source           string             `json:"source"`
	AttemptStartedAt string             `json:"attempt_started_at"`
	AttemptEndedAt   string             `json:"attempt_ended_at"`
	Start            *RateLimitSnapshot `json:"start"`
	StartProvenance  string             `json:"start_provenance,omitempty"`
	BaselineGapMS    *int64             `json:"baseline_gap_ms,omitempty"`
	AccountScope     string             `json:"account_scope,omitempty"`
	End              *RateLimitSnapshot `json:"end"`
	Deltas           []RateLimitDelta   `json:"deltas"`
}

type RateLimitSnapshot struct {
	ObservedAt string          `json:"observed_at"`
	LimitID    *string         `json:"limit_id"`
	PlanType   *string         `json:"plan_type"`
	Primary    RateLimitWindow `json:"primary"`
	Secondary  RateLimitWindow `json:"secondary"`
}

type RateLimitWindow struct {
	Reported      bool     `json:"reported"`
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *int64   `json:"window_minutes"`
	ResetsAt      *int64   `json:"resets_at"`
}

type RateLimitDelta struct {
	Window           string   `json:"window"`
	Availability     string   `json:"availability"`
	PercentagePoints *float64 `json:"percentage_points"`
	Reason           string   `json:"reason,omitempty"`
	Precision        string   `json:"precision,omitempty"`
	Limitations      []string `json:"limitations,omitempty"`
}
