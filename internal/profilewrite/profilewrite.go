// Package profilewrite owns the shared four-agent profile writer used by
// native setup and the internal write-profile command.
package profilewrite

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

type Request struct {
	InteractiveCLI   string
	InteractiveModel string
	HeadlessCLI      string
	HeadlessModel    string
	TargetPath       string
}

var managedAgents = []string{"headless_base", "implementor", "interactive_base", "planner"}

func Write(req *Request) error {
	if err := validate(req); err != nil {
		return err
	}

	var doc yaml.Node
	body, err := os.ReadFile(req.TargetPath) // #nosec G304 -- explicit user-selected config path.
	switch {
	case err == nil:
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", req.TargetPath, err)
		}
	case os.IsNotExist(err):
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	default:
		return fmt.Errorf("read %s: %w", req.TargetPath, err)
	}

	if err := Merge(&doc, req); err != nil {
		return err
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal profile config: %w", err)
	}
	return writeAtomic0600(req.TargetPath, out)
}

func Collisions(path string) ([]string, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- explicit config path selected by setup.
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	root, err := documentMapping(&doc)
	if err != nil {
		return nil, err
	}
	agents := mappingAt(root, "profiles", "default", "agents")
	if agents == nil {
		return nil, nil
	}
	var collisions []string
	for i := 0; i+1 < len(agents.Content); i += 2 {
		if slices.Contains(managedAgents, agents.Content[i].Value) {
			collisions = append(collisions, agents.Content[i].Value)
		}
	}
	slices.Sort(collisions)
	return collisions, nil
}

func Merge(doc *yaml.Node, req *Request) error {
	if err := validate(req); err != nil {
		return err
	}
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
		"cli":          req.InteractiveCLI,
		"model":        req.InteractiveModel,
	})
	setMapping(agents, "headless_base", map[string]string{
		"default_mode": "headless",
		"cli":          req.HeadlessCLI,
		"model":        req.HeadlessModel,
	})
	setMapping(agents, "planner", map[string]string{"extends": "interactive_base"})
	setMapping(agents, "implementor", map[string]string{"extends": "headless_base"})
	return nil
}

func validate(req *Request) error {
	if req == nil {
		return fmt.Errorf("write-profile payload is nil")
	}
	if req.InteractiveCLI == "" {
		return fmt.Errorf("write-profile payload missing interactive_cli")
	}
	if req.HeadlessCLI == "" {
		return fmt.Errorf("write-profile payload missing headless_cli")
	}
	if req.TargetPath == "" {
		return fmt.Errorf("write-profile payload missing target_path")
	}
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

func mappingAt(root *yaml.Node, path ...string) *yaml.Node {
	current := root
	for _, key := range path {
		var next *yaml.Node
		for i := 0; i+1 < len(current.Content); i += 2 {
			if current.Content[i].Value == key && current.Content[i+1].Kind == yaml.MappingNode {
				next = current.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
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
	info, err := os.Stat(dir)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("parent path %s is not a directory", dir)
		}
	case os.IsNotExist(err):
		// #nosec G301 -- the setup spec requires newly-created config dirs to be 0755.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create parent directory %s: %w", dir, err)
		}
		// #nosec G302 -- normalizes only newly-created config directories.
		if err := os.Chmod(dir, 0o755); err != nil {
			return fmt.Errorf("chmod parent directory %s: %w", dir, err)
		}
	default:
		return fmt.Errorf("stat parent directory %s: %w", dir, err)
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
