package exec

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
)

func TestFollowUpSubprocessINT003(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			root, _ = filepath.EvalSymlinks(root)
			api, web := filepath.Join(root, "services", "api"), filepath.Join(root, "services", "web")
			for _, dir := range []string{api, web} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			a, _ := cli.Get(name)
			body := `printf '%s\n' "$@" > args; pwd > cwd; printf '%s\n' '{"type":"thread.started","thread_id":"native"}' '{"type":"result","result":"done","session_id":"native"}'`
			runner := &scriptCallRunner{script: fakeCallScript(t, body)}
			o := testAgentCallOptions(root, runner, a)
			o.Context.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{"implementor": {CLI: name, Model: "m1"}}}
			o.Adapter = func(string) (cli.Adapter, error) { return a, nil }
			h := NewAgentCallHandler(o)
			call := func(id, payload string) agentcall.Response {
				return decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: id, Payload: []byte(payload)}))
			}
			first := call("one", `{"agent":"implementor","prompt":"x","workdir":"services/api"}`)
			if first.Error != nil {
				t.Fatal(first.Error)
			}
			h.mu.Lock()
			native := h.byCallID[first.CallID].nativeSessionID
			h.mu.Unlock()
			if native == "" {
				t.Fatal("missing native session")
			}
			if name == "claude" {
				encoded := regexp.MustCompile(`[/._]`).ReplaceAllString(api, "-")
				dir := filepath.Join(home, ".claude", "projects", encoded)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, native+".jsonl"), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			second := call("two", `{"follow_up":"`+first.CallID+`","prompt":"fix"}`)
			if second.Error != nil || second.Details.Session.Model != "m1" || !second.Details.Session.Resumed {
				t.Fatalf("follow-up: %+v", second)
			}
			args, _ := os.ReadFile(filepath.Join(api, "args"))
			cwd, _ := os.ReadFile(filepath.Join(api, "cwd"))
			if !strings.Contains(string(args), native) || strings.TrimSpace(string(cwd)) != api {
				t.Fatalf("args=%s cwd=%s", args, cwd)
			}
			if name == "claude" && strings.Contains(string(args), "--model") {
				t.Fatalf("resume passes model: %s", args)
			}
			if name == "codex" && !strings.Contains(string(args), "m1") {
				t.Fatal("Codex omitted resume model")
			}
			override := call("three", `{"follow_up":"`+first.CallID+`","prompt":"fix","model":"m2"}`)
			if name == "claude" {
				if override.CallID != "" || override.Error == nil || override.Error.Code != agentcall.CodeInvalidModel {
					t.Fatalf("%+v", override)
				}
			} else if override.Error != nil || override.Details.Session.Model != "m2" {
				t.Fatalf("%+v", override)
			}
			third := call("four", `{"follow_up":"`+second.CallID+`","prompt":"fix","workdir":"services/web"}`)
			if name == "claude" {
				if third.CallID == "" || third.Error == nil || third.Error.Code != agentcall.CodeNotResumable || third.Details.Exit != "not_launched" {
					t.Fatalf("%+v", third)
				}
				if _, err := os.Stat(filepath.Join(web, "args")); !os.IsNotExist(err) {
					t.Fatal("missing session launched")
				}
			} else if third.Error != nil {
				t.Fatal(third.Error)
			}
			if len(o.Context.NamedSessions) != 0 {
				t.Fatal("follow-up wrote named session")
			}
			if name == "claude" {
				o.Context.NamedSessions["named"] = native
				named := call("five", `{"session":"named","prompt":"x","model":"m2","workdir":"services/api"}`)
				if named.Error != nil || named.Details.Session.Model != "" {
					t.Fatalf("unapplied named-session model: %+v", named)
				}
			}
		})
	}
}
