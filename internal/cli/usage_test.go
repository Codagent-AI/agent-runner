package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/model"
)

func TestUsageExtractorCapabilityExists(t *testing.T) {
	if reflect.TypeOf((*UsageExtractor)(nil)).Elem().Kind() != reflect.Interface {
		t.Fatal("UsageExtractor must be an optional adapter capability interface")
	}
}

func TestClaudeUsageExtraction(t *testing.T) {
	adapter := &ClaudeAdapter{}
	extractor := requireUsageExtractor(t, adapter)
	raw := readUsageFixture(t, "claude-stream.jsonl")

	wantCost := 0.0042
	want := UsageExtraction{
		Usage: model.UsageRecord{
			Status: model.UsageCollected, CLI: "claude", Provider: "anthropic",
			Model: "claude-sonnet-4-6",
			Tokens: model.TokenCounts{
				model.TokenInput: 3, model.TokenCachedInput: 101,
				model.TokenCacheWrite: 11, model.TokenOutput: 2,
			},
			Source: "claude:result-event", Completeness: model.CompletenessComplete,
		},
		EstimatedCostUSD: &wantCost,
	}
	got, err := extractor.ExtractUsage(raw)
	if err != nil {
		t.Fatalf("ExtractUsage() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ExtractUsage() mismatch (-want +got):\n%s", diff)
	}
}

func TestClaudeStructuredHeadlessOutput(t *testing.T) {
	adapter := &ClaudeAdapter{}

	t.Run("headless args request stream json", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{Prompt: "fixture", Context: ContextAutonomousHeadless})
		for _, want := range []string{"-p", "--output-format", "stream-json", "--verbose"} {
			if !containsString(args, want) {
				t.Fatalf("headless args %v do not contain %q", args, want)
			}
		}
	})

	t.Run("interactive args remain unstructured", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{Prompt: "fixture", Context: ContextInteractive})
		for _, unwanted := range []string{"--output-format", "stream-json", "--verbose"} {
			if containsString(args, unwanted) {
				t.Fatalf("interactive args %v contain %q", args, unwanted)
			}
		}
	})

	t.Run("capture filter returns result text", func(t *testing.T) {
		filter, ok := any(adapter).(OutputFilter)
		if !ok {
			t.Fatal("ClaudeAdapter does not implement OutputFilter")
		}
		if got := filter.FilterOutput(readUsageFixture(t, "claude-stream.jsonl")); got != "fixture-ok" {
			t.Fatalf("FilterOutput() = %q, want fixture-ok", got)
		}
	})

	t.Run("live stream does not repeat final result", func(t *testing.T) {
		wrapper, ok := any(adapter).(StdoutWrapper)
		if !ok {
			t.Fatal("ClaudeAdapter does not implement StdoutWrapper")
		}
		var display bytes.Buffer
		writer := wrapper.WrapStdout(&display)
		if _, err := io.WriteString(writer, readUsageFixture(t, "claude-stream.jsonl")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if closer, ok := writer.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		}
		if got := display.String(); got != "fixture-ok" {
			t.Fatalf("display = %q, want one copy of fixture-ok", got)
		}
	})
}

func TestCodexUsageExtraction(t *testing.T) {
	extractor := requireUsageExtractor(t, &CodexAdapter{})
	want := UsageExtraction{Usage: model.UsageRecord{
		Status: model.UsageCollected, CLI: "codex", Provider: "openai",
		RawCumulative: model.TokenCounts{
			model.TokenInput: 2521, model.TokenCachedInput: 2432,
			model.TokenOutput: 3, model.TokenReasoning: 19,
		},
		Source: "codex:turn.completed", Completeness: model.CompletenessComplete,
	}}
	got, err := extractor.ExtractUsage(readUsageFixture(t, "codex.jsonl"))
	if err != nil {
		t.Fatalf("ExtractUsage() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ExtractUsage() mismatch (-want +got):\n%s", diff)
	}
	if got.Usage.Tokens != nil {
		t.Fatalf("cumulative Codex usage must leave Tokens empty, got %#v", got.Usage.Tokens)
	}
}

func TestOpenCodeUsageExtraction(t *testing.T) {
	extractor := requireUsageExtractor(t, &OpenCodeAdapter{})
	wantCost := 0.0015
	want := UsageExtraction{
		Usage: model.UsageRecord{
			Status: model.UsageCollected, CLI: "opencode",
			Tokens: model.TokenCounts{
				model.TokenInput: 9751, model.TokenOutput: 7,
				model.TokenReasoning: 13, model.TokenCachedInput: 1793,
				model.TokenCacheWrite: 1, "other:total": 11565,
			},
			Source: "opencode:step_finish", Completeness: model.CompletenessComplete,
		},
		EstimatedCostUSD: &wantCost,
	}
	got, err := extractor.ExtractUsage(readUsageFixture(t, "opencode.jsonl"))
	if err != nil {
		t.Fatalf("ExtractUsage() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ExtractUsage() mismatch (-want +got):\n%s", diff)
	}
}

func TestCopilotUsageExtraction(t *testing.T) {
	adapter := &CopilotAdapter{}
	extractor := requireUsageExtractor(t, adapter)
	want := UsageExtraction{Usage: model.UsageRecord{
		Status: model.UsageCollected, CLI: "copilot", Provider: "github", Model: "gpt-5.4-mini",
		Tokens: model.TokenCounts{model.TokenOutput: 148},
		Source: "copilot:assistant.message", Completeness: model.CompletenessPartial,
	}}
	got, err := extractor.ExtractUsage(readUsageFixture(t, "copilot.jsonl"))
	if err != nil {
		t.Fatalf("ExtractUsage() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ExtractUsage() mismatch (-want +got):\n%s", diff)
	}
	if got.EstimatedCostUSD != nil {
		t.Fatalf("Copilot credits must not be converted to USD, got %v", *got.EstimatedCostUSD)
	}
}

func TestCopilotStructuredHeadlessOutput(t *testing.T) {
	adapter := &CopilotAdapter{}

	t.Run("headless args request json", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{Prompt: "fixture", Context: ContextAutonomousHeadless})
		for _, want := range []string{"--output-format", "json"} {
			if !containsString(args, want) {
				t.Fatalf("headless args %v do not contain %q", args, want)
			}
		}
	})

	t.Run("interactive args remain unstructured", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{Prompt: "fixture", Context: ContextInteractive})
		for _, unwanted := range []string{"--output-format", "json"} {
			if containsString(args, unwanted) {
				t.Fatalf("interactive args %v contain %q", args, unwanted)
			}
		}
	})

	t.Run("capture filter returns final response", func(t *testing.T) {
		filter, ok := any(adapter).(OutputFilter)
		if !ok {
			t.Fatal("CopilotAdapter does not implement OutputFilter")
		}
		if got := filter.FilterOutput(readUsageFixture(t, "copilot.jsonl")); got != "fixture-ok" {
			t.Fatalf("FilterOutput() = %q, want fixture-ok", got)
		}
	})
}

func TestCursorUsageExtraction(t *testing.T) {
	extractor := requireUsageExtractor(t, &CursorAdapter{})
	want := UsageExtraction{Usage: model.UsageRecord{
		Status: model.UsageCollected, CLI: "cursor", Provider: "cursor", Model: "Auto",
		Tokens: model.TokenCounts{
			model.TokenInput: 13740, model.TokenOutput: 31,
			model.TokenCachedInput: 4096, model.TokenCacheWrite: 0,
		},
		Source: "cursor:result-event", Completeness: model.CompletenessComplete,
	}}
	got, err := extractor.ExtractUsage(readUsageFixture(t, "cursor.jsonl"))
	if err != nil {
		t.Fatalf("ExtractUsage() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ExtractUsage() mismatch (-want +got):\n%s", diff)
	}
	if got.EstimatedCostUSD != nil {
		t.Fatalf("Cursor does not report USD cost, got %v", *got.EstimatedCostUSD)
	}
}

func TestUsageExtractionUnavailableAndMalformed(t *testing.T) {
	tests := []struct {
		name    string
		adapter Adapter
		valid   string
		cli     string
		source  string
	}{
		{name: "claude", adapter: &ClaudeAdapter{}, valid: `{"type":"system","subtype":"init"}` + "\n", cli: "claude", source: "claude:result-event"},
		{name: "codex", adapter: &CodexAdapter{}, valid: `{"type":"turn.started"}` + "\n", cli: "codex", source: "codex:turn.completed"},
		{name: "opencode", adapter: &OpenCodeAdapter{}, valid: `{"type":"text","part":{"type":"text","text":"ok"}}` + "\n", cli: "opencode", source: "opencode:step_finish"},
		{name: "copilot", adapter: &CopilotAdapter{}, valid: `{"type":"result","usage":{"premiumRequests":1}}` + "\n", cli: "copilot", source: "copilot:assistant.message"},
		{name: "cursor", adapter: &CursorAdapter{}, valid: `{"type":"result","result":"ok"}` + "\n", cli: "cursor", source: "cursor:result-event"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" no usage event", func(t *testing.T) {
			got, err := requireUsageExtractor(t, tt.adapter).ExtractUsage(tt.valid)
			if err != nil {
				t.Fatalf("ExtractUsage() error = %v", err)
			}
			want := UsageExtraction{Usage: model.UsageRecord{
				Status: model.UsageUnavailable, Reason: model.UnavailableNoUsageEvent,
				CLI: tt.cli, Source: tt.source,
			}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("ExtractUsage() mismatch (-want +got):\n%s", diff)
			}
		})
		t.Run(tt.name+" malformed output", func(t *testing.T) {
			if _, err := requireUsageExtractor(t, tt.adapter).ExtractUsage("not-json\n"); err == nil {
				t.Fatal("ExtractUsage() error = nil, want parse error")
			}
		})
	}
}

func TestUsageExtractionEdgeSemantics(t *testing.T) {
	t.Run("last Claude result wins and unknown numeric category is preserved", func(t *testing.T) {
		raw := `{"type":"result","usage":{"input_tokens":1},"total_cost_usd":0.1}` + "\n" +
			`{"type":"result","usage":{"input_tokens":2,"future_tokens":7},"total_cost_usd":0.2}` + "\n"
		got, err := requireUsageExtractor(t, &ClaudeAdapter{}).ExtractUsage(raw)
		if err != nil {
			t.Fatal(err)
		}
		wantTokens := model.TokenCounts{model.TokenInput: 2, "other:future_tokens": 7}
		if diff := cmp.Diff(wantTokens, got.Usage.Tokens); diff != "" {
			t.Fatalf("tokens mismatch (-want +got):\n%s", diff)
		}
		if got.Usage.Completeness != model.CompletenessPartial || got.EstimatedCostUSD == nil || *got.EstimatedCostUSD != 0.2 {
			t.Fatalf("extraction = %#v", got)
		}
	})

	t.Run("malformed known token count is a parse failure", func(t *testing.T) {
		raw := `{"type":"result","usage":{"input_tokens":"many","output_tokens":2}}` + "\n"
		if _, err := requireUsageExtractor(t, &ClaudeAdapter{}).ExtractUsage(raw); err == nil {
			t.Fatal("ExtractUsage() error = nil, want malformed token count error")
		}
	})

	t.Run("OpenCode keeps received partial increments", func(t *testing.T) {
		raw := `{"type":"step_finish","part":{"tokens":{"input":9,"output":2},"cost":0.3}}` + "\n"
		got, err := requireUsageExtractor(t, &OpenCodeAdapter{}).ExtractUsage(raw)
		if err != nil {
			t.Fatalf("ExtractUsage() error = %v", err)
		}
		if diff := cmp.Diff(model.TokenCounts{model.TokenInput: 9, model.TokenOutput: 2}, got.Usage.Tokens); diff != "" {
			t.Fatalf("tokens mismatch (-want +got):\n%s", diff)
		}
		if got.Usage.Completeness != model.CompletenessPartial {
			t.Fatalf("completeness = %q, want partial", got.Usage.Completeness)
		}
	})

	t.Run("OpenCode cost remains independent when tokens are absent", func(t *testing.T) {
		raw := `{"type":"step_finish","part":{"cost":0.3}}` + "\n"
		got, err := requireUsageExtractor(t, &OpenCodeAdapter{}).ExtractUsage(raw)
		if err != nil {
			t.Fatalf("ExtractUsage() error = %v", err)
		}
		if got.Usage.Status != model.UsageUnavailable || got.Usage.Reason != model.UnavailableNoUsageEvent {
			t.Fatalf("usage = %#v, want no-usage-event", got.Usage)
		}
		if got.EstimatedCostUSD == nil || *got.EstimatedCostUSD != 0.3 {
			t.Fatalf("cost = %#v, want 0.3", got.EstimatedCostUSD)
		}
	})

	t.Run("Copilot maps all recorded token metric names but ignores credits", func(t *testing.T) {
		raw := `{"type":"assistant.message","data":{"model":"model-x","inputTokens":1,"cachedInputTokens":2,"cacheWriteTokens":3,"outputTokens":4,"reasoningTokens":5,"futureTokens":6,"cost":99}}` + "\n"
		got, err := requireUsageExtractor(t, &CopilotAdapter{}).ExtractUsage(raw)
		if err != nil {
			t.Fatal(err)
		}
		want := model.TokenCounts{
			model.TokenInput: 1, model.TokenCachedInput: 2, model.TokenCacheWrite: 3,
			model.TokenOutput: 4, model.TokenReasoning: 5, "other:futureTokens": 6,
		}
		if diff := cmp.Diff(want, got.Usage.Tokens); diff != "" {
			t.Fatalf("tokens mismatch (-want +got):\n%s", diff)
		}
		if got.Usage.Completeness != model.CompletenessComplete || got.EstimatedCostUSD != nil {
			t.Fatalf("extraction = %#v", got)
		}
	})
}

func requireUsageExtractor(t *testing.T, adapter Adapter) UsageExtractor {
	t.Helper()
	extractor, ok := adapter.(UsageExtractor)
	if !ok {
		t.Fatalf("%T does not implement UsageExtractor", adapter)
	}
	return extractor
}

func readUsageFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "usage", name))
	if err != nil {
		t.Fatalf("read usage fixture: %v", err)
	}
	return strings.TrimSpace(string(raw)) + "\n"
}
