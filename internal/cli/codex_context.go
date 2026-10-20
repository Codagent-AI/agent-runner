package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

//nolint:gocritic // UsageContext is an immutable value shared by adapter capability interfaces.
func (a *CodexAdapter) ExtractUsageWithContext(stdout string, uc UsageContext) (UsageExtraction, error) {
	result, err := a.ExtractUsage(stdout)
	if err == nil {
		result.Usage.Context, result.Usage.RequestEvidence = codexContext(stdout, uc)
	}
	return result, err
}

//nolint:gocritic // UsageContext is an immutable value shared by adapter capability interfaces.
func codexContext(stdout string, uc UsageContext) (*model.ContextMeasurement, *model.RequestEvidence) {
	session := ""
	scanner := newStreamScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		var event struct {
			Type   string `json:"type"`
			Thread string `json:"thread_id"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "thread.started" {
			session = event.Thread
		}
	}
	if session == "" {
		return unavailableCodexContext(model.UnavailableNoUsageEvent), unavailableCodexRequests(model.UnavailableCodexRolloutNotFound)
	}
	home := ""
	for _, entry := range uc.Env {
		if v, ok := strings.CutPrefix(entry, "CODEX_HOME="); ok {
			home = v
		}
	}
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return unavailableCodexContext(model.UnavailableCodexRolloutNotFound), unavailableCodexRequests(model.UnavailableCodexRolloutNotFound)
		}
		home = filepath.Join(dir, ".codex")
	}
	path := findCodexRollout(filepath.Join(home, "sessions"), session)
	if path == "" {
		return unavailableCodexContext(model.UnavailableCodexRolloutNotFound), unavailableCodexRequests(model.UnavailableCodexRolloutNotFound)
	}
	f, err := os.Open(path) // #nosec G304 -- located in the effective Codex sessions directory
	if err != nil {
		return unavailableCodexContext(model.UnavailableCodexRolloutNotFound), unavailableCodexRequests(model.UnavailableCodexRolloutNotFound)
	}
	defer func() { _ = f.Close() }()
	return readCodexRollout(f, session, uc.StartedAt)
}

func readCodexRollout(reader io.Reader, session string, startedAt time.Time) (*model.ContextMeasurement, *model.RequestEvidence) {
	acc := codexRolloutAccumulator{session: session, compacted: map[string]bool{}}
	line := 0
	// Rollout lines (compacted replacement history, large tool outputs) can be
	// arbitrarily large, so read with a bufio.Reader instead of a bounded Scanner.
	br := bufio.NewReader(reader)
	for {
		raw, readErr := br.ReadBytes('\n')
		if readErr != nil && readErr != io.EOF {
			return unavailableCodexContext(model.UnavailableParseFailure), unavailableCodexRequests(model.UnavailableParseFailure)
		}
		if len(raw) == 0 {
			break // a trailing newline at EOF is not an extra line
		}
		line++
		raw = bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r"))
		var event struct {
			Timestamp time.Time       `json:"timestamp"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return unavailableCodexContext(model.UnavailableParseFailure), unavailableCodexRequests(model.UnavailableParseFailure)
		}
		if !event.Timestamp.Before(startedAt) && !acc.apply(event.Type, event.Payload, line) {
			return unavailableCodexContext(model.UnavailableParseFailure), unavailableCodexRequests(model.UnavailableParseFailure)
		}
		if readErr == io.EOF {
			break
		}
	}
	if len(acc.records) == 0 && acc.nonzero {
		c := unavailableCodexContext(model.UnavailableCodexRolloutUnsupported)
		c.ContextWindowLimit = acc.window
		return c, acc.requestEvidence()
	}
	return reduceCodexContext(acc.records, acc.compacted, line, acc.window), acc.requestEvidence()
}

type codexRolloutAccumulator struct {
	session        string
	requests       []model.RequestUsage
	requestFailure bool
	records        []codexContextResponse
	compacted      map[string]bool
	window         *int64
	nonzero        bool
}

// apply folds one in-window record into the accumulator. It returns false when
// a record type that contributes to the measurement has an undecodable payload.
// Other record types are unrelated and their payloads are never decoded.
func (a *codexRolloutAccumulator) apply(eventType string, payload json.RawMessage, line int) bool {
	switch eventType {
	case "compacted":
		var p struct {
			Compaction string `json:"compaction_response_id"`
		}
		if !decodeCodexPayload(payload, &p) {
			return false
		}
		if p.Compaction != "" {
			a.compacted[p.Compaction] = true
		}
	case "token_usage_record":
		var p struct {
			Thread   string `json:"thread_id"`
			Response string `json:"response_id"`
			Usage    struct {
				Input      *int64 `json:"input_tokens"`
				Cached     *int64 `json:"cached_input_tokens"`
				Output     *int64 `json:"output_tokens"`
				CacheWrite *int64 `json:"cache_write_input_tokens"`
			} `json:"usage"`
		}
		if !decodeCodexPayload(payload, &p) {
			return false
		}
		if p.Thread == a.session {
			a.records = append(a.records, codexContextResponse{p.Response, p.Usage.Input, line})
			if p.Usage.Input == nil || p.Usage.Cached == nil || p.Usage.Output == nil {
				a.requestFailure = true
			} else {
				a.requests = append(a.requests, model.RequestUsage{Input: *p.Usage.Input, Cached: *p.Usage.Cached, Output: *p.Usage.Output, CacheWrite: p.Usage.CacheWrite})
			}
		}
	case "event_msg":
		// Only token_count payloads are decoded in full; any other event_msg is
		// unrelated and must not discard a measurement.
		var kind struct {
			Type string `json:"type"`
		}
		if !decodeCodexPayload(payload, &kind) || kind.Type != "token_count" {
			return true
		}
		var p struct {
			Info *struct {
				Window *int64           `json:"model_context_window"`
				Last   map[string]int64 `json:"last_token_usage"`
				Total  map[string]int64 `json:"total_token_usage"`
			} `json:"info"`
		}
		if !decodeCodexPayload(payload, &p) {
			return false
		}
		if p.Info != nil {
			if p.Info.Window != nil {
				a.window = p.Info.Window
			}
			a.nonzero = a.nonzero || codexNonzeroUsage(p.Info.Last) || codexNonzeroUsage(p.Info.Total)
		}
	}
	return true
}

// decodeCodexPayload decodes a record payload into v. An absent payload leaves
// v at its zero value, matching how a missing field behaved before.
func decodeCodexPayload(raw json.RawMessage, v any) bool {
	if len(raw) == 0 {
		return true
	}
	return json.Unmarshal(raw, v) == nil
}

type codexContextResponse struct {
	id    string
	input *int64
	line  int
}

func reduceCodexContext(records []codexContextResponse, compacted map[string]bool, line int, window *int64) *model.ContextMeasurement {
	obs := make([]contextObservation, 0, len(records))
	for _, r := range records {
		if compacted[r.id] {
			continue
		}
		o := contextObservation{unclassified: r.line == line || r.input == nil || r.id == ""}
		if r.input != nil {
			o.tokens = *r.input
		}
		obs = append(obs, o)
	}
	return buildContext("codex:rollout", obs, window, false)
}

// Walk date directories newest first, stopping as soon as the session is found.
// ReadDir follows the sessions symlink installed in Runner's private Codex home.
func findCodexRollout(dir, session string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if found := findCodexRollout(path, session); found != "" {
				return found
			}
		} else if strings.HasPrefix(e.Name(), "rollout-") && strings.HasSuffix(e.Name(), "-"+session+".jsonl") {
			return path
		}
	}
	return ""
}

func codexNonzeroUsage(counts map[string]int64) bool {
	for _, n := range counts {
		if n != 0 {
			return true
		}
	}
	return false
}

func unavailableCodexContext(reason model.UnavailableReason) *model.ContextMeasurement {
	return &model.ContextMeasurement{Status: model.ContextUnavailable, Reason: reason, Source: "codex:rollout"}
}

func unavailableCodexRequests(reason model.UnavailableReason) *model.RequestEvidence {
	return &model.RequestEvidence{Source: "codex:rollout", Status: model.UsageUnavailable, Reason: reason}
}
func (a *codexRolloutAccumulator) requestEvidence() *model.RequestEvidence {
	if a.requestFailure {
		return unavailableCodexRequests(model.UnavailableParseFailure)
	}
	if len(a.requests) == 0 {
		return unavailableCodexRequests(model.UnavailableCodexRolloutUnsupported)
	}
	return &model.RequestEvidence{Source: "codex:rollout", Status: model.UsageCollected, Requests: a.requests}
}
