package liverun

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
	iexec "github.com/codagent/agent-runner/internal/exec"
)

type crashTestLog struct{}

func (crashTestLog) Printf(string, ...any) {}
func (crashTestLog) Println(...any)        {}
func (crashTestLog) Errorf(string, ...any) {}

func TestTUIProcessRunnerCrashClassification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs")
	}
	for _, tt := range []struct {
		name, script             string
		missing, cancel, crashed bool
	}{
		{"nonzero", "echo 'Selected model is at capacity' >&2\nexit 1", false, false, true},
		{"signal", "kill -9 $$", false, false, true},
		{"missing", "", true, false, true},
		{"wait delay", "sleep 1 &\nexit 0", false, false, true},
		{"question", "echo 'AskUserQuestion not allowed' >&2\nexit 0", false, false, false},
		{"success", "exit 0", false, false, false},
		{"cancel", "sleep 5", false, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "agent")
			if !tt.missing {
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tt.script+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				time.AfterFunc(100*time.Millisecond, cancel)
			}
			runner := NewCoordinator(nil, dir).TUIProcessRunner(nil)
			result, _ := iexec.InvokeAgent(&iexec.AgentInvocation{Context: ctx, Adapter: &cli.ClaudeAdapter{}, Args: []string{path}, CLI: "claude", InvocationContext: cli.ContextAutonomousHeadless, Prefix: "[agent]", Supervision: iexec.AgentProcessSupervision{TerminationGrace: 150 * time.Millisecond}}, runner, crashTestLog{})
			if result.Crashed != tt.crashed {
				t.Fatalf("crashed=%t want=%t result=%+v", result.Crashed, tt.crashed, result)
			}
			if tt.name == "nonzero" && !strings.Contains(result.Stderr, "Selected model is at capacity") {
				t.Fatalf("stderr=%q", result.Stderr)
			}
		})
	}
}
