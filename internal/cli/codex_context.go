package cli

import (
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
		result.Usage.Context = codexContext(stdout, uc)
	}
	return result, err
}

//nolint:gocritic // UsageContext is an immutable value shared by adapter capability interfaces.
func codexContext(stdout string, uc UsageContext) *model.ContextMeasurement {
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
		return unavailableCodexContext(model.UnavailableNoUsageEvent)
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
			return unavailableCodexContext(model.UnavailableCodexRolloutNotFound)
		}
		home = filepath.Join(dir, ".codex")
	}
	path := findCodexRollout(filepath.Join(home, "sessions"), session)
	if path == "" {
		return unavailableCodexContext(model.UnavailableCodexRolloutNotFound)
	}
	f, err := os.Open(path) // #nosec G304 -- located in the effective Codex sessions directory
	if err != nil {
		return unavailableCodexContext(model.UnavailableCodexRolloutNotFound)
	}
	defer func() { _ = f.Close() }()
	return readCodexContext(f, session, uc.StartedAt)
}

func readCodexContext(reader io.Reader, session string, startedAt time.Time) *model.ContextMeasurement {
	var records []codexContextResponse
	compacted := map[string]bool{}
	var window *int64
	nonzero := false
	line := 0
	scanner := newStreamScanner(reader)
	for scanner.Scan() {
		line++
		var event struct {
			Timestamp time.Time `json:"timestamp"`
			Type      string    `json:"type"`
			Payload   struct {
				Type       string `json:"type"`
				Thread     string `json:"thread_id"`
				Response   string `json:"response_id"`
				Compaction string `json:"compaction_response_id"`
				Usage      struct {
					Input *int64 `json:"input_tokens"`
				} `json:"usage"`
				Info *struct {
					Window *int64           `json:"model_context_window"`
					Last   map[string]int64 `json:"last_token_usage"`
					Total  map[string]int64 `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return unavailableCodexContext(model.UnavailableParseFailure)
		}
		if event.Timestamp.Before(startedAt) {
			continue
		}
		p := event.Payload
		switch event.Type {
		case "compacted":
			if p.Compaction != "" {
				compacted[p.Compaction] = true
			}
		case "token_usage_record":
			if p.Thread == session {
				records = append(records, codexContextResponse{p.Response, p.Usage.Input, line})
			}
		case "event_msg":
			if p.Type == "token_count" && p.Info != nil {
				if p.Info.Window != nil {
					window = p.Info.Window
				}
				nonzero = nonzero || codexNonzeroUsage(p.Info.Last) || codexNonzeroUsage(p.Info.Total)
			}
		}
	}
	if scanner.Err() != nil {
		return unavailableCodexContext(model.UnavailableParseFailure)
	}
	if len(records) == 0 && nonzero {
		c := unavailableCodexContext(model.UnavailableCodexRolloutUnsupported)
		c.ContextWindowLimit = window
		return c
	}
	return reduceCodexContext(records, compacted, line, window)
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
