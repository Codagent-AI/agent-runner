package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestExternalUserWholeTurn(t *testing.T) {
	stdout := `{"type":"assistant","message":{"content":[{"type":"text","text":"scope"}]},"parent_tool_use_id":null}` + "\n" + `{"type":"assistant","message":{"content":[{"type":"text","text":"hidden"}]},"parent_tool_use_id":"child"}` + "\n" + `{"type":"assistant","message":{"content":[{"type":"tool_use"},{"type":"text","text":"question"}]}}` + "\n" + `{"type":"result","result":"question"}`
	if got := ExternalUserTurnText(&ClaudeAdapter{}, stdout); got != "scope\n\nquestion" {
		t.Fatal(got)
	}
}
func TestExternalUserAdapterArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, adapter := range []Adapter{&ClaudeAdapter{}, &CodexAdapter{}} {
		for _, session := range []string{"", "session"} {
			input := &BuildArgsInput{Context: ContextExternalUser, SessionID: session, Resume: session != "", Prompt: "- option a", DisallowedTools: []string{"AskUserQuestion"}, CompletionCommand: &CompletionCommand{Executable: "/runner", Args: []string{"step", "complete"}}}
			args, err := BuildInvocationArgs(adapter, input)
			if err != nil {
				t.Fatal(err)
			}
			if args[len(args)-1] != "- option a" {
				t.Fatal(args)
			}
			position := slices.Index(args, "--")
			if position < 0 || (session != "" && args[0] == "codex" && args[position+1] != session) {
				t.Fatal(args)
			}
			joined := strings.Join(args, " ")
			if args[0] == "claude" && (!strings.Contains(joined, "--permission-mode acceptEdits") || !strings.Contains(joined, "AskUserQuestion") || !strings.Contains(joined, "--plugin-dir")) {
				t.Fatal(args)
			}
			if args[0] == "codex" && (!strings.Contains(joined, "--sandbox workspace-write") || !strings.Contains(joined, "notify=")) {
				t.Fatal(args)
			}
		}
	}
}

func TestExternalUserAgentCallPreapproval(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			adapter, input := agentCallTestInput(t, name, ContextExternalUser)
			prepared, err := prepareAgentCallTestInvocation(adapter, input)
			if err != nil {
				t.Fatal(err)
			}
			assertAgentCallApproval(t, name, ContextAutonomousHeadless, prepared)
		})
	}
}
