package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/codagent/agent-runner/internal/profilewrite"
	"github.com/codagent/agent-runner/internal/usersettings"
	"gopkg.in/yaml.v3"
)

type writeProfilePayload struct {
	InteractiveCLI   string `json:"interactive_cli"`
	InteractiveModel string `json:"interactive_model"`
	HeadlessCLI      string `json:"headless_cli"`
	HeadlessModel    string `json:"headless_model"`
	TargetPath       string `json:"target_path"`
}

func handleInternal(args []string) int {
	return handleInternalWithIO(args, os.Stdin, os.Stderr)
}

func handleInternalWithIO(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: missing internal command")
		return 1
	}
	switch args[0] {
	case "write-profile":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal write-profile accepts no arguments")
			return 1
		}
		var payload writeProfilePayload
		if err := decodeWriteProfilePayload(stdin, &payload); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		if err := writeProfile(&payload); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		return 0
	case "write-setting":
		if len(args) != 3 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal write-setting requires key and value")
			return 1
		}
		if err := writeSetting(args[1], args[2]); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		return 0
	case "json-value":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal json-value requires key")
			return 1
		}
		value, err := decodeJSONStringField(stdin, args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, value)
		return 0
	case "json-list-count":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal json-list-count requires key")
			return 1
		}
		values, err := decodeJSONStringListField(stdin, args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, len(values))
		return 0
	case "json-list-join":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal json-list-join requires key")
			return 1
		}
		values, err := decodeJSONStringListField(stdin, args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, strings.Join(values, ", "))
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "agent-runner: unknown internal command %q\n", args[0])
		return 1
	}
}

func decodeJSONStringField(r io.Reader, key string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&fields); err != nil {
		return "", fmt.Errorf("decode json input: %w", err)
	}
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("json input missing %s", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("json field %s must be a string: %w", key, err)
	}
	return value, nil
}

func decodeJSONStringListField(r io.Reader, key string) ([]string, error) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&fields); err != nil {
		return nil, fmt.Errorf("decode json input: %w", err)
	}
	raw, ok := fields[key]
	if !ok {
		return nil, fmt.Errorf("json input missing %s", key)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("json field %s must be an array of strings: %w", key, err)
	}
	return values, nil
}

func decodeWriteProfilePayload(r io.Reader, payload *writeProfilePayload) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		return fmt.Errorf("decode write-profile payload: %w", err)
	}
	if payload.InteractiveCLI == "" {
		return fmt.Errorf("write-profile payload missing interactive_cli")
	}
	if payload.HeadlessCLI == "" {
		return fmt.Errorf("write-profile payload missing headless_cli")
	}
	if payload.TargetPath == "" {
		return fmt.Errorf("write-profile payload missing target_path")
	}
	return nil
}

func writeProfile(payload *writeProfilePayload) error {
	return profilewrite.Write(&profilewrite.Request{
		InteractiveCLI:   payload.InteractiveCLI,
		InteractiveModel: payload.InteractiveModel,
		HeadlessCLI:      payload.HeadlessCLI,
		HeadlessModel:    payload.HeadlessModel,
		TargetPath:       payload.TargetPath,
	})
}

func mergeProfileAgents(doc *yaml.Node, payload *writeProfilePayload) error {
	return profilewrite.Merge(doc, &profilewrite.Request{
		InteractiveCLI:   payload.InteractiveCLI,
		InteractiveModel: payload.InteractiveModel,
		HeadlessCLI:      payload.HeadlessCLI,
		HeadlessModel:    payload.HeadlessModel,
		TargetPath:       payload.TargetPath,
	})
}

func writeSetting(key, value string) error {
	settings, err := usersettings.Load()
	if err != nil {
		return err
	}
	switch key {
	case "setup.completed_at":
		settings.Setup.CompletedAt = value
	case "onboarding.completed_at":
		settings.Onboarding.CompletedAt = value
	case "onboarding.dismissed":
		settings.Onboarding.Dismissed = value
	default:
		return fmt.Errorf("unsupported setting key %q", key)
	}
	return usersettings.Save(settings)
}
