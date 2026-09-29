package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
)

// nestedRateLimitEndTolerance absorbs producer/Codex flush ordering at attempt end.
const nestedRateLimitEndTolerance = 2 * time.Second

type RateLimitEnrichment struct {
	HeadKey   string                       `json:"head_key"`
	AttemptID string                       `json:"attempt_id"`
	StartedAt string                       `json:"started_at"`
	EndedAt   string                       `json:"ended_at"`
	Evidence  model.CodexRateLimitEvidence `json:"evidence"`
}

// matches reports whether the enrichment was captured against these head bounds.
func (e *RateLimitEnrichment) matches(p *measurements.Payload) bool {
	return p.Lifecycle.StartedAt != nil && p.Lifecycle.EndedAt != nil &&
		e.AttemptID == p.AttemptID && e.StartedAt == *p.Lifecycle.StartedAt && e.EndedAt == *p.Lifecycle.EndedAt
}

func (c *Collector) SetCodexRateLimitReader(reader model.CodexRateLimitReadFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rateLimitReader = reader
}

func isCodexMeasurement(record *measurements.Record) bool {
	p := &record.Payload
	isCodex := func(adapter *string) bool { return adapter != nil && *adapter == "codex" }
	return p.Adapter == "codex" || isCodex(p.Requested.Adapter) || isCodex(p.Resolved.Adapter)
}

// captureRateLimitEnrichmentsLocked recaptures evidence for heads inserted or
// replaced by the current import; unchanged heads keep their enrichment.
func (c *Collector) captureRateLimitEnrichmentsLocked(changed map[string]struct{}) {
	if c.rateLimitReader == nil || len(changed) == 0 {
		return
	}
	index := c.rateLimitEnrichmentIndexLocked()
	for i := range c.artifact.MeasurementHeads {
		head := &c.artifact.MeasurementHeads[i]
		if _, ok := changed[head.Key]; !ok {
			continue
		}
		var record measurements.Record
		if json.Unmarshal(head.Record, &record) != nil || record.Type != "model_attempt" || !isCodexMeasurement(&record) {
			continue
		}
		p := &record.Payload
		if p.Lifecycle.StartedAt == nil || p.Lifecycle.EndedAt == nil {
			continue
		}
		start, e1 := time.Parse(time.RFC3339Nano, *p.Lifecycle.StartedAt)
		end, e2 := time.Parse(time.RFC3339Nano, *p.Lifecycle.EndedAt)
		if e1 != nil || e2 != nil {
			continue
		}
		existing := index[head.Key]
		if existing != nil && existing.matches(p) {
			continue
		}
		enrichment := RateLimitEnrichment{
			HeadKey: head.Key, AttemptID: p.AttemptID, StartedAt: *p.Lifecycle.StartedAt, EndedAt: *p.Lifecycle.EndedAt,
			Evidence: c.rateLimitReader(model.CodexRateLimitRequest{
				ThreadID: providerSessionID(head.Record), RunID: c.artifact.RunID,
				StartedAt: start, EndedAt: end, EndTolerance: nestedRateLimitEndTolerance,
			}),
		}
		if existing != nil {
			*existing = enrichment
		} else {
			c.artifact.RateLimitEnrichments = append(c.artifact.RateLimitEnrichments, enrichment)
			index = c.rateLimitEnrichmentIndexLocked()
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

// rateLimitEnrichmentIndexLocked maps head keys to enrichments. Pointers stay
// valid until RateLimitEnrichments is next appended to.
func (c *Collector) rateLimitEnrichmentIndexLocked() map[string]*RateLimitEnrichment {
	index := make(map[string]*RateLimitEnrichment, len(c.artifact.RateLimitEnrichments))
	for i := range c.artifact.RateLimitEnrichments {
		index[c.artifact.RateLimitEnrichments[i].HeadKey] = &c.artifact.RateLimitEnrichments[i]
	}
	return index
}

func enrichmentForHead(head *MeasurementHead, record *measurements.Record, index map[string]*RateLimitEnrichment) *model.CodexRateLimitEvidence {
	if head.Status == "conflicting" {
		return unavailableNestedEvidence(model.RateLimitReasonExcludedMeasurement)
	}
	e := index[head.Key]
	if e == nil {
		return unavailableNestedEvidence(model.RateLimitReasonSessionUnidentified)
	}
	if !e.matches(&record.Payload) {
		return unavailableNestedEvidence(model.RateLimitReasonStaleEnrichment)
	}
	value := e.Evidence
	return &value
}

func unavailableNestedEvidence(reason string) *model.CodexRateLimitEvidence {
	return &model.CodexRateLimitEvidence{
		Status: model.RateLimitStatusUnavailable, Reason: reason, Source: model.RateLimitSourceCodexSessionLog,
		AccountScope: model.RateLimitAccountUnverified, Deltas: model.UnavailableRateLimitDeltas(reason),
	}
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

type windowGroupKey struct {
	accountScope, limitID, role string
	windowMinutes, resetsAt     int64
}

type observedSnapshot struct {
	at       time.Time
	snapshot *model.RateLimitSnapshot
}

type windowAccumulator struct {
	sortKey   string
	group     RateLimitWindowGroup
	start     *observedSnapshot
	end       *observedSnapshot
	intervals [][2]time.Time
}

//nolint:gocognit,cyclop,funlen // Grouping, coverage, and span are one deterministic projection.
func rollupCodexRateLimits(steps []StepRecord) *CodexRateLimitRollup {
	rollup := &CodexRateLimitRollup{Windows: []RateLimitWindowGroup{}}
	for _, role := range model.RateLimitWindowRoles {
		rollup.Coverage = append(rollup.Coverage, RateLimitRoleCoverage{Window: role})
	}
	groups := map[windowGroupKey]*windowAccumulator{}
	for i := range steps {
		e := steps[i].CodexRateLimits
		if e == nil {
			continue
		}
		rollup.MeasuredAttempts++
		var start, end *observedSnapshot
		if e.Start != nil {
			if at, ok := e.Start.ObservedTime(); ok {
				start = &observedSnapshot{at: at, snapshot: e.Start}
			}
		}
		if e.End != nil {
			if at, ok := e.End.ObservedTime(); ok {
				end = &observedSnapshot{at: at, snapshot: e.End}
			}
		}
		attemptStart, err1 := time.Parse(time.RFC3339Nano, e.AttemptStartedAt)
		attemptEnd, err2 := time.Parse(time.RFC3339Nano, e.AttemptEndedAt)
		for roleIndex, role := range model.RateLimitWindowRoles {
			delta := deltaForRole(e.Deltas, role)
			available := delta != nil && delta.Availability == model.RateLimitAvailable && delta.PercentagePoints != nil
			coverage := &rollup.Coverage[roleIndex]
			coverage.Measured++
			if delta != nil && delta.Availability == model.RateLimitAvailable {
				coverage.WithDelta++
			}
			if e.End == nil {
				continue
			}
			endWindow := e.End.Window(role)
			if !endWindow.Reported || endWindow.WindowMinutes == nil || endWindow.ResetsAt == nil {
				continue
			}
			key := windowGroupKey{accountScope: e.AccountScope, role: role, windowMinutes: *endWindow.WindowMinutes, resetsAt: *endWindow.ResetsAt}
			if e.End.LimitID != nil {
				key.limitID = *e.End.LimitID
			}
			acc := groups[key]
			if acc == nil {
				acc = &windowAccumulator{
					sortKey: fmt.Sprintf("%q/%q/%s/%d/%d", key.accountScope, key.limitID, role, key.windowMinutes, key.resetsAt),
					group: RateLimitWindowGroup{
						AccountScope: e.AccountScope, LimitID: e.End.LimitID, Window: role, WindowMinutes: key.windowMinutes, ResetsAt: key.resetsAt,
						DeltaSum: model.UnavailableRateLimitDelta(role, model.RateLimitReasonNoBaseline),
						Span:     model.UnavailableRateLimitDelta(role, model.RateLimitReasonNoBaseline),
					},
				}
				groups[key] = acc
			}
			if available {
				acc.group.Contributing++
				if e.AccountScope != model.RateLimitAccountUnverified {
					if acc.group.DeltaSum.PercentagePoints == nil {
						acc.group.DeltaSum = model.AvailableRateLimitDelta(role, 0)
					}
					*acc.group.DeltaSum.PercentagePoints += *delta.PercentagePoints
				}
			}
			if start != nil && e.Start.Window(role).Reported && e.Start.SameWindow(e.End, role) && (acc.start == nil || start.at.Before(acc.start.at)) {
				acc.start = start
			}
			if end != nil && (acc.end == nil || acc.end.at.Before(end.at)) {
				acc.end = end
			}
			if err1 == nil && err2 == nil {
				acc.intervals = append(acc.intervals, [2]time.Time{attemptStart, attemptEnd})
			}
		}
	}
	if rollup.MeasuredAttempts == 0 {
		return nil
	}
	for i := range rollup.Coverage {
		c := &rollup.Coverage[i]
		switch c.WithDelta {
		case 0:
			c.Coverage = "none"
		case c.Measured:
			c.Coverage = "complete"
		default:
			c.Coverage = "partial"
		}
	}
	ordered := make([]*windowAccumulator, 0, len(groups))
	for _, acc := range groups {
		ordered = append(ordered, acc)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].sortKey < ordered[j].sortKey })
	for _, acc := range ordered {
		role := acc.group.Window
		switch {
		case acc.group.AccountScope == model.RateLimitAccountUnverified:
			acc.group.DeltaSum = model.UnavailableRateLimitDelta(role, model.RateLimitReasonAccountUnverified)
			acc.group.Span = model.UnavailableRateLimitDelta(role, model.RateLimitReasonAccountUnverified)
		case acc.start != nil && acc.end != nil:
			acc.group.Span = rollupSpan(acc.start.snapshot, acc.end.snapshot, role)
		}
		if intervalsOverlap(acc.intervals) {
			acc.group.Limitations = []string{model.RateLimitLimitationOverlapping}
			acc.group.DeltaSum = model.UnavailableRateLimitDelta(role, model.RateLimitReasonOverlappingAttempts)
			acc.group.DeltaSum.Limitations = []string{model.RateLimitLimitationOverlapping}
		}
		rollup.Windows = append(rollup.Windows, acc.group)
	}
	return rollup
}

// intervalsOverlap reports whether any attempt starts before an earlier one ends.
func intervalsOverlap(intervals [][2]time.Time) bool {
	if len(intervals) < 2 {
		return false
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i][0].Before(intervals[j][0]) })
	latestEnd := intervals[0][1]
	for _, interval := range intervals[1:] {
		if interval[0].Before(latestEnd) {
			return true
		}
		if interval[1].After(latestEnd) {
			latestEnd = interval[1]
		}
	}
	return false
}

func deltaForRole(deltas []model.RateLimitDelta, role string) *model.RateLimitDelta {
	for i := range deltas {
		if deltas[i].Window == role {
			return &deltas[i]
		}
	}
	return nil
}

func rollupSpan(start, end *model.RateLimitSnapshot, role string) model.RateLimitDelta {
	startPercent, endPercent := start.Window(role).UsedPercent, end.Window(role).UsedPercent
	switch {
	case startPercent == nil || endPercent == nil:
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonNoBaseline)
	case *endPercent < *startPercent:
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonInconsistent)
	}
	return model.AvailableRateLimitDelta(role, *endPercent-*startPercent)
}
