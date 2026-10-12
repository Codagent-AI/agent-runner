package exec

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestCaptureStartGitReportsInterruptedProbeAsUnavailable(t *testing.T) {
	exitErr := exec.Command("false").Run()
	var exit *exec.ExitError
	if !errors.As(exitErr, &exit) {
		t.Fatalf("want *exec.ExitError from false, got %v", exitErr)
	}
	options := testAgentCallOptions(t.TempDir(), &scriptCallRunner{}, &callTestAdapter{})
	options.Git = func(ctx context.Context, _ string, _ ...string) (string, error) {
		<-ctx.Done()
		return "", exitErr
	}
	h := NewAgentCallHandler(options)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := h.captureStartGit(ctx, t.TempDir()).State; got != "unavailable" {
		t.Fatalf("state = %q, want unavailable", got)
	}
}
