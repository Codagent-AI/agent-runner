package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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
