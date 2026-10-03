package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestClaudeSubagentUsageAndInheritedSpan(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	session := "11111111-1111-1111-1111-111111111111"
	project := filepath.Join(root, "projects", "shortened-project")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"),
		`{"uuid":"old","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"old-tool"}]}}`+"\n"+
			`{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"new-tool"}]}}`+"\n"+
			`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	subdir := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(subdir, "agent-old.jsonl"), `{"type":"assistant","message":{"id":"old-message","model":"old-model","usage":{"input_tokens":900,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}}`+"\n")
	writeClaudeFixture(t, filepath.Join(subdir, "agent-old.meta.json"), `{"toolUseId":"old-tool"}`)
	writeClaudeFixture(t, filepath.Join(subdir, "agent-new.meta.json"), `{"toolUseId":"new-tool","agentType":"Explore","spawnDepth":1}`)
	writeClaudeFixture(t, filepath.Join(subdir, "agent-new.jsonl"),
		`{"type":"assistant","message":{"id":"m1","model":"claude-haiku-4-5","usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":8}}}`+"\n"+
			`{"type":"assistant","message":{"id":"m1","model":"claude-haiku-4-5","usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":640}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","subtype":"init","session_id":"` + session + `","model":"claude-opus-5-5"}` + "\n" +
		`{"uuid":"last","type":"assistant","parent_tool_use_id":"new-tool","message":{"model":"claude-haiku-4-5"}}` + "\n" +
		`{"type":"result","session_id":"` + session + `","total_cost_usd":5.77,"usage":{"input_tokens":1,"cache_read_input_tokens":10,"cache_creation_input_tokens":20,"output_tokens":5}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	u := got.Usage
	if u.Model != "claude-opus-5-5" || u.SubagentCollection != model.CompletenessComplete || len(u.Allocations) != 2 {
		t.Fatalf("usage = %+v", u)
	}
	if u.Tokens[model.TokenInput] != 3 || u.Tokens[model.TokenOutput] != 645 || u.TokenTotals == nil || u.TokenTotals.Total != 685 {
		t.Fatalf("totals = %+v", u)
	}
	if u.Allocations[1].Model != "claude-haiku-4-5" || u.Allocations[1].ToolUseID != "new-tool" {
		t.Fatalf("allocation = %+v", u.Allocations[1])
	}
}

func writeClaudeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeSubagentCollectionFailures(t *testing.T) {
	cases := []struct {
		name, parent, sidecar, transcript, stdoutSpawns string
		want                                            model.UnavailableReason
		count                                           int
	}{
		{"missing transcript", `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Task","id":"tool"}]}}` + "\n" + `{"uuid":"last","type":"assistant","message":{"content":[]}}` + "\n", "", "", "", model.UnavailableSubagentTranscriptMissing, 2},
		{"span unavailable", `{"uuid":"other","type":"assistant","message":{"content":[]}}` + "\n", "", "", "", model.UnavailableSubagentSpanUnavailable, 1},
		{"invalid parent", `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}` + "\n" + `not-json` + "\n" + `{"uuid":"last","type":"assistant","message":{"content":[]}}` + "\n", `{"toolUseId":"tool"}`, `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":5}}}` + "\n", "", model.UnavailableSubagentParentInvalid, 2},
		{"invalid subagent", `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}` + "\n" + `{"uuid":"last","type":"assistant","message":{"content":[]}}` + "\n", `{"toolUseId":"tool"}`, `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":5}}}` + "\n" + `not-json` + "\n", "", model.UnavailableSubagentTranscriptInvalid, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			session := "22222222-2222-2222-2222-222222222222"
			project := filepath.Join(root, "projects", "project")
			writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), tc.parent)
			if tc.sidecar != "" {
				sub := filepath.Join(project, session, "subagents")
				writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), tc.sidecar)
				writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), tc.transcript)
			}
			stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4},"total_cost_usd":5.77}` + "\n"
			got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
			if err != nil {
				t.Fatal(err)
			}
			u := got.Usage
			if u.SubagentCollectionReason != tc.want || u.Completeness != model.CompletenessPartial || len(u.Allocations) != tc.count || u.RawCumulativeCostUSD == nil || *u.RawCumulativeCostUSD != 5.77 {
				t.Fatalf("usage=%+v", u)
			}
		})
	}
}

func TestClaudeSubagentNestedAndMultiModel(t *testing.T) {
	root := t.TempDir()
	session := "33333333-3333-3333-3333-333333333333"
	project := filepath.Join(root, "projects", "project")
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"outer"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"outer"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"a","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2},"content":[{"type":"tool_use","name":"Agent","id":"inner"}]}}`+"\n"+`{"type":"assistant","message":{"id":"b","model":"sonnet","usage":{"input_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":4}}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-b.meta.json"), `{"toolUseId":"inner"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-b.jsonl"), `{"type":"assistant","message":{"id":"c","model":"haiku","usage":{"input_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":6}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant"}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	u := got.Usage
	if u.SubagentCollection != model.CompletenessComplete || len(u.Allocations) != 4 || u.Tokens[model.TokenInput] != 10 || u.Tokens[model.TokenOutput] != 13 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestClaudeSubagentCollectedWithoutMainResultUsage(t *testing.T) {
	root := t.TempDir()
	session := "66666666-6666-6666-6666-666666666666"
	project := filepath.Join(root, "projects", "project")
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant"}` + "\n" + `{"type":"result","total_cost_usd":1.2}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	u := got.Usage
	if u.Status != model.UsageCollected || u.Completeness != model.CompletenessPartial || u.Allocations[0].Reason != model.UnavailableNoUsageEvent || u.Tokens[model.TokenInput] != 1 || u.TokenTotals.Total != 10 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestClaudeFailedSpawnDoesNotDegradeCollection(t *testing.T) {
	root := t.TempDir()
	session := "77777777-7777-7777-7777-777777777777"
	project := filepath.Join(root, "projects", "project")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool","is_error":true}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant"}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollection != model.CompletenessComplete || len(got.Usage.Allocations) != 1 {
		t.Fatalf("usage=%+v", got.Usage)
	}
}

func TestClaudeSubagentRunningAndNoSubagents(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "no subagents", true: "still running"}[running], func(t *testing.T) {
			root := t.TempDir()
			session := "88888888-8888-8888-8888-888888888888"
			project := filepath.Join(root, "projects", "project")
			parent := `{"uuid":"first","type":"assistant","message":{"content":[]}}` + "\n" + `{"uuid":"last","type":"assistant","message":{"content":[]}}` + "\n"
			stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n"
			if running {
				parent = `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}` + "\n" + `{"uuid":"last","type":"assistant","message":{"content":[]}}` + "\n"
				sub := filepath.Join(project, session, "subagents")
				writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool"}`)
				writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}}`+"\n")
				stdout += `{"type":"system","subtype":"task_started","tool_use_id":"tool"}` + "\n"
			}
			writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), parent)
			stdout += `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
			got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
			if err != nil {
				t.Fatal(err)
			}
			u := got.Usage
			if running {
				if u.SubagentCollectionReason != model.UnavailableSubagentStillRunning || len(u.Allocations) != 2 || u.Tokens[model.TokenOutput] != 3 {
					t.Fatalf("usage=%+v", u)
				}
			} else {
				if u.SubagentCollection != model.CompletenessComplete || len(u.Allocations) != 1 || u.TokenTotals.Total != 2 {
					t.Fatalf("usage=%+v", u)
				}
			}
		})
	}
}

func TestClaudeParallelSubagentsAllCounted(t *testing.T) {
	root := t.TempDir()
	session := "99999999-9999-9999-9999-999999999999"
	project := filepath.Join(root, "projects", "project")
	sub := filepath.Join(project, session, "subagents")
	parent := `{"uuid":"first","type":"assistant","message":{"content":[`
	for i := 1; i <= 4; i++ {
		if i > 1 {
			parent += ","
		}
		parent += fmt.Sprintf(`{"type":"tool_use","name":"Agent","id":"tool-%d"}`, i)
		writeClaudeFixture(t, filepath.Join(sub, fmt.Sprintf("agent-%d.meta.json", i)), fmt.Sprintf(`{"toolUseId":"tool-%d"}`, i))
		writeClaudeFixture(t, filepath.Join(sub, fmt.Sprintf("agent-%d.jsonl", i)), fmt.Sprintf(`{"type":"assistant","message":{"id":"m%d","model":"haiku","usage":{"input_tokens":%d,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}}`+"\n", i, i))
	}
	parent += `]}}` + "\n" + `{"uuid":"last","type":"assistant","message":{"content":[]}}` + "\n"
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), parent)
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	u := got.Usage
	if u.SubagentCollection != model.CompletenessComplete || len(u.Allocations) != 5 || u.Tokens[model.TokenInput] != 11 || u.TokenTotals.Total != 16 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestClaudeTranscriptPathsUsesInvocationHome(t *testing.T) {
	home := t.TempDir()
	session := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	project := filepath.Join(home, ".claude", "projects", "project")
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"HOME=" + home}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollection != model.CompletenessComplete || len(got.Usage.Allocations) != 2 {
		t.Fatalf("usage=%+v", got.Usage)
	}
}

func TestClaudeTranscriptReadsStayInsideConfigRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	work := t.TempDir()
	session := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	encoded := claudePathUnsafeRe.ReplaceAllString(work, "-")
	writeClaudeFixture(t, filepath.Join(outside, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	projects := filepath.Join(root, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(projects, encoded)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollectionReason != model.UnavailableSubagentSpanUnavailable {
		t.Fatalf("usage=%+v", got.Usage)
	}
}

func TestClaudeBackgroundShellTaskIsNotSubagent(t *testing.T) {
	for _, finished := range []bool{true, false} {
		t.Run(map[bool]string{true: "finished", false: "still running"}[finished], func(t *testing.T) {
			root := t.TempDir()
			session := "55555555-5555-5555-5555-555555555555"
			project := filepath.Join(root, "projects", "project")
			writeClaudeFixture(t, filepath.Join(project, session+".jsonl"),
				`{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"bash-tool"}]}}`+"\n"+
					`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
			stdout := `{"uuid":"first","type":"system","subtype":"init","session_id":"` + session + `","model":"claude-opus-5-5"}` + "\n" +
				`{"type":"system","subtype":"task_started","task_id":"b1","tool_use_id":"bash-tool","task_type":"local_bash"}` + "\n"
			if finished {
				stdout += `{"type":"system","subtype":"task_notification","task_id":"b1","tool_use_id":"bash-tool","status":"completed"}` + "\n"
			}
			stdout += `{"uuid":"last","type":"assistant","message":{"model":"claude-opus-5-5"}}` + "\n" +
				`{"type":"result","session_id":"` + session + `","total_cost_usd":1,"usage":{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4}}` + "\n"
			got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
			if err != nil {
				t.Fatal(err)
			}
			u := got.Usage
			if u.SubagentCollection != model.CompletenessComplete || u.SubagentCollectionReason != "" || len(u.Allocations) != 1 || u.Completeness != model.CompletenessComplete {
				t.Fatalf("background shell task counted as subagent: %+v", u)
			}
		})
	}
}

func TestClaudeSubagentCollectionReasonFollowsPrecedence(t *testing.T) {
	root := t.TempDir()
	session := "66666666-6666-6666-6666-666666666666"
	project := filepath.Join(root, "projects", "project")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"),
		`{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"a-running"},{"type":"tool_use","name":"Agent","id":"b-missing"}]}}`+"\n"+
			`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"a-running"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":1,"cache_creation_input_tokens":1,"output_tokens":1}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","subtype":"init","session_id":"` + session + `","model":"opus"}` + "\n" +
		`{"type":"system","subtype":"task_started","tool_use_id":"a-running","task_type":"local_agent"}` + "\n" +
		`{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" +
		`{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4},"total_cost_usd":1}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollectionReason != model.UnavailableSubagentTranscriptMissing {
		t.Fatalf("reason = %q, want %q (transcript-missing outranks still-running)", got.Usage.SubagentCollectionReason, model.UnavailableSubagentTranscriptMissing)
	}
}

func TestClaudeParentInvalidLineAfterSpanFollowedByValidLine(t *testing.T) {
	root := t.TempDir()
	session := "77777777-7777-7777-7777-777777777777"
	project := filepath.Join(root, "projects", "project")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"),
		`{"uuid":"first","type":"assistant","message":{"content":[]}}`+"\n"+
			`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n"+
			`{"uuid":"trunc`+"\n"+
			`{"uuid":"later","type":"assistant","message":{"content":[]}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" +
		`{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" +
		`{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollectionReason != model.UnavailableSubagentParentInvalid {
		t.Fatalf("usage=%+v", got.Usage)
	}
}

func TestClaudeTranscriptFallbackToleratesGlobMetacharactersInConfigRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cfg[1]")
	session := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	project := filepath.Join(root, "projects", "shortened-project")
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Workdir: t.TempDir(), Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollection != model.CompletenessComplete || len(got.Usage.Allocations) != 2 {
		t.Fatalf("usage=%+v", got.Usage)
	}
}

func TestClaudeSidecarIndexToleratesGlobMetacharactersInProjectDir(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(t.TempDir(), "w[1]")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	session := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	encoded := claudePathUnsafeRe.ReplaceAllString(work, "-")
	project := filepath.Join(root, "projects", encoded)
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollection != model.CompletenessComplete || len(got.Usage.Allocations) != 2 {
		t.Fatalf("usage=%+v", got.Usage)
	}
}

// The and-scene eval sandbox keeps Claude's transcripts in persisted state and
// links ~/.claude/projects to them. Claude follows that link, so collection
// must too, while a project directory linked out of the store stays refused.
func TestClaudeTranscriptsFollowSymlinkedProjectsDir(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	work := t.TempDir()
	session := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(state, filepath.Join(home, ".claude", "projects")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	project := filepath.Join(state, claudePathUnsafeRe.ReplaceAllString(work, "-"))
	sub := filepath.Join(project, session, "subagents")
	writeClaudeFixture(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool"}`)
	writeClaudeFixture(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"claude-haiku-4-5","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"claude-sonnet-5-5"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"claude-sonnet-5-5"}}` + "\n" + `{"type":"result","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}` + "\n"
	got, err := (&ClaudeAdapter{}).ExtractUsageWithContext(stdout, UsageContext{Workdir: work, Env: []string{"HOME=" + home}})
	if err != nil {
		t.Fatal(err)
	}
	u := got.Usage
	if u.SubagentCollection != model.CompletenessComplete || u.SubagentCollectionReason != "" || len(u.Allocations) != 2 {
		t.Fatalf("usage=%+v", u)
	}
	if u.Allocations[1].Model != "claude-haiku-4-5" || u.TokenTotals == nil || u.TokenTotals.Total != 5 {
		t.Fatalf("usage=%+v", u)
	}
}
