package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
)

var claudeTokenFields = map[string]string{
	"input_tokens": model.TokenInput, "cache_read_input_tokens": model.TokenCachedInput,
	"cache_creation_input_tokens": model.TokenCacheWrite, "output_tokens": model.TokenOutput,
}

// claudeTokenTotals applies Claude's exclusive-input accounting: cache reads
// and writes are added to uncached input exactly once.
func claudeTokenTotals(tokens model.TokenCounts) *model.TokenTotals {
	input := tokens[model.TokenInput] + tokens[model.TokenCachedInput] + tokens[model.TokenCacheWrite]
	output := tokens[model.TokenOutput]
	return &model.TokenTotals{Input: input, Output: output, Total: input + output}
}

type claudeEntry struct {
	UUID            string `json:"uuid"`
	Type            string `json:"type"`
	SessionID       string `json:"session_id"`
	ParentToolUseID string `json:"parent_tool_use_id"`
	ToolUseID       string `json:"tool_use_id"`
	Status          string `json:"status"`
	Subtype         string `json:"subtype"`
	TaskType        string `json:"task_type"`
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

// claudeAgentTask treats an unreported task type as an agent so an older
// stream format cannot hide a spawn.
func claudeAgentTask(taskType string) bool {
	return taskType == "" || strings.Contains(taskType, "agent")
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

// claudeReasonRank orders step-level subagent collection reasons; the most
// fundamental failure is reported when several apply.
var claudeReasonRank = map[model.UnavailableReason]int{
	model.UnavailableSubagentSpanUnavailable:   0,
	model.UnavailableTranscriptAmbiguous:       0,
	model.UnavailableSubagentParentInvalid:     1,
	model.UnavailableSubagentTranscriptMissing: 2,
	model.UnavailableSubagentTranscriptInvalid: 3,
	model.UnavailableSubagentStillRunning:      4,
}

type claudeCollectionReason struct{ reason model.UnavailableReason }

func (c *claudeCollectionReason) note(reason model.UnavailableReason) {
	if reason != "" && (c.reason == "" || claudeReasonRank[reason] < claudeReasonRank[c.reason]) {
		c.reason = reason
	}
}

// claudeStdout is the invocation evidence needed to locate its subagents.
type claudeStdout struct {
	session string
	uuids   map[string]bool
	spawns  map[string]bool
	running map[string]bool
}

func scanClaudeStdout(stdout string) claudeStdout {
	s := claudeStdout{uuids: map[string]bool{}, spawns: map[string]bool{}, running: map[string]bool{}}
	scanner := newStreamScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		e, err := parseClaudeEntry(scanner.Bytes())
		if err != nil {
			continue
		}
		if e.SessionID != "" {
			s.session = e.SessionID
		}
		if e.UUID != "" {
			s.uuids[e.UUID] = true
		}
		if e.ParentToolUseID != "" {
			continue
		}
		ids, _, _ := claudeTools(&e)
		for _, id := range ids {
			s.spawns[id] = true
		}
		if e.ToolUseID == "" {
			continue
		}
		// Background shell tools also emit task events; only agent tasks
		// are subagent spawns. Completion events carry no task type.
		if e.Subtype == "task_started" && claudeAgentTask(e.TaskType) {
			s.running[e.ToolUseID] = true
			s.spawns[e.ToolUseID] = true
		}
		if e.Subtype == "task_notification" || (e.Subtype == "task_updated" && (e.Status == "completed" || e.Status == "failed")) {
			s.running[e.ToolUseID] = false
		}
	}
	return s
}

// ExtractUsageWithContext combines Claude's invocation result with transcript evidence.
// Transcript failures reduce completeness but never fail the invocation.
func (a *ClaudeAdapter) ExtractUsageWithContext(stdout string, uc UsageContext) (UsageExtraction, error) {
	result, err := a.ExtractUsage(stdout)
	if err != nil {
		return result, err
	}
	evidence := scanClaudeStdout(stdout)
	if evidence.session == "" {
		return result, nil
	}
	u := &result.Usage
	u.Allocations = []model.UsageAllocation{{ID: "main", Kind: "main", Status: u.Status, Reason: u.Reason, Model: u.Model, Tokens: u.Tokens, TokenTotals: u.TokenTotals, Completeness: u.Completeness}}

	var reason claudeCollectionReason
	spawns, failed := evidence.spawns, map[string]bool{}
	var transcripts *os.Root
	configRoot, parentRel, subagentsRel, pathReason := claudeTranscriptPaths(evidence.session, uc)
	reason.note(pathReason)
	if configRoot != "" {
		transcripts, err = os.OpenRoot(configRoot)
		if err != nil {
			reason.note(model.UnavailableSubagentSpanUnavailable)
		} else {
			defer func() { _ = transcripts.Close() }()
			spanSpawns, spanFailed, spanReason := scanClaudeParentSpan(transcripts, parentRel, evidence.uuids)
			reason.note(spanReason)
			for id := range spanSpawns {
				spawns[id] = true
			}
			failed = spanFailed
		}
	}
	if transcripts == nil {
		subagentsRel = ""
	}
	u.Allocations = append(u.Allocations, collectClaudeSubagents(transcripts, subagentsRel, spawns, failed, evidence.running, &reason)...)

	if reason.reason != "" {
		u.SubagentCollection = model.CompletenessPartial
		u.SubagentCollectionReason = reason.reason
	} else {
		u.SubagentCollection = model.CompletenessComplete
	}
	sumClaudeAllocations(u)
	return result, nil
}

// scanClaudeParentSpan streams the parent session transcript and returns the
// direct spawns written between the first and last entries this invocation
// reported on stdout. An invalid entry inside the span, or immediately after
// it, could hide a spawn and makes collection partial.
func scanClaudeParentSpan(root *os.Root, parentRel string, uuids map[string]bool) (spawns, failed map[string]bool, reason model.UnavailableReason) {
	f, err := root.Open(parentRel)
	if err != nil {
		return nil, nil, model.UnavailableSubagentSpanUnavailable
	}
	defer func() { _ = f.Close() }()
	spawns, failed = map[string]bool{}, map[string]bool{}
	// Entries after the latest stdout match are held until the next match
	// confirms they lie inside the span.
	var pendingSpawns []string
	pendingFailed := map[string]bool{}
	started, pendingInvalid, invalid, invalidAfterMatch, afterMatch := false, false, false, false, false
	scanner := newStreamScanner(f)
	for scanner.Scan() {
		e, parseErr := parseClaudeEntry(scanner.Bytes())
		matched := parseErr == nil && e.UUID != "" && uuids[e.UUID]
		if !started && !matched {
			continue
		}
		started = true
		entryInvalid := parseErr != nil
		var ids []string
		var errs map[string]bool
		if parseErr == nil && e.ParentToolUseID == "" && !e.IsSidechain {
			var badTool bool
			ids, errs, badTool = claudeTools(&e)
			entryInvalid = badTool || (e.Type == "assistant" && len(e.Message.Content) == 0)
		}
		pendingSpawns = append(pendingSpawns, ids...)
		for id := range errs {
			pendingFailed[id] = true
		}
		pendingInvalid = pendingInvalid || entryInvalid
		if afterMatch && parseErr != nil {
			invalidAfterMatch = true
		}
		afterMatch = matched
		if matched {
			invalidAfterMatch = false
			for _, id := range pendingSpawns {
				spawns[id] = true
			}
			for id := range pendingFailed {
				failed[id] = true
			}
			invalid = invalid || pendingInvalid
			pendingSpawns, pendingFailed, pendingInvalid = nil, map[string]bool{}, false
		}
	}
	if scanner.Err() != nil {
		invalid = true
	}
	switch {
	case !started:
		return spawns, failed, model.UnavailableSubagentSpanUnavailable
	case invalid || invalidAfterMatch:
		return spawns, failed, model.UnavailableSubagentParentInvalid
	}
	return spawns, failed, ""
}

type claudeSidecarFile struct {
	transcript string
	meta       claudeSidecar
}

// indexClaudeSidecars maps spawning tool-use IDs to subagent transcripts.
func indexClaudeSidecars(root *os.Root, subagentsRel string) map[string]claudeSidecarFile {
	index := map[string]claudeSidecarFile{}
	if root == nil || subagentsRel == "" {
		return index
	}
	sidecars, _ := fs.Glob(root.FS(), path.Join(filepath.ToSlash(subagentsRel), "agent-*.meta.json"))
	for _, sidecar := range sidecars {
		var m claudeSidecar
		raw, err := fs.ReadFile(root.FS(), sidecar)
		if err == nil && json.Unmarshal(raw, &m) == nil && m.ToolUseID != "" {
			index[m.ToolUseID] = claudeSidecarFile{transcript: strings.TrimSuffix(sidecar, ".meta.json") + ".jsonl", meta: m}
		}
	}
	return index
}

// collectClaudeSubagents walks spawns breadth-first, following nested spawns
// recorded in each subagent transcript, and returns one allocation per
// subagent and model.
func collectClaudeSubagents(root *os.Root, subagentsRel string, spawns, failed, running map[string]bool, reason *claudeCollectionReason) []model.UsageAllocation {
	if len(spawns) == 0 {
		return nil
	}
	index := indexClaudeSidecars(root, subagentsRel)
	queue := make([]claudeSpawn, 0, len(spawns))
	for id := range spawns {
		queue = append(queue, claudeSpawn{id: id, depth: 1})
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].id < queue[j].id })
	var allocations []model.UsageAllocation
	seen := map[string]bool{}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if seen[s.id] {
			continue
		}
		seen[s.id] = true
		file, found := index[s.id]
		if !found && failed[s.id] {
			continue
		}
		base := model.UsageAllocation{ID: "subagent:" + s.id, Kind: "subagent", ToolUseID: s.id, ParentToolUseID: s.parent, SpawnDepth: s.depth, AgentType: file.meta.AgentType}
		var subagent []model.UsageAllocation
		var children []string
		var invalid bool
		var openErr error
		if found {
			subagent, children, invalid, openErr = readClaudeSubagent(root, file.transcript, &base)
		}
		if !found || errors.Is(openErr, fs.ErrNotExist) {
			base.Status = model.UsageUnavailable
			base.Reason = model.UnavailableSubagentTranscriptMissing
			reason.note(base.Reason)
			allocations = append(allocations, base)
			continue
		}
		if len(subagent) == 0 {
			base.Status = model.UsageUnavailable
			subagent = []model.UsageAllocation{base}
			invalid = true
		}
		for i := range subagent {
			a := &subagent[i]
			if invalid {
				a.Completeness = model.CompletenessPartial
				a.Reason = model.UnavailableSubagentTranscriptInvalid
				reason.note(a.Reason)
			}
			if running[s.id] {
				a.Completeness = model.CompletenessPartial
				a.Reason = model.UnavailableSubagentStillRunning
				reason.note(a.Reason)
			}
		}
		allocations = append(allocations, subagent...)
		for _, id := range children {
			queue = append(queue, claudeSpawn{id: id, parent: s.id, depth: s.depth + 1})
		}
	}
	return allocations
}

// sumClaudeAllocations derives the attempt-level record from its allocations
// so each token is counted exactly once.
func sumClaudeAllocations(u *model.UsageRecord) {
	if u.Allocations[0].Status == model.UsageUnavailable || u.SubagentCollection == model.CompletenessPartial {
		u.Completeness = model.CompletenessPartial
	}
	totals := model.TokenTotals{}
	tokens := model.TokenCounts{}
	hasTotals, collected := false, false
	for i := range u.Allocations {
		a := &u.Allocations[i]
		if a.Status != model.UsageCollected {
			continue
		}
		collected = true
		for k, v := range a.Tokens {
			tokens[k] += v
		}
		if a.TokenTotals == nil {
			u.Completeness = model.CompletenessPartial
			continue
		}
		hasTotals = true
		totals.Input += a.TokenTotals.Input
		totals.Output += a.TokenTotals.Output
		totals.Total += a.TokenTotals.Total
	}
	if !collected {
		u.Tokens = nil
		u.TokenTotals = nil
		u.Completeness = model.CompletenessPartial
		return
	}
	u.Status = model.UsageCollected
	u.Reason = ""
	u.Tokens = tokens
	u.TokenTotals = nil
	if hasTotals {
		u.TokenTotals = &totals
	}
}

//nolint:funlen // Keeps transcript parsing and message deduplication together.
func readClaudeSubagent(root *os.Root, transcript string, base *model.UsageAllocation) (allocations []model.UsageAllocation, children []string, invalid bool, err error) {
	f, err := root.Open(transcript)
	if err != nil {
		return nil, nil, true, err
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
	children = []string{}
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
	allocations = make([]model.UsageAllocation, 0, len(keys))
	for _, k := range keys {
		a := byModel[k]
		if a.Completeness == model.CompletenessComplete {
			a.TokenTotals = claudeTokenTotals(a.Tokens)
		}
		allocations = append(allocations, *a)
	}
	return allocations, children, invalid, nil
}

// claudeTranscriptPaths locates the session's parent transcript and subagent
// directory, returned relative to the Claude config root that holds them.
func claudeTranscriptPaths(session string, uc UsageContext) (configRoot, parentRel, subagentsRel string, reason model.UnavailableReason) {
	if validateSessionID(session) != nil || strings.Contains(session, `\`) {
		return "", "", "", model.UnavailableSubagentSpanUnavailable
	}
	root := ""
	home := ""
	for _, entry := range uc.Env {
		if value, ok := strings.CutPrefix(entry, "CLAUDE_CONFIG_DIR="); ok {
			root = value
		}
		if value, ok := strings.CutPrefix(entry, "HOME="); ok {
			home = value
		}
	}
	if root == "" {
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", "", "", model.UnavailableSubagentSpanUnavailable
			}
		}
		root = filepath.Join(home, ".claude")
	}
	work := uc.Workdir
	if work == "" {
		work, _ = os.Getwd()
	}
	abs, err := filepath.Abs(work)
	if err != nil {
		return "", "", "", model.UnavailableSubagentSpanUnavailable
	}
	project := claudePathUnsafeRe.ReplaceAllString(abs, "-")
	if _, err := os.Stat(filepath.Join(root, "projects", project, session+".jsonl")); err != nil {
		// Claude shortens and hashes long project directory names.
		matches, globErr := filepath.Glob(filepath.Join(root, "projects", "*", session+".jsonl"))
		switch {
		case globErr != nil || len(matches) == 0:
			return "", "", "", model.UnavailableSubagentSpanUnavailable
		case len(matches) > 1:
			return "", "", "", model.UnavailableTranscriptAmbiguous
		}
		project = filepath.Base(filepath.Dir(matches[0]))
	}
	projectRel := filepath.Join("projects", project)
	return root, filepath.Join(projectRel, session+".jsonl"), filepath.Join(projectRel, session, "subagents"), ""
}
