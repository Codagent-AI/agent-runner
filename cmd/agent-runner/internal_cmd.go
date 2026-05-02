package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	default:
		_, _ = fmt.Fprintf(stderr, "agent-runner: unknown internal command %q\n", args[0])
		return 1
	}
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
	var doc yaml.Node
	body, err := os.ReadFile(payload.TargetPath) // #nosec G304 -- explicit user-selected config path.
	switch {
	case err == nil:
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", payload.TargetPath, err)
		}
	case os.IsNotExist(err):
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	default:
		return fmt.Errorf("read %s: %w", payload.TargetPath, err)
	}

	if err := mergeProfileAgents(&doc, payload); err != nil {
		return err
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal profile config: %w", err)
	}
	return writeAtomic0600(payload.TargetPath, out)
}

func mergeProfileAgents(doc *yaml.Node, payload *writeProfilePayload) error {
	root, err := documentMapping(doc)
	if err != nil {
		return err
	}
	profiles, err := ensureMapping(root, "profiles", "profiles")
	if err != nil {
		return err
	}
	def, err := ensureMapping(profiles, "default", "profiles.default")
	if err != nil {
		return err
	}
	agents, err := ensureMapping(def, "agents", "profiles.default.agents")
	if err != nil {
		return err
	}

	setMapping(agents, "interactive_base", map[string]string{
		"default_mode": "interactive",
		"cli":          payload.InteractiveCLI,
		"model":        payload.InteractiveModel,
	})
	setMapping(agents, "headless_base", map[string]string{
		"default_mode": "headless",
		"cli":          payload.HeadlessCLI,
		"model":        payload.HeadlessModel,
	})
	setMapping(agents, "planner", map[string]string{"extends": "interactive_base"})
	setMapping(agents, "implementor", map[string]string{"extends": "headless_base"})
	return nil
}

func documentMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("config root must be a mapping")
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root must be a mapping")
	}
	return doc.Content[0], nil
}

func ensureMapping(root *yaml.Node, key, path string) (*yaml.Node, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			if root.Content[i+1].Kind != yaml.MappingNode {
				return nil, fmt.Errorf("%s must be a mapping", path)
			}
			return root.Content[i+1], nil
		}
	}
	value := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, yamlScalar(key), value)
	return value, nil
}

func setMapping(root *yaml.Node, key string, values map[string]string) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, field := range []string{"default_mode", "cli", "model", "extends"} {
		value, ok := values[field]
		if !ok || value == "" {
			continue
		}
		node.Content = append(node.Content, yamlScalar(field), yamlScalar(value))
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = node
			return
		}
	}
	root.Content = append(root.Content, yamlScalar(key), node)
}

func yamlScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func writeAtomic0600(path string, payload []byte) error {
	dir := filepath.Dir(path)
	// #nosec G301 -- onboarding spec requires the parent directory to be 0755.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return fmt.Errorf("chmod parent directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".agent-runner-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary file %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temporary file %s to %s: %w", tmpName, path, err)
	}
	cleanup = false
	return nil
}

func writeSetting(key, value string) error {
	settings, err := usersettings.Load()
	if err != nil {
		return err
	}
	switch key {
	case "onboarding.completed_at":
		settings.Onboarding.CompletedAt = value
	case "onboarding.dismissed":
		settings.Onboarding.Dismissed = value
	default:
		return fmt.Errorf("unsupported setting key %q", key)
	}
	return usersettings.Save(settings)
}
