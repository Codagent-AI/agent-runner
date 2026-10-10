package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

func (a *ClaudeAdapter) PrepareInteractiveUsage(sessionID string, _ bool, uc UsageContext) (InteractiveUsagePlan, error) {
	p := InteractiveUsagePlan{SessionID: sessionID, Context: uc, ReportReason: model.UnavailableCostReportUnavailable}
	root, parent, _, reason := claudeTranscriptPaths(sessionID, uc)
	if reason == model.UnavailableTranscriptAmbiguous {
		p.PrepareErr = reason
	}
	if root != "" {
		p.TranscriptPath = filepath.Join(root, parent)
		cp, err := fileCheckpoint(p.TranscriptPath)
		if err != nil {
			p.PrepareErr = model.UnavailableTranscriptSpanUnavailable
		} else {
			p.StartOffset = cp.Offset
		}
	}
	line, err := resolveClaudeStatusLine(uc)
	if err != nil {
		p.ReportError = err.Error()
		return p, nil
	}
	if uc.StateDir == "" {
		p.ReportError = "interactive usage state directory is empty"
		return p, nil
	}
	dir := filepath.Join(uc.StateDir, "usage")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		p.ReportError = err.Error()
		return p, nil
	}
	f, err := createClaudeReport(dir, uc.ReportName)
	if err != nil {
		p.ReportError = err.Error()
		return p, nil
	}
	p.ReportPath = f.Name()
	if err = f.Chmod(0o600); err != nil {
		p.ReportError = err.Error()
		_ = f.Close()
		return p, nil
	}
	if err = f.Close(); err != nil {
		p.ReportError = err.Error()
		return p, nil
	}
	p.ReportEnabled = true
	p.ReportReason = ""
	p.StatusLine = line
	return p, nil
}

// createClaudeReport uses exclusive creation so a resumed process cannot reuse
// another process's startup baseline, even after a pre-spawn failure.
func createClaudeReport(dir string, name *string) (*os.File, error) {
	if name == nil || *name == "" {
		return os.CreateTemp(dir, "interactive-*.statusline.jsonl")
	}
	if filepath.Base(*name) != *name || strings.Contains(*name, `\`) {
		return nil, fmt.Errorf("invalid interactive usage report name")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(*name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return os.CreateTemp(dir, "interactive-*.statusline.jsonl")
	}
	return f, err
}

type claudeSpanMessage struct {
	model           string
	tokens          model.TokenCounts
	complete        bool
	position        int64
	timestamp       time.Time
	latestTimestamp time.Time
}

type claudeInteractiveSpan struct {
	messages      map[string]claudeSpanMessage
	spawns        map[string]bool
	failed        map[string]bool
	settled       map[string]claudeSettlement
	reason        model.UnavailableReason
	firstPrompt   string
	firstUserSeen bool
	final         string
}

type claudeSettlement struct {
	position  int64
	timestamp time.Time
}

func readClaudeInteractiveSpan(root *os.Root, path string, offset int64, main bool) claudeInteractiveSpan {
	s := claudeInteractiveSpan{messages: map[string]claudeSpanMessage{}, spawns: map[string]bool{}, failed: map[string]bool{}, settled: map[string]claudeSettlement{}}
	if main {
		var reason model.UnavailableReason
		s.spawns, s.failed, reason = scanClaudeParentSpan(root, path, claudeParentSpanSelector{startOffset: &offset, onEntry: s.consumeMetadata})
		if reason != "" {
			s.reason = reason
		}
		return s
	}
	f, err := root.Open(path)
	if err != nil {
		s.reason = model.UnavailableTranscriptMissing
		return s
	}
	defer func() { _ = f.Close() }()
	reader := claudeJSONLReader{file: f, offset: offset}
	if err = reader.read(context.Background(), true, func(line []byte, position int64) error {
		s.consumeLine(line, position, main)
		return nil
	}); err != nil {
		s.reason = model.UnavailableTranscriptSpanUnavailable
	}

	return s
}

func (s *claudeInteractiveSpan) consumeLine(line []byte, position int64, main bool) {
	e, err := parseClaudeEntry(line)
	if err != nil {
		s.reason = model.UnavailableTranscriptInvalid
		return
	}
	if main && (e.IsSidechain || e.ParentToolUseID != "") {
		return
	}
	s.consume(&e, position)
}

func interactiveMainAllocations(s *claudeInteractiveSpan) []model.UsageAllocation {
	byModel := map[string]*model.UsageAllocation{}
	for _, m := range s.messages {
		if m.tokens == nil || m.model == "" {
			continue
		}
		a := byModel[m.model]
		if a == nil {
			a = &model.UsageAllocation{ID: "main:" + m.model, Kind: "main", Model: m.model, Status: model.UsageCollected, Completeness: model.CompletenessComplete, Tokens: model.TokenCounts{}}
			byModel[m.model] = a
		}
		for k, v := range m.tokens {
			a.Tokens[k] += v
		}
	}
	keys := make([]string, 0, len(byModel))
	for k := range byModel {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	allocations := make([]model.UsageAllocation, 0, len(keys))
	for _, k := range keys {
		a := byModel[k]
		a.TokenTotals = claudeTokenTotals(a.Tokens)
		if s.reason != "" {
			a.Completeness = model.CompletenessPartial
			a.Reason = s.reason
		}
		allocations = append(allocations, *a)
	}
	if len(allocations) == 0 {
		reason := s.reason
		if reason == "" {
			reason = model.UnavailableNoUsageEvent
		}
		allocations = append(allocations, model.UsageAllocation{ID: "main", Kind: "main", Status: model.UsageUnavailable, Reason: reason})
	}
	return allocations
}

func resolveInteractiveSpan(p *InteractiveUsagePlan, uc UsageContext) (store *os.Root, parentPath, subagentsPath string, result claudeInteractiveSpan) {
	empty := claudeInteractiveSpan{reason: p.PrepareErr}
	if p.PrepareErr != "" {
		return nil, "", "", empty
	}
	root, parent, subs, reason := claudeTranscriptPaths(p.SessionID, uc)
	if root == "" {
		if reason != model.UnavailableTranscriptAmbiguous {
			reason = model.UnavailableTranscriptMissing
		}
		empty.reason = reason
		return nil, "", "", empty
	}
	if p.TranscriptPath != "" && p.TranscriptPath != filepath.Join(root, parent) {
		empty.reason = model.UnavailableTranscriptSpanUnavailable
		return nil, "", "", empty
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		empty.reason = model.UnavailableTranscriptMissing
		return nil, "", "", empty
	}
	span := readClaudeInteractiveSpan(r, parent, p.StartOffset, true)
	return r, parent, subs, span
}

//nolint:gocritic // The optional collector capability passes its immutable invocation plan by value.
func (a *ClaudeAdapter) ExtractInteractiveUsage(p InteractiveUsagePlan, uc UsageContext) UsageExtraction {
	root, _, subs, s := resolveInteractiveSpan(&p, uc)
	if root != nil {
		defer func() { _ = root.Close() }()
	}
	reports := readClaudeReports(p.ReportPath)
	for _, r := range reports {
		if r.SessionID != p.SessionID {
			s.reason = model.UnavailableSessionSwitched
		}
	}
	u := model.UsageRecord{CLI: "claude", Provider: "anthropic", Source: "claude:session-transcript", Status: model.UsageUnavailable, Reason: s.reason, Completeness: model.CompletenessComplete}
	u.Allocations = interactiveMainAllocations(&s)
	if u.Reason == "" {
		u.Reason = u.Allocations[0].Reason
	}
	if s.reason != "" {
		u.Completeness = model.CompletenessPartial
	}
	if len(u.Allocations) == 1 {
		u.Model = u.Allocations[0].Model
	}
	var reason claudeCollectionReason
	switch s.reason {
	case model.UnavailableTranscriptMissing, model.UnavailableTranscriptSpanUnavailable:
		reason.note(model.UnavailableSubagentSpanUnavailable)
	case model.UnavailableTranscriptAmbiguous:
		reason.note(model.UnavailableTranscriptAmbiguous)
	case model.UnavailableTranscriptInvalid:
		reason.note(model.UnavailableSubagentParentInvalid)
	}
	running, settled := interactiveClaudeLifecycle(root, subs, &s)
	u.Allocations = append(u.Allocations, collectClaudeSubagents(root, subs, s.spawns, s.failed, running, &reason)...)
	u.SubagentCollection = model.CompletenessComplete
	if reason.reason != "" {
		u.SubagentCollection = model.CompletenessPartial
		u.SubagentCollectionReason = reason.reason
	}
	sumClaudeAllocations(&u)
	if len(u.Allocations) == 1 {
		u.Model = u.Allocations[0].Model
	}
	cost, costReason := interactiveClaudeCost(&p, &s, reports, settled)
	return UsageExtraction{Usage: u, EstimatedCostUSD: cost, CostUnavailableReason: costReason, CostReportError: p.ReportError}
}

// WaitForFinalReport observes causal usage evidence, never file quietness.
//
//nolint:gocritic // The optional collector capability passes its immutable invocation plan by value.
func (a *ClaudeAdapter) WaitForFinalReport(ctx context.Context, p InteractiveUsagePlan) {
	if !p.ReportEnabled {
		return
	}
	if ctx.Err() != nil || p.PrepareErr != "" {
		return
	}
	reportFile, err := openClaudeScopedFile(p.ReportPath)
	if err != nil {
		return
	}
	defer func() { _ = reportFile.Close() }()
	reportReader := claudeJSONLReader{file: reportFile}
	var transcript *claudeJSONLReader
	defer func() {
		if transcript != nil {
			_ = transcript.file.Close()
		}
	}()
	span := claudeInteractiveSpan{messages: map[string]claudeSpanMessage{}, spawns: map[string]bool{}, failed: map[string]bool{}, settled: map[string]claudeSettlement{}}
	reports := claudeReportIndex{session: p.SessionID, latest: map[[4]int64]time.Time{}}
	ticker := time.NewTicker(durabilityPollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if transcript == nil {
			transcript, err = openInteractiveClaudeTranscript(&p)
			if err != nil {
				return
			}
		}
		if transcript != nil && transcript.read(ctx, false, func(line []byte, position int64) error {
			span.consumeLine(line, position, true)
			return nil
		}) != nil {
			return
		}
		if err = reportReader.read(ctx, false, reports.consume); err != nil {
			return
		}
		if m, ok := span.messages[span.final]; ok && reports.matches(&m) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func openInteractiveClaudeTranscript(p *InteractiveUsagePlan) (*claudeJSONLReader, error) {
	root, parent, _, reason := claudeTranscriptPaths(p.SessionID, p.Context)
	if reason == model.UnavailableTranscriptAmbiguous {
		return nil, errors.New("ambiguous Claude transcript")
	}
	if root == "" {
		return nil, nil
	}
	path := filepath.Join(root, parent)
	if p.TranscriptPath != "" && p.TranscriptPath != path {
		return nil, errors.New("claude transcript path changed")
	}
	file, err := openClaudeScopedFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &claudeJSONLReader{file: file, offset: p.StartOffset}, nil
}

// Only the newest matching tuple matters for readiness. Keep session identity
// outside the key because each index belongs to one invocation.
type claudeReportIndex struct {
	session string
	latest  map[[4]int64]time.Time
}

func (reports *claudeReportIndex) consume(line []byte, _ int64) error {
	var report ClaudeStatusReport
	if err := json.Unmarshal(line, &report); err != nil {
		return nil
	}
	if report.SessionID != reports.session {
		return nil
	}
	tokens, complete, err := tokenCountsFromObject(report.CurrentUsage, claudeTokenFields)
	if err == nil && complete {
		key := claudeUsageKey(tokens)
		if report.RecordedAt.After(reports.latest[key]) {
			reports.latest[key] = report.RecordedAt
		}
	}
	return nil
}

func (reports *claudeReportIndex) matches(m *claudeSpanMessage) bool {
	return m.reportIsFreshAt(reports.latest[claudeUsageKey(m.tokens)])
}

func claudeUsageKey(tokens model.TokenCounts) [4]int64 {
	return [4]int64{tokens[model.TokenInput], tokens[model.TokenCachedInput], tokens[model.TokenCacheWrite], tokens[model.TokenOutput]}
}

type ClaudeStatusReport struct {
	RecordedAt   time.Time       `json:"recorded_at"`
	SessionID    string          `json:"session_id"`
	PromptID     string          `json:"prompt_id"`
	TotalCostUSD *float64        `json:"total_cost_usd"`
	CurrentUsage json.RawMessage `json:"current_usage"`
}

func readClaudeReports(path string) []ClaudeStatusReport {
	f, err := openClaudeScopedFile(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var reports []ClaudeStatusReport
	scanner := newStreamScanner(f)
	for scanner.Scan() {
		var r ClaudeStatusReport
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			return nil
		}
		reports = append(reports, r)
	}
	if scanner.Err() != nil {
		return nil
	}
	return reports
}

func reportMatches(r *ClaudeStatusReport, m *claudeSpanMessage) bool {
	tokens, complete, err := tokenCountsFromObject(r.CurrentUsage, claudeTokenFields)
	return err == nil && complete && m.reportIsFreshAt(r.RecordedAt) && claudeUsageKey(tokens) == claudeUsageKey(m.tokens)
}

func (m *claudeSpanMessage) reportIsFreshAt(recorded time.Time) bool {
	return m.complete && !m.timestamp.IsZero() && !recorded.IsZero() && !recorded.Before(m.timestamp)
}

func interactiveClaudeCost(p *InteractiveUsagePlan, s *claudeInteractiveSpan, reports []ClaudeStatusReport, settled bool) (*float64, model.UnavailableReason) {
	if !p.ReportEnabled || len(reports) == 0 {
		return nil, model.UnavailableCostReportUnavailable
	}
	final, ok := s.messages[s.final]
	var end *ClaudeStatusReport
	if ok {
		for i := range reports {
			if reportMatches(&reports[i], &final) {
				end = &reports[i]
			}
		}
	}
	if end == nil || end.TotalCostUSD == nil {
		return nil, model.UnavailableCostReportStale
	}
	base := reports[0]
	firstPrompt := ""
	for _, r := range reports {
		if r.PromptID != "" {
			firstPrompt = r.PromptID
			break
		}
	}
	if base.TotalCostUSD == nil || base.PromptID != "" || firstPrompt == "" || s.firstPrompt == "" || firstPrompt != s.firstPrompt {
		return nil, model.UnavailableCostBaselineMissing
	}
	if !settled {
		return nil, model.UnavailableCostSubagentUnsettled
	}
	for _, r := range reports {
		if r.SessionID != p.SessionID {
			return nil, model.UnavailableCostSessionMismatch
		}
	}
	delta := *end.TotalCostUSD - *base.TotalCostUSD
	if delta < 0 {
		return nil, model.UnavailableCounterReset
	}
	return &delta, ""
}

func (s *claudeInteractiveSpan) consume(e *claudeEntry, position int64) {
	ids, failed, bad := claudeTools(e)
	if bad {
		s.reason = model.UnavailableTranscriptInvalid
	}
	for _, id := range ids {
		s.spawns[id] = true
	}
	for id := range failed {
		s.failed[id] = true
	}
	s.consumeMetadata(e, position)
}

func (s *claudeInteractiveSpan) consumeMetadata(e *claudeEntry, position int64) {
	if e.Type == "user" && !s.firstUserSeen {
		s.firstPrompt = e.PromptID
		s.firstUserSeen = true
	}
	for _, id := range claudeCompletedTools(e) {
		s.settled[id] = claudeSettlement{position, e.Timestamp}
	}

	s.consumeMessage(e, position)
}

func (s *claudeInteractiveSpan) consumeMessage(e *claudeEntry, position int64) {
	if e.Type != "assistant" || e.Message.Model == "<synthetic>" {
		return
	}
	id := e.Message.ID
	if id == "" {
		s.reason = model.UnavailableTranscriptInvalid
		return
	}
	m := claudeSpanMessage{model: e.Message.Model, position: position, timestamp: e.Timestamp, latestTimestamp: e.Timestamp}
	prior, hadPrior := s.messages[id]
	if hadPrior {
		m.position = prior.position
		m.timestamp = prior.timestamp
	}
	counts, complete, err := tokenCountsFromObject(e.Message.Usage, claudeTokenFields)
	switch {
	case len(e.Message.Usage) == 0 || string(e.Message.Usage) == "null":
		s.reason = model.UnavailableNoUsageEvent
	case err != nil:
		s.reason = model.UnavailableTranscriptInvalid
	default:
		m.tokens = counts
		m.complete = complete
		if !complete {
			s.reason = model.UnavailableNoUsageEvent
		}
	}
	if m.tokens == nil && hadPrior {
		m.tokens = prior.tokens
		m.complete = false
	}
	if m.model == "" {
		s.reason = model.UnavailableTranscriptInvalid
		m.model = prior.model
		m.complete = false
	}
	s.messages[id] = m
	s.final = id
}
