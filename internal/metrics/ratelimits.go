package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
)

type CodexRateLimitRequest struct {
	ThreadID, RunID    string
	StartedAt, EndedAt time.Time
	EndTolerance       time.Duration
}

type RateLimitEnrichment struct {
	HeadKey   string                       `json:"head_key"`
	AttemptID string                       `json:"attempt_id"`
	StartedAt string                       `json:"started_at"`
	EndedAt   string                       `json:"ended_at"`
	Evidence  model.CodexRateLimitEvidence `json:"evidence"`
}

func (c *Collector) SetCodexRateLimitReader(reader func(CodexRateLimitRequest) model.CodexRateLimitEvidence) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rateLimitReader = reader
}

func isCodexMeasurement(record *measurements.Record) bool {
	p := &record.Payload
	return p.Adapter == "codex" || p.Requested.Adapter != nil && *p.Requested.Adapter == "codex" || p.Resolved.Adapter != nil && *p.Resolved.Adapter == "codex"
}

func (c *Collector) captureRateLimitEnrichmentsLocked() {
	if c.rateLimitReader == nil {
		return
	}
	for i := range c.artifact.MeasurementHeads {
		head := &c.artifact.MeasurementHeads[i]
		var record measurements.Record
		if json.Unmarshal(head.Record, &record) != nil || record.Type != "model_attempt" || !isCodexMeasurement(&record) || record.Payload.Lifecycle.StartedAt == nil || record.Payload.Lifecycle.EndedAt == nil {
			continue
		}
		startText, endText := *record.Payload.Lifecycle.StartedAt, *record.Payload.Lifecycle.EndedAt
		start, e1 := time.Parse(time.RFC3339Nano, startText)
		end, e2 := time.Parse(time.RFC3339Nano, endText)
		if e1 != nil || e2 != nil {
			continue
		}
		existing := c.findRateLimitEnrichment(head.Key)
		if existing != nil && existing.AttemptID == record.Payload.AttemptID && existing.StartedAt == startText && existing.EndedAt == endText {
			continue
		}
		threadID := providerSessionID(head.Record)
		evidence := c.rateLimitReader(CodexRateLimitRequest{ThreadID: threadID, RunID: c.artifact.RunID, StartedAt: start, EndedAt: end, EndTolerance: 2 * time.Second})
		enrichment := RateLimitEnrichment{HeadKey: head.Key, AttemptID: record.Payload.AttemptID, StartedAt: startText, EndedAt: endText, Evidence: evidence}
		if existing != nil {
			*existing = enrichment
		} else {
			c.artifact.RateLimitEnrichments = append(c.artifact.RateLimitEnrichments, enrichment)
		}
	}
}

func providerSessionID(raw json.RawMessage) string {
	var record struct {
		Payload struct {
			Native []struct {
				Name  string `json:"name"`
				Value any    `json:"value"`
			} `json:"provider_native_usage"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &record) != nil {
		return ""
	}
	for _, row := range record.Payload.Native {
		if row.Name == "provider_session_id" {
			if id, ok := row.Value.(string); ok {
				return id
			}
		}
	}
	return ""
}

func (c *Collector) findRateLimitEnrichment(key string) *RateLimitEnrichment {
	for i := range c.artifact.RateLimitEnrichments {
		if c.artifact.RateLimitEnrichments[i].HeadKey == key {
			return &c.artifact.RateLimitEnrichments[i]
		}
	}
	return nil
}

func (c *Collector) enrichmentForHead(head *MeasurementHead, record *measurements.Record) *model.CodexRateLimitEvidence {
	e := c.findRateLimitEnrichment(head.Key)
	if head.Status == "conflicting" {
		return unavailableNestedEvidence("excluded-measurement")
	}
	if e == nil {
		return unavailableNestedEvidence("session-unidentified")
	}
	if record.Payload.Lifecycle.StartedAt == nil || record.Payload.Lifecycle.EndedAt == nil || e.AttemptID != record.Payload.AttemptID || e.StartedAt != *record.Payload.Lifecycle.StartedAt || e.EndedAt != *record.Payload.Lifecycle.EndedAt {
		return unavailableNestedEvidence("stale-enrichment")
	}
	value := e.Evidence
	return &value
}

func unavailableNestedEvidence(reason string) *model.CodexRateLimitEvidence {
	return &model.CodexRateLimitEvidence{Status: "unavailable", Reason: reason, Source: "codex:session-log", AccountScope: "unverified", Deltas: []model.RateLimitDelta{{Window: "primary", Availability: "unavailable", Reason: reason}, {Window: "secondary", Availability: "unavailable", Reason: reason}}}
}

type CodexRateLimitRollup struct {
	MeasuredAttempts int                     `json:"measured_attempts"`
	Coverage         []RateLimitRoleCoverage `json:"coverage"`
	Windows          []RateLimitWindowGroup  `json:"windows"`
}
type RateLimitRoleCoverage struct {
	Window    string `json:"window"`
	Measured  int    `json:"measured_attempts"`
	WithDelta int    `json:"attempts_with_delta"`
	Coverage  string `json:"coverage"`
}
type RateLimitWindowGroup struct {
	AccountScope  string               `json:"account_scope"`
	LimitID       *string              `json:"limit_id"`
	Window        string               `json:"window"`
	WindowMinutes int64                `json:"window_minutes"`
	ResetsAt      int64                `json:"resets_at"`
	DeltaSum      model.RateLimitDelta `json:"delta_sum"`
	Contributing  int                  `json:"contributing_attempts"`
	Span          model.RateLimitDelta `json:"run_span"`
	Limitations   []string             `json:"limitations,omitempty"`
}

type windowAccumulator struct {
	group     RateLimitWindowGroup
	start     *model.RateLimitSnapshot
	end       *model.RateLimitSnapshot
	intervals [][2]time.Time
}

//nolint:gocognit,cyclop,funlen // Grouping, coverage, and span are one deterministic projection.
func rollupCodexRateLimits(steps []StepRecord) *CodexRateLimitRollup {
	rollup := &CodexRateLimitRollup{Coverage: []RateLimitRoleCoverage{{Window: "primary"}, {Window: "secondary"}}, Windows: []RateLimitWindowGroup{}}
	groups := map[string]*windowAccumulator{}
	for i := range steps {
		e := steps[i].CodexRateLimits
		if e == nil {
			continue
		}
		rollup.MeasuredAttempts++
		for roleIndex, role := range []string{"primary", "secondary"} {
			coverage := &rollup.Coverage[roleIndex]
			coverage.Measured++
			delta := deltaForRole(e.Deltas, role)
			if delta != nil && delta.Availability == "available" {
				coverage.WithDelta++
			}
			if e.End == nil {
				continue
			}
			endWindow := windowForRole(e.End, role)
			if !endWindow.Reported || endWindow.WindowMinutes == nil || endWindow.ResetsAt == nil {
				continue
			}
			limit := ""
			if e.End.LimitID != nil {
				limit = *e.End.LimitID
			}
			key := fmt.Sprintf("%q/%q/%s/%d/%d", e.AccountScope, limit, role, *endWindow.WindowMinutes, *endWindow.ResetsAt)
			acc := groups[key]
			if acc == nil {
				acc = &windowAccumulator{group: RateLimitWindowGroup{AccountScope: e.AccountScope, LimitID: e.End.LimitID, Window: role, WindowMinutes: *endWindow.WindowMinutes, ResetsAt: *endWindow.ResetsAt,
					DeltaSum: model.RateLimitDelta{Window: role, Availability: "unavailable", Reason: "no-baseline"}, Span: model.RateLimitDelta{Window: role, Availability: "unavailable", Reason: "no-baseline"}}}
				groups[key] = acc
			}
			if delta != nil && delta.Availability == "available" && delta.PercentagePoints != nil {
				acc.group.Contributing++
				if e.AccountScope != "unverified" {
					if acc.group.DeltaSum.PercentagePoints == nil {
						zero := 0.0
						acc.group.DeltaSum = model.RateLimitDelta{Window: role, Availability: "available", PercentagePoints: &zero, Precision: "approximate", Limitations: []string{"account-wide", "coarse-precision"}}
					}
					*acc.group.DeltaSum.PercentagePoints += *delta.PercentagePoints
				}
			}
			if e.Start != nil {
				sw := windowForRole(e.Start, role)
				if sw.Reported && sameWindowForRollup(e.Start, e.End, sw, endWindow) && (acc.start == nil || observedBefore(e.Start, acc.start)) {
					acc.start = e.Start
				}
			}
			if acc.end == nil || observedBefore(acc.end, e.End) {
				acc.end = e.End
			}
			start, err1 := time.Parse(time.RFC3339Nano, e.AttemptStartedAt)
			end, err2 := time.Parse(time.RFC3339Nano, e.AttemptEndedAt)
			if err1 == nil && err2 == nil {
				acc.intervals = append(acc.intervals, [2]time.Time{start, end})
			}
		}
	}
	if rollup.MeasuredAttempts == 0 {
		return nil
	}
	for i := range rollup.Coverage {
		c := &rollup.Coverage[i]
		c.Coverage = "partial"
		switch c.WithDelta {
		case 0:
			c.Coverage = "none"
		default:
			if c.WithDelta == c.Measured {
				c.Coverage = "complete"
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		acc := groups[key]
		if acc.group.AccountScope == "unverified" {
			acc.group.DeltaSum = model.RateLimitDelta{Window: acc.group.Window, Availability: "unavailable", Reason: "account-unverified"}
			acc.group.Span = model.RateLimitDelta{Window: acc.group.Window, Availability: "unavailable", Reason: "account-unverified"}
		} else if acc.start != nil {
			acc.group.Span = rollupSpan(acc.start, acc.end, acc.group.Window)
		}
		sort.Slice(acc.intervals, func(i, j int) bool { return acc.intervals[i][0].Before(acc.intervals[j][0]) })
		var latestEnd time.Time
		for i := 1; i < len(acc.intervals); i++ {
			if i == 1 || acc.intervals[i-1][1].After(latestEnd) {
				latestEnd = acc.intervals[i-1][1]
			}
			if acc.intervals[i][0].Before(latestEnd) {
				acc.group.Limitations = []string{"overlapping-attempts"}
				acc.group.DeltaSum.Limitations = append(acc.group.DeltaSum.Limitations, "overlapping-attempts")
				break
			}
		}
		rollup.Windows = append(rollup.Windows, acc.group)
	}
	return rollup
}

func windowForRole(snapshot *model.RateLimitSnapshot, role string) model.RateLimitWindow {
	if role == "secondary" {
		return snapshot.Secondary
	}
	return snapshot.Primary
}
func deltaForRole(deltas []model.RateLimitDelta, role string) *model.RateLimitDelta {
	for i := range deltas {
		if deltas[i].Window == role {
			return &deltas[i]
		}
	}
	return nil
}
func sameWindowForRollup(a, b *model.RateLimitSnapshot, aw, bw model.RateLimitWindow) bool {
	return sameStringValue(a.LimitID, b.LimitID) && sameIntValue(aw.WindowMinutes, bw.WindowMinutes) && sameIntValue(aw.ResetsAt, bw.ResetsAt)
}
func sameStringValue(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func sameIntValue(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func rollupSpan(start, end *model.RateLimitSnapshot, role string) model.RateLimitDelta {
	sw, ew := windowForRole(start, role), windowForRole(end, role)
	d := model.RateLimitDelta{Window: role, Availability: "unavailable", Reason: "no-baseline"}
	if sw.UsedPercent != nil && ew.UsedPercent != nil {
		value := *ew.UsedPercent - *sw.UsedPercent
		if value >= 0 {
			d = model.RateLimitDelta{Window: role, Availability: "available", PercentagePoints: &value, Precision: "approximate", Limitations: []string{"account-wide", "coarse-precision"}}
		} else {
			d.Reason = "inconsistent"
		}
	}
	return d
}

func observedBefore(a, b *model.RateLimitSnapshot) bool {
	aTime, aErr := time.Parse(time.RFC3339Nano, a.ObservedAt)
	bTime, bErr := time.Parse(time.RFC3339Nano, b.ObservedAt)
	return aErr == nil && bErr == nil && aTime.Before(bTime)
}
