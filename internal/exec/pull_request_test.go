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

func TestRecordPullRequestCaptureDeduplicatesAcrossNestedContexts(t *testing.T) {
	root := makeCtx()
	recorder := &mockAuditLogger{}
	root.AuditLogger = recorder
	child := model.NewSubWorkflowContext(root, &model.SubWorkflowContextOptions{
		StepID:          "implement",
		Params:          map[string]string{},
		WorkflowFile:    "child.yaml",
		SubWorkflowName: "child",
	})
	url := model.NewCapturedString("https://github.com/Codagent-AI/agent-runner/pull/62")

	recordPullRequestCapture(root, "root-step", "pr_url", url)
	recordPullRequestCapture(child, "child-step", "pr_url", url)

	var recorded int
	for _, event := range recorder.events {
		if event.Type == "pull_request_recorded" {
			recorded++
		}
	}
	if recorded != 1 {
		t.Fatalf("pull_request_recorded events = %d, want 1", recorded)
	}
}

func TestRecordPullRequestCaptureSeparatesRepositoryScopes(t *testing.T) {
	workspace := makeCtx()
	recorder := &mockAuditLogger{}
	workspace.AuditLogger = recorder
	url := model.NewCapturedString("https://github.com/Codagent-AI/agent-runner/pull/62")
	backend := model.NewRepositoryExecutionContext(workspace, model.Repository{Name: "backend", Dir: "/repos/backend"}, 0)
	frontend := model.NewRepositoryExecutionContext(workspace, model.Repository{Name: "frontend", Dir: "/repos/frontend"}, 1)

	recordPullRequestCapture(backend, "backend-pr", "pr_url", url)
	recordPullRequestCapture(frontend, "frontend-pr", "pr_url", url)

	var recorded int
	for _, event := range recorder.events {
		if event.Type == "pull_request_recorded" {
			recorded++
		}
	}
	if recorded != 2 {
		t.Fatalf("pull_request_recorded events = %d, want one per repository", recorded)
	}
}
