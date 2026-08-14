package exec

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/model"
)

func TestExecuteShellStepRecordsPullRequestCapture(t *testing.T) {
	ctx := makeCtx()
	recorder := &mockAuditLogger{}
	ctx.AuditLogger = recorder
	step := model.Step{ID: "record-pr", Command: "gh pr view", Capture: "pr_url"}

	if outcome, err := ExecuteShellStep(&step, ctx, &mockRunner{results: []ProcessResult{{Stdout: " https://github.com/Codagent-AI/agent-runner/pull/62\n", ExitCode: 0}}}, &mockLogger{}); err != nil || outcome != OutcomeSuccess {
		t.Fatalf("ExecuteShellStep() = (%q, %v), want (success, nil)", outcome, err)
	}

	got := make([]map[string]any, 0, len(recorder.events))
	for _, event := range recorder.events {
		if event.Type != "pull_request_recorded" {
			continue
		}
		got = append(got, event.Data)
	}
	want := []map[string]any{{"url": "https://github.com/Codagent-AI/agent-runner/pull/62"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("recorded events (-want +got):\n%s", diff)
	}
}
