package agentcall

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestValidationRejectsInvalidForms(t *testing.T) {
	value := func(s string) *string { return &s }
	tests := []struct {
		name string
		in   Request
		code string
	}{
		{name: "missing prompt", in: Request{Agent: value("implementor")}, code: CodeInvalidRequest},
		{name: "missing target", in: Request{Prompt: "do it"}, code: CodeInvalidTarget},
		{name: "multiple targets", in: Request{Prompt: "do it", Agent: value("implementor"), Session: value("named")}, code: CodeInvalidTarget},
		{name: "blank agent", in: Request{Prompt: "do it", Agent: value(" ")}, code: CodeInvalidTarget},
		{name: "reserved new session", in: Request{Prompt: "do it", Session: value("new")}, code: CodeInvalidSession},
		{name: "reserved resume session", in: Request{Prompt: "do it", Session: value("resume")}, code: CodeInvalidSession},
		{name: "reserved inherit session", in: Request{Prompt: "do it", Session: value("inherit")}, code: CodeInvalidSession},
		{name: "named session cli override", in: Request{Prompt: "do it", Session: value("named"), CLI: value("codex")}, code: CodeInvalidRequest},
		{name: "mode is forbidden", in: Request{Prompt: "do it", Agent: value("implementor"), Mode: value("autonomous")}, code: CodeInvalidRequest},
		{name: "blank model override", in: Request{Prompt: "do it", Agent: value("implementor"), Model: value(" ")}, code: CodeInvalidRequest},
		{name: "blank workdir override", in: Request{Prompt: "do it", Agent: value("implementor"), Workdir: value(" ")}, code: CodeInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Validate(); got == nil || got.Code != tt.code {
				t.Fatalf("Validate() = %#v, want code %q", got, tt.code)
			}
		})
	}
}

func TestCanonicalToolPublishesOnlySupportedFields(t *testing.T) {
	tool := Tool()
	if tool.Name != ToolName || !strings.Contains(tool.Description, "synchronous") || !strings.Contains(tool.Description, "serial") {
		t.Fatalf("Tool() = %#v", tool)
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, field := range []string{`"prompt"`, `"agent"`, `"session"`, `"cli"`, `"model"`, `"workdir"`} {
		if !strings.Contains(schema, field) {
			t.Fatalf("schema %s missing %s", schema, field)
		}
	}
	if strings.Contains(schema, `"mode"`) {
		t.Fatalf("schema unexpectedly publishes mode: %s", schema)
	}
}

func TestResultJSONDoesNotExposeExecutionInternals(t *testing.T) {
	raw, err := json.Marshal(Result{Target: Target{Kind: TargetAgent, Name: "implementor"}, Response: "done"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if got != `{"target":{"kind":"agent","name":"implementor"},"response":"done"}` {
		t.Fatalf("result JSON = %s", got)
	}
	for _, forbidden := range []string{"session_id", "usage", "cost"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("result JSON exposes %q: %s", forbidden, got)
		}
	}
}
