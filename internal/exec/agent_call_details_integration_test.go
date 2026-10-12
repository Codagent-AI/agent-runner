package exec

import (
	"context"
	stdexec "os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

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
func TestCallGitPhasesShareOneDeadline(t *testing.T) {
	o := testAgentCallOptions(t.TempDir(), &callTestRunner{result: ProcessResult{Started: true, Stdout: "done"}}, &callTestAdapter{})
	var mu sync.Mutex
	var deadlines []time.Time
	o.Git = func(ctx context.Context, _ string, args ...string) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Errorf("git %v ran without a deadline", args)
		}
		mu.Lock()
		deadlines = append(deadlines, deadline)
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		if args[0] != "rev-parse" {
			return "", nil
		}
		if args[1] == "HEAD" {
			return "abc", nil
		}
		return "true", nil
	}
	h := NewAgentCallHandler(o)
	r := decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x"}`)}))
	if r.Error != nil || r.Details == nil || r.Details.Git.State != "captured" {
		t.Fatalf("%+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	// Start capture runs 2 commands and end collection runs 3; each phase
	// shares one deadline instead of granting every command its own budget.
	want := []time.Time{deadlines[0], deadlines[0], deadlines[2], deadlines[2], deadlines[2]}
	if diff := cmp.Diff(want, deadlines); diff != "" {
		t.Fatalf("git deadlines (-want +got):\n%s", diff)
	}
}
func TestCallCancelAbortsStartGitCapture(t *testing.T) {
	o := testAgentCallOptions(t.TempDir(), &callTestRunner{result: ProcessResult{Started: true, Stdout: "done"}}, &callTestAdapter{})
	o.WaitBudget = 50 * time.Millisecond
	captureStarted := make(chan struct{})
	var once sync.Once
	o.Git = func(ctx context.Context, _ string, _ ...string) (string, error) {
		once.Do(func() { close(captureStarted) })
		<-ctx.Done()
		return "", ctx.Err()
	}
	h := NewAgentCallHandler(o)
	accepted := startAgentCall(t, h, control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x"}`)})
	<-captureStarted
	start := time.Now()
	r := cancelAgentCall(t, h, accepted.CallID)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancel waited %s for pre-launch git capture", elapsed)
	}
	if r.Status != "canceled" {
		t.Fatalf("%+v", r)
	}
}
