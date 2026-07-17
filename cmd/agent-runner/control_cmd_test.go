package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/interactive"
)

func TestStepCompleteCommandSendsCompletionEvent(t *testing.T) {
	replaceControlSender(t, func(_ context.Context, messageType string, _ func(string) string) (string, error) {
		if messageType != interactive.MessageCompleteStep {
			t.Fatalf("message type = %q", messageType)
		}
		return "receipt-123", nil
	})
	var stdout, stderr bytes.Buffer
	if code := handleStepWithIO([]string{"complete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "completion requested") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "agent-runner completion receipt receipt-123") {
		t.Fatalf("stdout = %q, want the acknowledged receipt line", stdout.String())
	}
}

func TestStepCompleteCommandOmitsReceiptLineWhenServerSendsNone(t *testing.T) {
	replaceControlSender(t, func(context.Context, string, func(string) string) (string, error) {
		return "", nil
	})
	var stdout, stderr bytes.Buffer
	if code := handleStepWithIO([]string{"complete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "completion receipt") {
		t.Fatalf("stdout = %q, want no receipt line for an empty receipt", stdout.String())
	}
}

func TestStepCompleteCommandOutsideSessionExplainsScope(t *testing.T) {
	for _, key := range []string{interactive.EnvControlSocket, interactive.EnvRunID, interactive.EnvStepID, interactive.EnvControlToken} {
		t.Setenv(key, "")
	}
	var stdout, stderr bytes.Buffer
	if code := handleStepWithIO([]string{"complete"}, &stdout, &stderr); code == 0 {
		t.Fatal("command unexpectedly succeeded")
	}
	if !strings.Contains(stderr.String(), "inside an interactive agent step session") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestStepCommandRejectsCrossTerminalArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := handleStepWithIO([]string{"complete", "--run", "another"}, &stdout, &stderr); code == 0 {
		t.Fatal("command unexpectedly accepted targeting arguments")
	}
	if !strings.Contains(stderr.String(), "accepts no arguments") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInternalTurnCommittedSendsHookEvent(t *testing.T) {
	replaceControlSender(t, func(_ context.Context, messageType string, _ func(string) string) (string, error) {
		if messageType != interactive.MessageTurnCommitted {
			t.Fatalf("message type = %q", messageType)
		}
		return "", nil
	})
	var stderr bytes.Buffer
	if code := handleInternalWithIO([]string{"turn-committed"}, strings.NewReader(""), &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	stderr.Reset()
	if code := handleInternalWithIO([]string{"turn-committed", `{"type":"agent-turn-complete"}`}, strings.NewReader(""), &stderr); code != 0 {
		t.Fatalf("Codex notify payload code = %d, stderr = %s", code, stderr.String())
	}
}

func replaceControlSender(t *testing.T, sender controlEventSender) {
	t.Helper()
	previous := sendControlEvent
	sendControlEvent = sender
	t.Cleanup(func() { sendControlEvent = previous })
}
