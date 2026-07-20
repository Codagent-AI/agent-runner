package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/codagent/agent-runner/internal/control"
)

type controlEventSender func(context.Context, string, func(string) string) (string, error)

var sendControlEvent controlEventSender = control.SendControlEventFromEnvironment

func handleStep(args []string) int {
	return handleStepWithIO(args, os.Stdout, os.Stderr)
}

func handleStepWithIO(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: missing step command")
		return 1
	}
	if args[0] != "complete" {
		_, _ = fmt.Fprintf(stderr, "agent-runner: unknown step command %q\n", args[0])
		return 1
	}
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: step complete accepts no arguments")
		return 1
	}
	receipt, err := sendControlMessage(control.MessageCompleteStep)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "agent-runner: step completion requested")
	if receipt != "" {
		// The receipt line lands in the CLI's recorded tool output; durability
		// probes without a terminal committed-turn marker (Cursor) search the
		// native store for this exact receipt as committed-turn evidence.
		_, _ = fmt.Fprintln(stdout, "agent-runner completion receipt "+receipt)
	}
	return 0
}

func sendControlMessage(messageType string) (string, error) {
	return sendControlEvent(context.Background(), messageType, os.Getenv)
}
