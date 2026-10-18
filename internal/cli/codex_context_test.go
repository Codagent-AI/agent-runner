package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

// Exercises rollout discovery through the private home's real sessions symlink.
func TestCodexContextPrivateHomeINT001(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	private, err := prepareCodexRunnerHome(&CompletionCommand{Executable: "/bin/true", Args: []string{"step", "complete"}}, nil, ContextAutonomousHeadless, "context-test")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	stdout := `{"type":"thread.started","thread_id":"session"}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":42,"cached_input_tokens":2,"output_tokens":3}}`
	path := filepath.Join(home, "sessions", "2026", "10", "18", "rollout-test-session.jsonl")
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	record := func(thread, id string, n int64) string {
		raw, _ := json.Marshal(map[string]any{"timestamp": start.Format(time.RFC3339Nano), "type": "token_usage_record", "payload": map[string]any{"thread_id": thread, "response_id": id, "usage": map[string]any{"input_tokens": n}}})
		return string(raw) + "\n"
	}
	data := readUsageFixture(t, "codex-context-rollout.jsonl")
	uc := UsageContext{Env: []string{"CODEX_HOME=" + private}, StartedAt: start}
	a := &CodexAdapter{}
	baseline, err := a.ExtractUsage(stdout)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, data string
		status     model.ContextStatus
		reason     model.UnavailableReason
		last, peak *int64
	}{
		{"eligible", data, model.ContextAvailable, "", contextInt(18000), contextInt(180000)},
		{"trailing", data + record("session", "uncertain", 800000), model.ContextPartial, model.UnavailableContextUnclassifiedUpdate, nil, contextInt(180000)},
		{"older", `{"timestamp":"2026-10-18T12:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":20}}}}` + "\n", model.ContextUnavailable, model.UnavailableCodexRolloutUnsupported, nil, nil},
		{"missing", "", model.ContextUnavailable, model.UnavailableCodexRolloutNotFound, nil, nil},
		{"malformed", data + "broken\n", model.ContextUnavailable, model.UnavailableParseFailure, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.data == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := a.ExtractUsageWithContext(stdout, uc)
			if err != nil {
				t.Fatal(err)
			}
			c := got.Usage.Context
			if c == nil || c.Status != tc.status || c.Reason != tc.reason || !equalContextInt(c.LastRequestInput, tc.last) || !equalContextInt(c.PeakContext, tc.peak) {
				t.Fatalf("context %+v", c)
			}
			if tc.name == "eligible" && !equalContextInt(c.ContextWindowLimit, contextInt(258400)) {
				t.Fatalf("window %+v", c)
			}
			got.Usage.Context = nil
			if diff := cmp.Diff(baseline, got); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestReadCodexContextTolerantReading(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	usage := func(id string, n int64) string {
		return `{"timestamp":"2026-10-18T12:00:00Z","type":"token_usage_record","payload":{"thread_id":"session","response_id":"` + id + `","usage":{"input_tokens":` + strconv.FormatInt(n, 10) + `}}}`
	}
	t.Run("line larger than the stream scanner limit", func(t *testing.T) {
		huge := `{"timestamp":"2026-10-18T12:00:00Z","type":"response_item","payload":{"output":"` + strings.Repeat("x", streamScanBufferMax+1024) + `"}}`
		data := usage("one", 100) + "\n" + huge + "\n" + usage("two", 200) + "\n" + `{"timestamp":"2026-10-18T12:00:00Z","type":"turn_context","payload":{}}` + "\n"
		c := readCodexContext(strings.NewReader(data), "session", start)
		if c.Status != model.ContextAvailable || !equalContextInt(c.LastRequestInput, contextInt(200)) || !equalContextInt(c.PeakContext, contextInt(200)) {
			t.Fatalf("context %+v", c)
		}
	})
	t.Run("unrelated records with incompatible payloads", func(t *testing.T) {
		data := usage("one", 100) + "\n" +
			`{"timestamp":"2026-10-18T12:00:00Z","type":"response_item","payload":{"info":"text","usage":5}}` + "\n" +
			`{"timestamp":"2026-10-18T12:00:00Z","type":"response_item","payload":"just a string"}` + "\n" +
			`{"timestamp":"2026-10-18T12:00:00Z","type":"event_msg","payload":{"type":"agent_message","info":"text","usage":5}}` + "\n" +
			`{"timestamp":"2026-10-18T12:00:00Z","type":"event_msg","payload":"not an object"}` + "\n" +
			usage("two", 200) + "\n" +
			`{"timestamp":"2026-10-18T12:00:00Z","type":"turn_context","payload":{}}` + "\n"
		c := readCodexContext(strings.NewReader(data), "session", start)
		if c.Status != model.ContextAvailable || !equalContextInt(c.LastRequestInput, contextInt(200)) {
			t.Fatalf("context %+v", c)
		}
	})
	t.Run("used record types with undecodable payloads still fail", func(t *testing.T) {
		for _, bad := range []string{
			`{"timestamp":"2026-10-18T12:00:00Z","type":"token_usage_record","payload":{"thread_id":"session","usage":"x"}}`,
			`{"timestamp":"2026-10-18T12:00:00Z","type":"compacted","payload":{"compaction_response_id":5}}`,
			`{"timestamp":"2026-10-18T12:00:00Z","type":"event_msg","payload":{"type":"token_count","info":"text"}}`,
		} {
			c := readCodexContext(strings.NewReader(usage("one", 100)+"\n"+bad+"\n"+usage("two", 200)+"\n"), "session", start)
			if c.Status != model.ContextUnavailable || c.Reason != model.UnavailableParseFailure {
				t.Fatalf("%s: context %+v", bad, c)
			}
		}
	})
	t.Run("final line without trailing newline", func(t *testing.T) {
		tail := `{"timestamp":"2026-10-18T12:00:00Z","type":"turn_context","payload":{}}`
		// The unterminated final line is processed (here an unrelated record), so
		// the preceding usage record is not last and stays classified.
		c := readCodexContext(strings.NewReader(usage("one", 100)+"\n"+tail), "session", start)
		if c.Status != model.ContextAvailable || !equalContextInt(c.LastRequestInput, contextInt(100)) {
			t.Fatalf("context %+v", c)
		}
		// A usage record that is itself the unterminated last line is unclassified.
		c = readCodexContext(strings.NewReader(usage("one", 100)+"\n"+usage("two", 800000)), "session", start)
		if c.Status != model.ContextPartial || c.Reason != model.UnavailableContextUnclassifiedUpdate {
			t.Fatalf("context %+v", c)
		}
		// An unterminated, malformed final line is a parse failure.
		c = readCodexContext(strings.NewReader(usage("one", 100)+"\n"+`{"timestamp":`), "session", start)
		if c.Status != model.ContextUnavailable || c.Reason != model.UnavailableParseFailure {
			t.Fatalf("context %+v", c)
		}
	})
}
