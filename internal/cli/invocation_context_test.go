package cli

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestInvocationContextHelpers(t *testing.T) {
	tests := []struct {
		name        string
		context     InvocationContext
		interactive bool
		autonomous  bool
		headless    bool
	}{
		{name: "interactive", context: ContextInteractive, interactive: true},
		{name: "autonomous headless", context: ContextAutonomousHeadless, autonomous: true, headless: true},
		{name: "autonomous interactive", context: ContextAutonomousInteractive, autonomous: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.context.IsInteractive(); got != tt.interactive {
				t.Fatalf("IsInteractive() = %t, want %t", got, tt.interactive)
			}
			if got := tt.context.IsAutonomous(); got != tt.autonomous {
				t.Fatalf("IsAutonomous() = %t, want %t", got, tt.autonomous)
			}
			if got := tt.context.IsHeadless(); got != tt.headless {
				t.Fatalf("IsHeadless() = %t, want %t", got, tt.headless)
			}
		})
	}
}

func TestAdapterInvocationContexts(t *testing.T) {
	tests := []struct {
		name                   string
		adapter                Adapter
		context                InvocationContext
		input                  BuildArgsInput
		wantPrefix             []string
		wantContains           []string
		wantAbsent             []string
		wantDisallowedToolFlag string
	}{
		{
			name:       "claude autonomous headless grants edits and prints",
			adapter:    &ClaudeAdapter{},
			context:    ContextAutonomousHeadless,
			input:      BuildArgsInput{Prompt: "do it", Model: "sonnet", SessionID: "abc", Resume: true},
			wantPrefix: []string{"claude", "--resume", "abc", "--effort", "high", "--permission-mode", "acceptEdits", "-p"},
			wantAbsent: []string{"--allow-all", "--force", "--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:         "claude autonomous interactive grants edits without print mode",
			adapter:      &ClaudeAdapter{},
			context:      ContextAutonomousInteractive,
			input:        BuildArgsInput{Prompt: "do it", SessionID: "abc"},
			wantContains: []string{"--permission-mode", "acceptEdits"},
			wantAbsent:   []string{"-p", "--allow-all", "--force", "--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:         "codex autonomous headless uses workspace write sandbox",
			adapter:      &CodexAdapter{},
			context:      ContextAutonomousHeadless,
			input:        BuildArgsInput{Prompt: "do it", Model: "o3", SessionID: "thread-123"},
			wantContains: []string{"--sandbox", "workspace-write", "exec", "--json", "-m", "o3"},
			wantAbsent:   []string{"--no-alt-screen", "--dangerously-bypass-approvals-and-sandbox", "--allow-all", "--force", "--dangerously-skip-permissions"},
		},
		{
			name:         "codex autonomous interactive grants sandbox without exec",
			adapter:      &CodexAdapter{},
			context:      ContextAutonomousInteractive,
			input:        BuildArgsInput{Prompt: "do it"},
			wantContains: []string{"--sandbox", "workspace-write", "--no-alt-screen"},
			wantAbsent:   []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:         "copilot autonomous headless grants write only",
			adapter:      &CopilotAdapter{},
			context:      ContextAutonomousHeadless,
			input:        BuildArgsInput{Prompt: "do it"},
			wantContains: []string{"-p", "--allow-tool=write", "--autopilot", "-s"},
			wantAbsent:   []string{"--allow-all", "--force", "--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:         "copilot autonomous interactive uses autopilot without print flags",
			adapter:      &CopilotAdapter{},
			context:      ContextAutonomousInteractive,
			input:        BuildArgsInput{Prompt: "do it"},
			wantContains: []string{"-i", "--allow-tool=write", "--autopilot"},
			wantAbsent:   []string{"-p", "-s", "--allow-all"},
		},
		{
			name:         "cursor autonomous headless trusts without force",
			adapter:      &CursorAdapter{},
			context:      ContextAutonomousHeadless,
			input:        BuildArgsInput{Prompt: "do it"},
			wantContains: []string{"-p", "--output-format", "stream-json", "--trust"},
			wantAbsent:   []string{"--force", "--allow-all", "--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:         "opencode autonomous headless omits dangerous skip permissions",
			adapter:      &OpenCodeAdapter{},
			context:      ContextAutonomousHeadless,
			input:        BuildArgsInput{Prompt: "do it"},
			wantContains: []string{"run", "--format", "json"},
			wantAbsent:   []string{"--dangerously-skip-permissions", "--allow-all", "--force", "--dangerously-bypass-approvals-and-sandbox"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.Context = tt.context
			tt.input.Effort = "high"
			args := tt.adapter.BuildArgs(&tt.input)
			if len(tt.wantPrefix) > 0 {
				if diff := cmp.Diff(tt.wantPrefix, args[:min(len(args), len(tt.wantPrefix))]); diff != "" {
					t.Fatalf("args prefix mismatch (-want +got):\n%s\nfull args: %v", diff, args)
				}
			}
			for _, want := range tt.wantContains {
				if !slices.Contains(args, want) {
					t.Fatalf("expected %q in args %v", want, args)
				}
			}
			for _, absent := range tt.wantAbsent {
				if slices.Contains(args, absent) {
					t.Fatalf("did not expect %q in args %v", absent, args)
				}
			}
		})
	}
}
