package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

func TestInteractiveClaudeSpan(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	session := "11111111-1111-1111-1111-111111111111"
	uc := UsageContext{Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + dir}, StateDir: t.TempDir()}
	a := &ClaudeAdapter{}
	p, err := a.PrepareInteractiveUsage(session, false, uc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "projects", "short", session+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","message":{"id":"m1","model":"opus","content":[],"usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":8}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	got := a.ExtractInteractiveUsage(p, uc)
	if got.Usage.Status != model.UsageCollected || got.Usage.Tokens[model.TokenOutput] != 8 || got.Usage.Source != "claude:session-transcript" {
		t.Fatalf("usage: %+v", got)
	}
	p, err = a.PrepareInteractiveUsage(session, true, uc)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(line + "broken\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	got = a.ExtractInteractiveUsage(p, uc)
	if got.Usage.Tokens[model.TokenOutput] != 8 || got.Usage.Completeness != model.CompletenessPartial {
		t.Fatalf("span: %+v", got)
	}
}

type interactiveUsageFixture struct {
	t             *testing.T
	a             *ClaudeAdapter
	uc            UsageContext
	session, path string
	p             InteractiveUsagePlan
}

func newInteractiveUsageFixture(t *testing.T) *interactiveUsageFixture {
	t.Helper()
	dir := t.TempDir()
	session := "11111111-1111-1111-1111-111111111111"
	f := &interactiveUsageFixture{t: t, a: &ClaudeAdapter{}, session: session, uc: UsageContext{Workdir: t.TempDir(), Env: []string{"CLAUDE_CONFIG_DIR=" + dir}, StateDir: t.TempDir()}}
	f.path = filepath.Join(dir, "projects", "short", session+".jsonl")
	var err error
	f.p, err = f.a.PrepareInteractiveUsage(session, false, f.uc)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *interactiveUsageFixture) write(path string, entries ...any) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	var data []byte
	for _, e := range entries {
		if raw, ok := e.(string); ok {
			data = append(data, []byte(raw)...)
		} else {
			raw, err := json.Marshal(e)
			if err != nil {
				f.t.Fatal(err)
			}
			data = append(data, raw...)
		}
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func usageTuple(output int) map[string]any {
	return map[string]any{"input_tokens": 2, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 4, "output_tokens": output}
}
func assistantFixture(id, name string, output, second int) map[string]any {
	return map[string]any{"type": "assistant", "timestamp": fmt.Sprintf("2026-10-10T00:00:%02dZ", second), "message": map[string]any{"id": id, "model": name, "content": []any{}, "usage": usageTuple(output)}}
}
func promptFixture() map[string]any {
	return map[string]any{"type": "user", "promptId": "prompt", "message": map[string]any{"content": "work"}}
}
func reportFixture(session, prompt string, cost float64, output int) ClaudeStatusReport {
	raw, _ := json.Marshal(usageTuple(output))
	value := cost
	return ClaudeStatusReport{RecordedAt: time.Date(2026, 10, 10, 0, 1, 0, 0, time.UTC), SessionID: session, PromptID: prompt, TotalCostUSD: &value, CurrentUsage: raw}
}

func TestInteractiveClaudeEvidenceReasons(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []any
		reason  model.UnavailableReason
	}{
		{"missing", nil, model.UnavailableTranscriptMissing},
		{"prompt-only", []any{promptFixture()}, model.UnavailableNoUsageEvent},
		{"malformed", []any{assistantFixture("m", "opus", 8, 10), "invalid"}, model.UnavailableTranscriptInvalid},
		{"missing-usage", []any{assistantFixture("m", "opus", 8, 10), map[string]any{"type": "assistant", "message": map[string]any{"id": "empty", "model": "opus", "content": []any{}}}}, model.UnavailableNoUsageEvent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInteractiveUsageFixture(t)
			if tc.entries != nil {
				f.write(f.path, tc.entries...)
			}
			got := f.a.ExtractInteractiveUsage(f.p, f.uc)
			if got.Usage.Allocations[0].Reason != tc.reason {
				t.Fatalf("got %+v", got)
			}
			if got.Usage.RawCumulativeCostUSD != nil {
				t.Fatal("cumulative cost")
			}
		})
	}
	t.Run("shrank", func(t *testing.T) {
		f := newInteractiveUsageFixture(t)
		f.write(f.path, assistantFixture("old", "opus", 8, 10))
		f.p, _ = f.a.PrepareInteractiveUsage(f.session, true, f.uc)
		f.write(f.path)
		got := f.a.ExtractInteractiveUsage(f.p, f.uc)
		if got.Usage.Reason != model.UnavailableTranscriptSpanUnavailable {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		f := newInteractiveUsageFixture(t)
		f.write(f.path, assistantFixture("m", "opus", 8, 10))
		f.write(filepath.Join(filepath.Dir(filepath.Dir(f.path)), "other", f.session+".jsonl"), assistantFixture("m", "opus", 8, 10))
		got := f.a.ExtractInteractiveUsage(f.p, f.uc)
		if got.Usage.Reason != model.UnavailableTranscriptAmbiguous {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("dedupe-models-inherit", func(t *testing.T) {
		f := newInteractiveUsageFixture(t)
		f.write(f.path, promptFixture(), assistantFixture("m", "opus", 8, 10), assistantFixture("m", "opus", 640, 10), assistantFixture("n", "haiku", 7, 11), map[string]any{"type": "last-prompt"})
		got := f.a.ExtractInteractiveUsage(f.p, f.uc)
		if got.Usage.Tokens[model.TokenOutput] != 647 || len(got.Usage.Allocations) != 2 || got.Usage.Completeness != model.CompletenessComplete {
			t.Fatalf("%+v", got)
		}
		f.p, _ = f.a.PrepareInteractiveUsage(f.session, true, f.uc)
		file, err := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(assistantFixture("next", "opus", 13, 12))
		_, err = file.Write(append(raw, '\n'))
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		got = f.a.ExtractInteractiveUsage(f.p, f.uc)
		if got.Usage.Tokens[model.TokenOutput] != 13 {
			t.Fatalf("%+v", got)
		}
	})
}

func TestInteractiveClaudeCostEligibilityINT004(t *testing.T) {
	for _, tc := range []struct {
		name     string
		baseline float64
		mutate   func([]ClaudeStatusReport)
		reason   model.UnavailableReason
	}{
		{name: "fresh"}, {name: "resumed", baseline: 1},
		{name: "lost-startup", mutate: func(r []ClaudeStatusReport) { r[0].PromptID = "prompt"; r[0].CurrentUsage = nil }, reason: model.UnavailableCostBaselineMissing},
		{name: "mid-stream", mutate: func(r []ClaudeStatusReport) { r[0].PromptID = "prompt" }, reason: model.UnavailableCostBaselineMissing},
		{name: "no-prompt-evidence", mutate: func(r []ClaudeStatusReport) { r[1].PromptID = "" }, reason: model.UnavailableCostBaselineMissing},
		{name: "wrong-prompt", mutate: func(r []ClaudeStatusReport) { r[1].PromptID = "other" }, reason: model.UnavailableCostBaselineMissing},
		{name: "session-switched", mutate: func(r []ClaudeStatusReport) { r[0].SessionID = "other" }, reason: model.UnavailableCostSessionMismatch},
		{name: "counter-reset", baseline: 2, reason: model.UnavailableCounterReset},
		{name: "stale", mutate: func(r []ClaudeStatusReport) { r[1].CurrentUsage, _ = json.Marshal(usageTuple(7)) }, reason: model.UnavailableCostReportStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInteractiveUsageFixture(t)
			f.write(f.path, promptFixture(), assistantFixture("m", "opus", 8, 10))
			reports := []ClaudeStatusReport{reportFixture(f.session, "", tc.baseline, 0), reportFixture(f.session, "prompt", 1.5, 8)}
			if tc.mutate != nil {
				tc.mutate(reports)
			}
			f.write(f.p.ReportPath, reports[0], reports[1])
			got := f.a.ExtractInteractiveUsage(f.p, f.uc)
			if got.CostUnavailableReason != tc.reason {
				t.Fatalf("%+v", got)
			}
			if tc.reason == "" {
				if got.EstimatedCostUSD == nil || *got.EstimatedCostUSD != 1.5-tc.baseline {
					t.Fatalf("%+v", got)
				}
			} else if got.EstimatedCostUSD != nil {
				t.Fatal("cost must be null")
			}
			if got.Usage.Tokens[model.TokenOutput] != 8 {
				t.Fatal("tokens changed")
			}
			if tc.name == "session-switched" && got.Usage.Completeness != model.CompletenessPartial {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func spawnFixture(id string, second int) map[string]any {
	e := assistantFixture("spawn-"+id, "opus", 5, second)
	e["message"].(map[string]any)["content"] = []any{map[string]any{"type": "tool_use", "name": "Task", "id": id}}
	return e
}
func terminalFixture(id, kind string, second int) map[string]any {
	text := "<task-notification><tool-use-id>" + id + "</tool-use-id><status>completed</status></task-notification>"
	e := map[string]any{"type": kind, "timestamp": fmt.Sprintf("2026-10-10T00:00:%02dZ", second)}
	switch kind {
	case "attachment":
		e["attachment"] = map[string]any{"type": "queued_command", "prompt": text}
	case "queue-operation":
		e["content"] = text
	default:
		e["message"] = map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id}}}
	}
	return e
}
func (f *interactiveUsageFixture) sub(id string, entries ...any) {
	path := filepath.Join(filepath.Dir(f.path), f.session, "subagents", "agent-"+id)
	f.write(path+".meta.json", map[string]any{"toolUseId": id, "agentType": "Explore"})
	f.write(path+".jsonl", entries...)
}
func TestInteractiveClaudeLifecycleINT001(t *testing.T) {
	for _, kind := range []string{"user", "attachment", "queue-operation", "missing", "reactivated", "streamed-after-notification", "late", "nested-unfinished", "nested-finished"} {
		t.Run(kind, func(t *testing.T) {
			f := newInteractiveUsageFixture(t)
			entries := []any{promptFixture(), spawnFixture("a", 5)}
			async := terminalFixture("a", "user", 6)
			async["toolUseResult"] = map[string]any{"isAsync": true, "status": "async_launched"}
			entries = append(entries, async)
			sub := []any{assistantFixture("sub", "haiku", 12, 8)}
			if kind == "nested-unfinished" || kind == "nested-finished" {
				sub = append(sub, spawnFixture("b", 9))
				f.sub("b", assistantFixture("nested", "haiku", 3, 9))
			}
			if kind != "missing" {
				terminalKind := kind
				if kind == "reactivated" || kind == "streamed-after-notification" || kind == "late" || (kind == "nested-unfinished" || kind == "nested-finished") {
					terminalKind = "attachment"
				}
				at := 10
				if kind == "late" {
					at = 20
				}
				entries = append(entries, terminalFixture("a", terminalKind, at))
			}
			if kind == "nested-finished" {
				sub = append(sub, terminalFixture("b", "attachment", 9))
			}
			if kind == "reactivated" {
				sub = append(sub, assistantFixture("again", "haiku", 2, 21))
			}
			if kind == "streamed-after-notification" {
				sub = append(sub, assistantFixture("sub", "haiku", 22, 21))
			}
			final := assistantFixture("final", "opus", 8, 15)
			if kind == "late" {
				entries = append(entries[:len(entries)-1], final, entries[len(entries)-1])
			} else {
				entries = append(entries, final)
			}
			f.write(f.path, entries...)
			f.sub("a", sub...)
			f.write(f.p.ReportPath, reportFixture(f.session, "", 0, 0), reportFixture(f.session, "prompt", 0.42, 8))
			got := f.a.ExtractInteractiveUsage(f.p, f.uc)
			partial := kind == "missing" || kind == "reactivated" || kind == "streamed-after-notification" || kind == "nested-unfinished"
			if (got.Usage.SubagentCollection == model.CompletenessPartial) != partial {
				t.Fatalf("%+v", got)
			}
			if partial || kind == "late" {
				if got.CostUnavailableReason != model.UnavailableCostSubagentUnsettled {
					t.Fatalf("%+v", got)
				}
			} else if got.EstimatedCostUSD == nil || *got.EstimatedCostUSD != 0.42 {
				t.Fatalf("%+v", got)
			}
			if len(got.Usage.Allocations) < 2 {
				t.Fatal("subagent absent")
			}
		})
	}
}

func TestInteractiveClaudeSettingsINT005(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	sub := filepath.Join(root, "sub")
	worktree := filepath.Join(t.TempDir(), "linked")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "HOME="+home)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init")
	run("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	run("worktree", "add", "-b", "linked", worktree)
	f := newInteractiveUsageFixture(t)
	setting := func(command string, padding int) map[string]any {
		return map[string]any{"statusLine": map[string]any{"type": "command", "command": command, "padding": padding, "refreshInterval": 500, "hideVimModeIndicator": true}}
	}
	f.write(filepath.Join(root, ".claude", "settings.local.json"), setting("printf ROOT", 3))
	f.write(filepath.Join(sub, ".claude", "settings.local.json"), setting("printf LEGACY", 4))
	f.write(filepath.Join(sub, ".claude", "settings.json"), setting("printf SHARED", 5))
	_, config, _ := claudeConfigHome(f.uc)
	f.write(filepath.Join(config, "settings.json"), setting("printf USER", 6))
	for _, tc := range []struct {
		work, command string
		padding       float64
	}{{root, "ROOT", 3}, {sub, "ROOT", 3}, {worktree, "ROOT", 3}, {t.TempDir(), "USER", 6}} {
		t.Run(tc.command+tc.work, func(t *testing.T) {
			uc := f.uc
			uc.Workdir = tc.work
			p, err := f.a.PrepareInteractiveUsage(f.session, false, uc)
			if err != nil {
				t.Fatal(err)
			}
			if !p.ReportEnabled {
				t.Fatalf("disabled: %+v", p)
			}
			args, err := f.a.BuildArgsWithError(&BuildArgsInput{Context: ContextAutonomousInteractive, SessionID: f.session, CompletionCommand: &CompletionCommand{Executable: "/bin/echo", Args: []string{"step", "complete"}}, InteractiveUsage: &p})
			if err != nil {
				t.Fatal(err)
			}
			var settings map[string]any
			for i, arg := range args {
				if arg == "--settings" {
					if err = json.Unmarshal([]byte(args[i+1]), &settings); err != nil {
						t.Fatal(err)
					}
				}
			}
			line := settings["statusLine"].(map[string]any)
			if line["padding"] != tc.padding || !strings.Contains(line["command"].(string), tc.command) || settings["hooks"] == nil || line["hideVimModeIndicator"] != true {
				t.Fatalf("settings: %+v", settings)
			}
		})
	}
	f.write(filepath.Join(sub, ".claude", "settings.json"), "invalid")
	uc := f.uc
	uc.Workdir = sub
	p, _ := f.a.PrepareInteractiveUsage(f.session, false, uc)
	if p.ReportEnabled || p.ReportReason != model.UnavailableCostReportUnavailable {
		t.Fatalf("%+v", p)
	}
	// A broken repository marker makes git resolution fail closed.
	broken := t.TempDir()
	f.write(filepath.Join(broken, ".git"), "gitdir: /missing")
	uc.Workdir = broken
	p, _ = f.a.PrepareInteractiveUsage(f.session, false, uc)
	if p.ReportEnabled {
		t.Fatal("broken git injected a statusLine")
	}
}

func TestWaitForInteractiveFinalReport(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	f.write(f.path, promptFixture(), assistantFixture("m", "opus", 8, 10))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { f.a.WaitForFinalReport(ctx, f.p); close(done) }()
	select {
	case <-done:
		t.Fatal("wait returned without report")
	case <-time.After(30 * time.Millisecond):
	}
	f.write(f.p.ReportPath, reportFixture(f.session, "", 0, 0), reportFixture(f.session, "prompt", 0.42, 8))
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("matching report did not unblock")
	}
	f.write(f.path, promptFixture(), assistantFixture("later", "opus", 9, 11))
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	start := time.Now()
	f.a.WaitForFinalReport(ctx2, f.p)
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("stale report accepted")
	}
}

func TestInteractiveClaudeExactLocationAmbiguous(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	_, config, _ := claudeConfigHome(f.uc)
	project := claudePathUnsafeRe.ReplaceAllString(f.uc.Workdir, "-")
	f.write(filepath.Join(config, "projects", project, f.session+".jsonl"), assistantFixture("m", "opus", 8, 10))
	f.write(f.path, assistantFixture("m", "opus", 8, 10))
	got := f.a.ExtractInteractiveUsage(f.p, f.uc)
	if got.Usage.Reason != model.UnavailableTranscriptAmbiguous {
		t.Fatalf("ambiguous exact path: %+v", got)
	}
}

func TestInteractiveClaudeGitFailureOutsideRepository(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	p, _ := f.a.PrepareInteractiveUsage(f.session, false, f.uc)
	if p.ReportEnabled {
		t.Fatal("unknown git failure enabled capture")
	}
}

func TestInteractiveClaudeParallelSubagents(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	entries := []any{promptFixture()}
	for _, id := range []string{"a", "b", "c"} {
		entries = append(entries, spawnFixture(id, 5), terminalFixture(id, "queue-operation", 10))
		f.sub(id, assistantFixture("sub-"+id, "haiku", 12, 8))
	}
	entries = append(entries, assistantFixture("final", "opus", 8, 15))
	f.write(f.path, entries...)
	f.write(f.p.ReportPath, reportFixture(f.session, "", 0, 0), reportFixture(f.session, "prompt", 0.42, 8))
	got := f.a.ExtractInteractiveUsage(f.p, f.uc)
	if len(got.Usage.Allocations) != 4 || got.Usage.Completeness != model.CompletenessComplete || got.EstimatedCostUSD == nil || *got.EstimatedCostUSD != 0.42 || got.Usage.Tokens[model.TokenOutput] != 59 {
		t.Fatalf("%+v", got)
	}
}

func TestInteractiveClaudeDamagedStreamingUpdateRetainsSubtotal(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	last := assistantFixture("m", "opus", 8, 11)
	delete(last["message"].(map[string]any), "usage")
	f.write(f.path, assistantFixture("m", "opus", 8, 10), last)
	got := f.a.ExtractInteractiveUsage(f.p, f.uc)
	if got.Usage.Tokens[model.TokenOutput] != 8 || got.Usage.Completeness != model.CompletenessPartial {
		t.Fatalf("%+v", got)
	}
}

func TestInteractiveClaudeHomeRepositoryUsesWorkdirLocal(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	home := t.TempDir()
	sub := filepath.Join(home, "sub")
	cmd := exec.Command("git", "init", home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	f.write(filepath.Join(home, ".claude", "settings.local.json"), map[string]any{"statusLine": map[string]any{"type": "command", "command": "ROOT"}})
	f.write(filepath.Join(sub, ".claude", "settings.local.json"), map[string]any{"statusLine": map[string]any{"type": "command", "command": "WORKDIR"}})
	f.uc.Workdir = sub
	f.uc.Env = append(f.uc.Env, "HOME="+home)
	line, err := resolveClaudeStatusLine(f.uc)
	if err != nil {
		t.Fatal(err)
	}
	if line["command"] != "WORKDIR" {
		t.Fatalf("home repository selected: %+v", line)
	}
}

func TestInteractiveSpanExactPositions(t *testing.T) {
	for _, ending := range []string{"\r\n", ""} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			dir := t.TempDir()
			raw, err := json.Marshal(assistantFixture("m", "opus", 8, 1))
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, ending...)
			if err = os.WriteFile(filepath.Join(dir, "span"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			s := readClaudeInteractiveSpan(root, "span", 0, true)
			if s.messages["m"].position != int64(len(raw)) {
				t.Fatalf("position=%d want %d", s.messages["m"].position, len(raw))
			}
		})
	}
}

func TestInteractiveReportRetry(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	name := "same-attempt.statusline.jsonl"
	f.uc.ReportName = &name
	first, err := f.a.PrepareInteractiveUsage(f.session, false, f.uc)
	if err != nil || !first.ReportEnabled {
		t.Fatalf("first plan: %+v %v", first, err)
	}
	f.write(first.ReportPath, reportFixture(f.session, "", 2, 0))
	second, err := f.a.PrepareInteractiveUsage(f.session, false, f.uc)
	if err != nil || !second.ReportEnabled || second.ReportPath == first.ReportPath {
		t.Fatalf("retry plan: %+v %v", second, err)
	}
	if len(readClaudeReports(first.ReportPath)) != 1 || len(readClaudeReports(second.ReportPath)) != 0 {
		t.Fatal("retry reused or damaged the startup baseline")
	}
}

func TestInteractiveReportDiagnostic(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	path := filepath.Join(f.uc.Workdir, ".claude", "settings.json")
	f.write(path, "invalid settings")
	p, err := f.a.PrepareInteractiveUsage(f.session, false, f.uc)
	if err != nil || p.ReportEnabled || !strings.Contains(p.ReportError, path) {
		t.Fatalf("plan diagnostic: %+v %v", p, err)
	}
	got := f.a.ExtractInteractiveUsage(p, f.uc)
	if got.CostReportError != p.ReportError {
		t.Fatalf("lost diagnostic: %+v", got)
	}
}

func TestClaudeJSONLIncrementalAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream")
	if err := os.WriteFile(path, []byte("first\r\npartial"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openClaudeScopedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r := claudeJSONLReader{file: file}
	var lines []string
	consume := func(line []byte, _ int64) error { lines = append(lines, string(line)); return nil }
	if err = r.read(context.Background(), false, consume); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "first\r\n" || r.offset != 14 {
		t.Fatalf("first read: %q offset=%d", lines, r.offset)
	}
	if err = r.read(context.Background(), false, consume); err != nil || len(lines) != 1 {
		t.Fatalf("reread old bytes: %q %v", lines, err)
	}
	appendFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer appendFile.Close()
	if _, err = appendFile.WriteString(" tail\nnext\n"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = r.read(ctx, false, func(line []byte, position int64) error {
		lines = append(lines, string(line))
		if position != 20 {
			t.Fatalf("position=%d", position)
		}
		cancel()
		return nil
	})
	if err != context.Canceled || len(lines) != 2 || lines[1] != "partial tail\n" {
		t.Fatalf("cancellation: %q %v", lines, err)
	}
	if err = r.read(context.Background(), false, consume); err != nil || len(lines) != 3 || lines[2] != "next\n" {
		t.Fatalf("remaining data: %q %v", lines, err)
	}
}

func TestInteractiveSpanAfterOversizedLine(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	f.write(f.path, `{"type":"user","text":"`+strings.Repeat("x", 17*1024*1024)+`"}`, assistantFixture("m", "opus", 8, 1))
	got := f.a.ExtractInteractiveUsage(f.p, f.uc)
	if got.Usage.Tokens[model.TokenOutput] != 8 {
		t.Fatalf("later usage lost: %+v", got.Usage)
	}
}

func TestWaitForInteractiveFinalReportAfterMalformedLine(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	f.write(f.path, promptFixture(), assistantFixture("m", "opus", 8, 10))
	f.write(f.p.ReportPath, "broken report")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { f.a.WaitForFinalReport(ctx, f.p); close(done) }()
	select {
	case <-done:
		t.Fatal("malformed report ended final-report wait")
	case <-time.After(50 * time.Millisecond):
	}
	raw, err := json.Marshal(reportFixture(f.session, "prompt", 0.42, 8))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(f.p.ReportPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if ctx.Err() != nil {
			t.Fatal("wait reached deadline instead of matching report")
		}
	case <-ctx.Done():
		t.Fatal("valid final report after malformed line did not unblock")
	}
}

// A line that cannot be parsed cannot be shown to name the step's session or
// to precede the baseline, so the whole report file is rejected for cost.
func TestInteractiveClaudeCostRejectsMalformedReport(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	f.write(f.path, promptFixture(), assistantFixture("m", "opus", 8, 10))
	f.write(f.p.ReportPath, reportFixture(f.session, "", 0, 0), "broken report", reportFixture(f.session, "prompt", 1.5, 8))
	got := f.a.ExtractInteractiveUsage(f.p, f.uc)
	if got.EstimatedCostUSD != nil || got.CostUnavailableReason != model.UnavailableCostReportUnavailable {
		t.Fatalf("malformed report must null cost: %+v", got)
	}
	if got.Usage.Tokens[model.TokenOutput] != 8 {
		t.Fatalf("tokens changed: %+v", got.Usage)
	}
}

func TestClaudeJSONLConsumerErrorRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream")
	if err := os.WriteFile(path, []byte("bad\ngood\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openClaudeScopedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r := claudeJSONLReader{file: file}
	err = r.read(context.Background(), false, func([]byte, int64) error { return fmt.Errorf("rejected line") })
	if err == nil {
		t.Fatal("consumer error was lost")
	}
	var got string
	if err = r.read(context.Background(), false, func(line []byte, _ int64) error { got = string(line); return nil }); err != nil {
		t.Fatal(err)
	}
	if got != "good\n" || r.offset != 9 {
		t.Fatalf("recovery mixed consumed lines: %q offset=%d", got, r.offset)
	}
}
