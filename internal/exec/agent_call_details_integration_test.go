package exec

import (
	"context"
	stdexec "os/exec"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/control"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := stdexec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func initCallGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init")
	gitTest(t, dir, "config", "user.name", "Test")
	gitTest(t, dir, "config", "user.email", "test@example.com")
	gitTest(t, dir, "commit", "--allow-empty", "-m", "initial")
	return dir
}
func TestCallGitDeltaINT004(t *testing.T) {
	for _, tc := range []struct {
		name, body, state string
		count             int
		truncated         bool
	}{
		{"two", "git commit --allow-empty -m first; git commit --allow-empty -m second", "captured", 2, false},
		{"cap", "i=0; while [ $i -lt 55 ]; do git commit --allow-empty -m commit-$i >/dev/null; i=$((i+1)); done", "captured", 50, true},
		{"reset", "git reset --hard HEAD~1", "non_linear", 0, false},
		{"diverged", "git checkout --orphan other; git commit --allow-empty -m other", "non_linear", 1, false},
		{"unchanged", "true", "captured", 0, false},
		{"non-git", "true", "not_git", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.name != "non-git" {
				dir = initCallGit(t)
			}
			if tc.name == "reset" {
				gitTest(t, dir, "commit", "--allow-empty", "-m", "before")
			}
			runner := &scriptCallRunner{script: fakeCallScript(t, tc.body+"\necho done")}
			h := NewAgentCallHandler(testAgentCallOptions(dir, runner, &callTestAdapter{}))
			r := decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x"}`)}))
			if r.Error != nil || r.Details == nil {
				t.Fatalf("%+v", r)
			}
			d := r.Details
			if d.Exit != "exited" || d.ExitCode == nil || *d.ExitCode != 0 || d.Git.State != tc.state || len(d.Git.Commits) != tc.count || d.Git.Truncated != tc.truncated {
				t.Fatalf("%+v", d)
			}
			if tc.name == "two" && d.Git.Commits[0].Subject != "second" {
				t.Fatalf("order: %+v", d.Git)
			}
			if tc.name == "unchanged" && d.Git.StartHead != d.Git.EndHead {
				t.Fatal("HEAD changed")
			}
		})
	}
}
func TestGitFailurePreservesResultINT004(t *testing.T) {
	o := testAgentCallOptions(t.TempDir(), &callTestRunner{result: ProcessResult{Started: true, Stdout: "done"}}, &callTestAdapter{})
	o.Git = func(ctx context.Context, _ string, _ ...string) (string, error) { <-ctx.Done(); return "", ctx.Err() }
	h := NewAgentCallHandler(o)
	start := time.Now()
	r := decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x"}`)}))
	if r.Error != nil || r.Details.Git.State != "unavailable" || time.Since(start) > 6*time.Second {
		t.Fatalf("%+v", r)
	}
}
