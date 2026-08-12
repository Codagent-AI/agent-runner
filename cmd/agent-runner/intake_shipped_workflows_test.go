package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/intakeroute"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// E2E-006: the shipped intake workflow and its handoff consumers.
//
// Every specification delta can be satisfied while the shipped prompts still
// tell the agent to send a field the validator rejects, or tell a consumer to
// open a file it is no longer given. Only the prompts prove otherwise, so they
// are asserted against the real request contract rather than against prose.

func readBuiltin(t *testing.T, ref string) string {
	t.Helper()
	body, err := builtinworkflows.ReadFile(ref)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", ref, err)
	}
	return string(body)
}

func TestShippedIntakePromptTeachesTheRealRouteRequestSchema(t *testing.T) {
	prompt := readBuiltin(t, builtinworkflows.IntakeRef())

	_, rest, ok := strings.Cut(prompt, "```json")
	if !ok {
		t.Fatal("intake prompt has no JSON example; the agent has nothing to copy")
	}
	example, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("intake prompt's JSON example is unterminated")
	}

	// The decoder the runner actually uses, including unknown-field rejection.
	decoder := json.NewDecoder(bytes.NewReader([]byte(example)))
	decoder.DisallowUnknownFields()
	var request intakeroute.Request
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("intake prompt's example request is not accepted by the validator: %v\nexample:\n%s", err, example)
	}
	if request.Workflow == "" {
		t.Fatal("intake prompt's example omits the required workflow field")
	}
}

func TestShippedIntakePromptDoesNotAskForARemovedHandoffField(t *testing.T) {
	prompt := readBuiltin(t, builtinworkflows.IntakeRef())

	if strings.Contains(prompt, "`handoff` (required") || strings.Contains(prompt, `"handoff":`) {
		t.Fatal("intake prompt still documents a handoff field in the route request; the validator rejects it")
	}
	// The agent must still be told to write the handoff itself.
	if !strings.Contains(prompt, "AGENT_RUNNER_INTAKE_HANDOFF") {
		t.Fatal("intake prompt no longer tells the agent where to write the handoff")
	}
}

func TestShippedHandoffConsumersReceiveContentsRatherThanAPath(t *testing.T) {
	consumers := []string{
		"builtin:core/define-change-v1.0.yaml",
		"builtin:spec-driven/simple-change-v1.0.yaml",
	}

	for _, ref := range consumers {
		t.Run(ref, func(t *testing.T) {
			body := readBuiltin(t, ref)

			if !strings.Contains(body, "{{intake_handoff}}") {
				t.Fatal("consumer does not reference {{intake_handoff}}, so intake context cannot reach it")
			}
			for _, forbidden := range []string{"handoff file", "read that handoff", "handoff path"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("consumer still directs the agent at a handoff file (%q); the handoff arrives as prompt text", forbidden)
				}
			}
		})
	}
}
