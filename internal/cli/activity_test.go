package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestActivityStreamsINT005(t *testing.T) {
	cases := map[string][]string{
		"claude": {"session started", "thinking", "tool_use: Bash", "tool_result", "subagent assistant message", "turn finished"},
		"codex":  {"turn.started", "item.started: command_execution", "item.completed: command_execution", "item.started: mcp_tool_call runner/call_agent", "item.completed: agent_message", "turn.completed"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := Get(name)
			s := a.(HeadlessActivitySummarizer)
			data, err := os.ReadFile("testdata/activity/" + name + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, line := range strings.Split(string(data), "\n") {
				summary, ok := s.SummarizeActivity([]byte(line))
				if ok {
					if strings.Contains(summary, "SECRET") || strings.ContainsAny(summary, "\n\r") || len([]rune(summary)) > 200 {
						t.Fatal(summary)
					}
					got = append(got, summary)
				}
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatal(diff)
			}
		})
	}
	for _, name := range []string{"copilot", "cursor", "opencode"} {
		a, _ := Get(name)
		if _, ok := a.(HeadlessActivitySummarizer); ok {
			t.Fatalf("unexpected summarizer %s", name)
		}
	}
}
func TestResumeModelDeclarationMatchesArgs(t *testing.T) {
	for _, name := range KnownCLIs() {
		t.Run(name, func(t *testing.T) {
			a, _ := Get(name)
			args, err := BuildInvocationArgs(a, &BuildArgsInput{Prompt: "x", SessionID: "native", Resume: true, Model: "test-model", Context: ContextAutonomousHeadless, Workdir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			v, ok := a.(ResumeModelApplier)
			declared := ok && v.AppliesResumeModel()
			if slices.Contains(args, "test-model") != declared {
				t.Fatalf("%s declared %v: %v", name, declared, args)
			}
		})
	}
}
