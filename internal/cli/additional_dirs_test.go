package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestAdditionalDirectories(t *testing.T) {
	for _, adapter := range []Adapter{&ClaudeAdapter{}, &CodexAdapter{}, &CopilotAdapter{}, &OpenCodeAdapter{}, &CursorAdapter{}} {
		input := &BuildArgsInput{Prompt: "test", Context: ContextAutonomousHeadless, AdditionalDirs: []string{"/work/specs"}}
		args, err := BuildInvocationArgs(adapter, input)
		if _, ok := adapter.(*CursorAdapter); ok {
			if err == nil || !strings.Contains(err.Error(), "/work/specs") {
				t.Fatal(args, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := adapter.(*OpenCodeAdapter); ok {
			if slices.Contains(args, "--add-dir") {
				t.Fatal(args)
			}
			continue
		}
		index := slices.Index(args, "--add-dir")
		if index < 0 || args[index+1] != "/work/specs" {
			t.Fatal(args)
		}
		if _, ok := adapter.(*CodexAdapter); ok && index > slices.Index(args, "exec") {
			t.Fatal(args)
		}
	}
}
