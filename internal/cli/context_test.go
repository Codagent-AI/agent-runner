package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

func TestBuildContext(t *testing.T) {
	for _, tc := range []struct {
		name       string
		obs        []contextObservation
		missing    bool
		status     model.ContextStatus
		last, peak *int64
	}{
		{"empty", nil, false, model.ContextUnavailable, nil, nil},
		{"ordinary", []contextObservation{{tokens: 180000}, {tokens: 30000}}, false, model.ContextAvailable, contextInt(30000), contextInt(180000)},
		{"earlier unknown", []contextObservation{{unclassified: true}, {tokens: 30000}}, false, model.ContextPartial, contextInt(30000), contextInt(30000)},
		{"trailing unknown", []contextObservation{{tokens: 90000}, {unclassified: true}}, false, model.ContextPartial, nil, contextInt(90000)},
		{"damaged", []contextObservation{{tokens: 90000}}, true, model.ContextPartial, nil, contextInt(90000)},
		{"unknown only", []contextObservation{{unclassified: true}}, false, model.ContextUnavailable, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildContext("test", tc.obs, nil, tc.missing)
			if got.Status != tc.status || !equalContextInt(got.LastRequestInput, tc.last) || !equalContextInt(got.PeakContext, tc.peak) {
				t.Fatalf("context: %+v", got)
			}
			if got.Status != model.ContextAvailable && got.Reason == "" {
				t.Fatal("missing reason")
			}
		})
	}
}
func contextInt(v int64) *int64 { return &v }
func equalContextInt(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func TestClaudeStreamContext(t *testing.T) {
	stdout := readUsageFixture(t, "claude-context-stream.jsonl")
	a := &ClaudeAdapter{}
	got, err := a.ExtractUsage(stdout)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Usage.Context
	if c == nil || c.Status != model.ContextAvailable || !equalContextInt(c.LastRequestInput, contextInt(9)) || !equalContextInt(c.PeakContext, contextInt(150)) || !equalContextInt(c.ContextWindowLimit, contextInt(1000000)) {
		t.Fatalf("context: %+v", c)
	}
	got, err = a.ExtractUsage(stdout + "\n" + `{"type":"assistant","message":{"id":"bad","model":"opus","usage":{"input_tokens":10}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.Context.Status != model.ContextPartial || got.Usage.Context.LastRequestInput != nil {
		t.Fatalf("context: %+v", got.Usage.Context)
	}
}

func TestInteractiveContext(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	priorMessage := assistantFixture("prior", "opus", 1, 1)
	priorMessage["message"].(map[string]any)["usage"].(map[string]any)["input_tokens"] = 999000
	f.write(f.path, priorMessage)
	var err error
	f.p, err = f.a.PrepareInteractiveUsage(f.session, true, f.uc)
	if err != nil {
		t.Fatal(err)
	}
	prior, _ := os.ReadFile(f.path)
	f.write(f.path, strings.TrimSpace(string(prior)), assistantFixture("new", "opus", 3, 2), assistantFixture("synthetic", "<synthetic>", 1000000, 3))
	window := int64(1000000)
	f.write(f.p.ReportPath, ClaudeStatusReport{SessionID: f.session, RecordedAt: time.Now(), ContextWindowSize: &window})
	got := f.a.ExtractInteractiveUsage(f.p, f.uc).Usage.Context
	if got == nil || got.Status != model.ContextAvailable || !equalContextInt(got.PeakContext, contextInt(9)) || !equalContextInt(got.LastRequestInput, contextInt(9)) || !equalContextInt(got.ContextWindowLimit, &window) {
		t.Fatalf("context %+v", got)
	}
	file, err := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("broken\n")
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	got = f.a.ExtractInteractiveUsage(f.p, f.uc).Usage.Context
	if got == nil || got.Status != model.ContextPartial || got.LastRequestInput != nil || !equalContextInt(got.PeakContext, contextInt(9)) {
		t.Fatalf("damaged context %+v", got)
	}
}

func TestInteractiveContextEarlierUnclassified(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	missing := assistantFixture("missing", "opus", 1, 1)
	missing["message"].(map[string]any)["usage"] = map[string]int{"input_tokens": 100}
	f.write(f.path, missing, assistantFixture("final", "opus", 3, 2))
	c := f.a.ExtractInteractiveUsage(f.p, f.uc).Usage.Context
	if c == nil || c.Status != model.ContextPartial || !equalContextInt(c.LastRequestInput, contextInt(9)) {
		t.Fatalf("earlier unknown must preserve eligible final: %+v", c)
	}
}

func TestClaudeContextIgnoresUsageMetadata(t *testing.T) {
	o := claudeContextObservation([]byte(`{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"service_tier":"standard","cache_creation":{"ephemeral_5m_input_tokens":4}}`))
	if o.unclassified || o.tokens != 9 {
		t.Fatalf("metadata corrupted observation: %+v", o)
	}
}

func TestInteractiveContextUsesFinalMessageUsage(t *testing.T) {
	f := newInteractiveUsageFixture(t)
	final := assistantFixture("same", "opus", 2, 2)
	delete(final["message"].(map[string]any), "usage")
	f.write(f.path, assistantFixture("same", "opus", 1, 1), final)
	c := f.a.ExtractInteractiveUsage(f.p, f.uc).Usage.Context
	if c == nil || c.Status != model.ContextUnavailable || c.PeakContext != nil || c.LastRequestInput != nil {
		t.Fatalf("missing final usage must not reuse prior values: %+v", c)
	}
}
