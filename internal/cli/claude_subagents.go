package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
)

var claudeTokenFields = map[string]string{
	"input_tokens": model.TokenInput, "cache_read_input_tokens": model.TokenCachedInput,
	"cache_creation_input_tokens": model.TokenCacheWrite, "output_tokens": model.TokenOutput,
}

type claudeEntry struct {
	UUID            string `json:"uuid"`
	Type            string `json:"type"`
	SessionID       string `json:"session_id"`
	ParentToolUseID string `json:"parent_tool_use_id"`
	ToolUseID       string `json:"tool_use_id"`
	Status          string `json:"status"`
	Subtype         string `json:"subtype"`
	Message         struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   json.RawMessage `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Content     json.RawMessage `json:"content"`
	IsError     bool            `json:"is_error"`
	IsSidechain bool            `json:"isSidechain"`
}
type claudeTool struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}
type claudeSidecar struct {
	AgentType  string `json:"agentType"`
	ToolUseID  string `json:"toolUseId"`
	SpawnDepth int    `json:"spawnDepth"`
}
type claudeSpawn struct {
	id, parent string
	depth      int
}

func parseClaudeEntry(line []byte) (claudeEntry, error) {
	var e claudeEntry
	err := json.Unmarshal(line, &e)
	return e, err
}
func claudeTools(e *claudeEntry) (ids []string, failed map[string]bool, invalid bool) {
	if e.Type != "assistant" && e.Type != "user" {
		return nil, nil, false
	}
	var raw json.RawMessage
	if len(e.Message.Content) > 0 {
		raw = e.Message.Content
	} else {
		raw = e.Content
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, false
	}
	var blocks []claudeTool
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, nil, e.Type == "assistant"
	}
	ids = []string{}
	failed = map[string]bool{}
	for _, b := range blocks {
		if b.Type == "tool_use" {
			if b.ID == "" || b.Name == "" {
				return ids, failed, true
			}
			if b.Name == "Agent" || b.Name == "Task" {
				ids = append(ids, b.ID)
			}
		}
		if b.Type == "tool_result" && b.ToolUseID != "" && b.IsError {
			failed[b.ToolUseID] = true
		}
	}
	return ids, failed, false
}

// ExtractUsageWithContext combines Claude's invocation result with transcript evidence.
// Transcript failures reduce completeness but never fail the invocation.
//
//nolint:gocognit,cyclop,funlen // Transcript reconciliation has ordered partial-failure paths.
func (a *ClaudeAdapter) ExtractUsageWithContext(stdout string, uc UsageContext) (UsageExtraction, error) {
	result, err := a.ExtractUsage(stdout)
	if err != nil {
		return result, err
	}
	uuids := map[string]bool{}
	spawns := map[string]claudeSpawn{}
	running := map[string]bool{}
	session := ""
	scanner := newStreamScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		e, parseErr := parseClaudeEntry(scanner.Bytes())
		if parseErr != nil {
			continue
		}
		if e.SessionID != "" {
			session = e.SessionID
		}
		if e.UUID != "" {
			uuids[e.UUID] = true
		}
		if e.ParentToolUseID == "" {
			ids, _, _ := claudeTools(&e)
			for _, id := range ids {
				spawns[id] = claudeSpawn{id: id, depth: 1}
			}
			if e.ToolUseID != "" {
				if e.Subtype == "task_started" {
					running[e.ToolUseID] = true
					spawns[e.ToolUseID] = claudeSpawn{id: e.ToolUseID, depth: 1}
				}
				if e.Subtype == "task_notification" || (e.Subtype == "task_updated" && (e.Status == "completed" || e.Status == "failed")) {
					running[e.ToolUseID] = false
					spawns[e.ToolUseID] = claudeSpawn{id: e.ToolUseID, depth: 1}
				}
			}
		}
	}
	if session == "" {
		return result, nil
	}
	u := &result.Usage
	main := model.UsageAllocation{ID: "main", Kind: "main", Status: u.Status, Reason: u.Reason, Model: u.Model, Tokens: u.Tokens, TokenTotals: u.TokenTotals, Completeness: u.Completeness}
	u.Allocations = []model.UsageAllocation{main}
	if main.Status == model.UsageUnavailable {
		u.Completeness = model.CompletenessPartial
	}
	reason := model.UnavailableReason("")
	parent, subdir, pathErr := claudeTranscriptPaths(session, uc)
	if pathErr != "" {
		reason = pathErr
	}
	failed := map[string]bool{}
	if parent != "" {
		lines, readErr := os.ReadFile(parent)
		if readErr != nil {
			reason = model.UnavailableSubagentSpanUnavailable
		} else {
			type row struct {
				e     claudeEntry
				valid bool
			}
			rows := []row{}
			first, last := -1, -1
			for _, line := range strings.Split(strings.TrimSuffix(string(lines), "\n"), "\n") {
				e, parseErr := parseClaudeEntry([]byte(line))
				rows = append(rows, row{e, parseErr == nil})
				if parseErr == nil && e.UUID != "" && uuids[e.UUID] {
					if first < 0 {
						first = len(rows) - 1
					}
					last = len(rows) - 1
				}
			}
			if first < 0 {
				reason = model.UnavailableSubagentSpanUnavailable
			} else {
				end := last + 1
				if end >= len(rows) {
					end = last
				}
				for i := first; i <= end; i++ {
					row := rows[i]
					if !row.valid {
						if reason == "" {
							reason = model.UnavailableSubagentParentInvalid
						}
						continue
					}
					if i > last {
						continue
					}
					if row.e.ParentToolUseID != "" || row.e.IsSidechain {
						continue
					}
					ids, errs, invalid := claudeTools(&row.e)
					if row.e.Type == "assistant" && len(row.e.Message.Content) == 0 {
						invalid = true
					}
					if invalid && reason == "" {
						reason = model.UnavailableSubagentParentInvalid
					}
					for _, id := range ids {
						spawns[id] = claudeSpawn{id: id, depth: 1}
					}
					for id := range errs {
						failed[id] = true
					}
				}
			}
		}
	} else if reason == "" {
		reason = model.UnavailableSubagentSpanUnavailable
	}
	if subdir != "" {
		sidecars, _ := filepath.Glob(filepath.Join(subdir, "agent-*.meta.json"))
		byID := map[string]string{}
		metadata := map[string]claudeSidecar{}
		for _, path := range sidecars {
			var m claudeSidecar
			raw, err := os.ReadFile(path)
			if err == nil && json.Unmarshal(raw, &m) == nil && m.ToolUseID != "" {
				byID[m.ToolUseID] = strings.TrimSuffix(path, ".meta.json") + ".jsonl"
				metadata[m.ToolUseID] = m
			}
		}
		queue := make([]claudeSpawn, 0, len(spawns))
		for _, s := range spawns {
			queue = append(queue, s)
		}
		sort.Slice(queue, func(i, j int) bool { return queue[i].id < queue[j].id })
		seen := map[string]bool{}
		for len(queue) > 0 {
			s := queue[0]
			queue = queue[1:]
			if seen[s.id] {
				continue
			}
			seen[s.id] = true
			path := byID[s.id]
			if path == "" && failed[s.id] {
				continue
			}
			meta := metadata[s.id]
			base := model.UsageAllocation{ID: "subagent:" + s.id, Kind: "subagent", ToolUseID: s.id, ParentToolUseID: s.parent, SpawnDepth: s.depth, AgentType: meta.AgentType}
			if path == "" {
				base.Status = model.UsageUnavailable
				base.Reason = model.UnavailableSubagentTranscriptMissing
				u.Allocations = append(u.Allocations, base)
				if reason == "" {
					reason = base.Reason
				}
				continue
			}
			if _, err := os.Stat(path); err != nil {
				base.Status = model.UsageUnavailable
				base.Reason = model.UnavailableSubagentTranscriptMissing
				u.Allocations = append(u.Allocations, base)
				if reason == "" {
					reason = base.Reason
				}
				continue
			}
			allocations, children, invalid := readClaudeSubagent(path, &base)
			if len(allocations) == 0 {
				base.Status = model.UsageUnavailable
				base.Reason = model.UnavailableSubagentTranscriptInvalid
				allocations = []model.UsageAllocation{base}
				invalid = true
			}
			for i := range allocations {
				a := &allocations[i]
				if invalid {
					a.Completeness = model.CompletenessPartial
					a.Reason = model.UnavailableSubagentTranscriptInvalid
					if reason == "" {
						reason = a.Reason
					}
				}
				if running[s.id] {
					a.Completeness = model.CompletenessPartial
					a.Reason = model.UnavailableSubagentStillRunning
					if reason == "" {
						reason = a.Reason
					}
				}
				u.Allocations = append(u.Allocations, *a)
			}
			for _, id := range children {
				queue = append(queue, claudeSpawn{id: id, parent: s.id, depth: s.depth + 1})
			}
		}
	} else if len(spawns) > 0 {
		for id := range spawns {
			u.Allocations = append(u.Allocations, model.UsageAllocation{ID: "subagent:" + id, Kind: "subagent", ToolUseID: id, Status: model.UsageUnavailable, Reason: model.UnavailableSubagentTranscriptMissing})
		}
		if reason == "" {
			reason = model.UnavailableSubagentTranscriptMissing
		}
	}
	if reason != "" {
		u.SubagentCollection = model.CompletenessPartial
		u.SubagentCollectionReason = reason
		u.Completeness = model.CompletenessPartial
	} else {
		u.SubagentCollection = model.CompletenessComplete
	}
	totals := model.TokenTotals{}
	tokens := model.TokenCounts{}
	hasTotals := false
	collected := false
	for i := range u.Allocations {
		a := &u.Allocations[i]
		if a.Status != model.UsageCollected {
			continue
		}
		collected = true
		for k, v := range a.Tokens {
			tokens[k] += v
		}
		if a.TokenTotals != nil {
			hasTotals = true
			totals.Input += a.TokenTotals.Input
			totals.Output += a.TokenTotals.Output
			totals.Total += a.TokenTotals.Total
		} else {
			u.Completeness = model.CompletenessPartial
		}
	}
	if collected {
		u.Status = model.UsageCollected
		u.Reason = ""
		u.Tokens = tokens
		if hasTotals {
			u.TokenTotals = &totals
		} else {
			u.TokenTotals = nil
		}
	} else {
		u.Tokens = nil
		u.TokenTotals = nil
		u.Completeness = model.CompletenessPartial
	}
	return result, nil
}

//nolint:funlen // Keeps transcript parsing and message deduplication together.
func readClaudeSubagent(path string, base *model.UsageAllocation) ([]model.UsageAllocation, []string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, true
	}
	defer func() { _ = f.Close() }()
	scanner := newStreamScanner(f)
	type message struct {
		model    string
		tokens   model.TokenCounts
		complete bool
	}
	messages := map[string]message{}
	anonymous := []message{}
	children := []string{}
	invalid := false
	for scanner.Scan() {
		e, err := parseClaudeEntry(scanner.Bytes())
		if err != nil {
			invalid = true
			continue
		}
		ids, _, bad := claudeTools(&e)
		children = append(children, ids...)
		if bad {
			invalid = true
		}
		if e.Type != "assistant" || e.Message.Model == "<synthetic>" || len(e.Message.Usage) == 0 {
			continue
		}
		counts, complete, err := tokenCountsFromObject(e.Message.Usage, claudeTokenFields)
		if err != nil {
			invalid = true
			continue
		}
		m := message{e.Message.Model, counts, complete}
		if e.Message.ID == "" {
			anonymous = append(anonymous, m)
		} else {
			messages[e.Message.ID] = m
		}
	}
	if scanner.Err() != nil {
		invalid = true
	}
	all := append([]message{}, anonymous...)
	for _, m := range messages {
		all = append(all, m)
	}
	byModel := map[string]*model.UsageAllocation{}
	for _, m := range all {
		if m.model == "" {
			invalid = true
			continue
		}
		a := byModel[m.model]
		if a == nil {
			v := *base
			v.Model = m.model
			v.ID = base.ID + ":" + m.model
			v.Status = model.UsageCollected
			v.Completeness = model.CompletenessComplete
			v.Tokens = model.TokenCounts{}
			byModel[m.model] = &v
			a = &v
		}
		for k, v := range m.tokens {
			a.Tokens[k] += v
		}
		if !m.complete {
			a.Completeness = model.CompletenessPartial
			invalid = true
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
		if a.Completeness == model.CompletenessComplete {
			input := a.Tokens[model.TokenInput] + a.Tokens[model.TokenCachedInput] + a.Tokens[model.TokenCacheWrite]
			output := a.Tokens[model.TokenOutput]
			a.TokenTotals = &model.TokenTotals{Input: input, Output: output, Total: input + output}
		}
		allocations = append(allocations, *a)
	}
	return allocations, children, invalid
}

func claudeTranscriptPaths(session string, uc UsageContext) (parentPath, subagentDir string, reason model.UnavailableReason) {
	if strings.ContainsAny(session, "/\\") || session == "." || session == ".." {
		return "", "", model.UnavailableSubagentSpanUnavailable
	}
	root := ""
	for _, entry := range uc.Env {
		if value, ok := strings.CutPrefix(entry, "CLAUDE_CONFIG_DIR="); ok {
			root = value
		}
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", model.UnavailableSubagentSpanUnavailable
		}
		root = filepath.Join(home, ".claude")
	}
	work := uc.Workdir
	if work == "" {
		work, _ = os.Getwd()
	}
	abs, err := filepath.Abs(work)
	if err != nil {
		return "", "", model.UnavailableSubagentSpanUnavailable
	}
	encoded := claudePathUnsafeRe.ReplaceAllString(abs, "-")
	path := filepath.Join(root, "projects", encoded, session+".jsonl")
	if _, err := os.Stat(path); err != nil {
		matches, globErr := filepath.Glob(filepath.Join(root, "projects", "*", session+".jsonl"))
		if globErr != nil {
			return "", "", model.UnavailableSubagentSpanUnavailable
		}
		if len(matches) > 1 {
			return "", "", model.UnavailableTranscriptAmbiguous
		}
		if len(matches) == 0 {
			return "", "", model.UnavailableSubagentSpanUnavailable
		}
		path = matches[0]
	}
	return path, filepath.Join(filepath.Dir(path), session, "subagents"), ""
}
