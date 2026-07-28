// Package profilewrite owns the shared four-agent profile writer used by
// native setup and the internal write-profile command.
package profilewrite

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type Request struct {
	TargetPath       string
	LeadCLI          string
	LeadModel        string
	CrosscheckCLI    string
	CrosscheckModel  string
	ImplementorCLI   string
	ImplementorModel string
	TesterCLI        string
	TesterModel      string
}

var managedAgents = []string{"crosscheck", "implementor", "lead", "planner", "reviewer", "tester"}

type Staged interface {
	Commit() error
	Discard() error
}

func Stage(req *Request) (Staged, error) {
	payload, err := render(req)
	if err != nil {
		return nil, err
	}
	return stageAtomic0600(req.TargetPath, payload)
}

func Write(req *Request) error {
	staged, err := Stage(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = staged.Discard()
	}()
	return staged.Commit()
}

func render(req *Request) ([]byte, error) {
	if err := validate(req); err != nil {
		return nil, err
	}

	var doc yaml.Node
	body, err := os.ReadFile(req.TargetPath) // #nosec G304 -- explicit user-selected config path.
	switch {
	case err == nil:
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", req.TargetPath, err)
		}
	case os.IsNotExist(err):
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	default:
		return nil, fmt.Errorf("read %s: %w", req.TargetPath, err)
	}

	if err := Merge(&doc, req); err != nil {
		return nil, err
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("marshal profile config: %w", err)
	}
	return out, nil
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
	current := root
	for i, key := range []string{"profiles", "default", "agents"} {
		path := strings.Join([]string{"profiles", "default", "agents"}[:i+1], ".")
		next, present, err := optionalMapping(current, key, path)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, nil
		}
		current = next
	}
	agents := current
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

	deleteMapping(agents, "planner")
	deleteMapping(agents, "reviewer")
	setMapping(agents, "lead", map[string]string{
		"default_mode": "interactive",
		"cli":          req.LeadCLI,
		"model":        req.LeadModel,
	})
	setMapping(agents, "crosscheck", map[string]string{
		"default_mode": "autonomous",
		"cli":          req.CrosscheckCLI,
		"model":        req.CrosscheckModel,
	})
	setMapping(agents, "implementor", map[string]string{
		"default_mode": "autonomous",
		"cli":          req.ImplementorCLI,
		"model":        req.ImplementorModel,
	})
	setMapping(agents, "tester", map[string]string{
		"default_mode": "autonomous",
		"cli":          req.TesterCLI,
		"model":        req.TesterModel,
	})
	return nil
}

func validate(req *Request) error {
	if req == nil {
		return fmt.Errorf("write-profile payload is nil")
	}
	if req.LeadCLI == "" {
		return fmt.Errorf("write-profile payload missing lead_cli")
	}
	if req.CrosscheckCLI == "" {
		return fmt.Errorf("write-profile payload missing crosscheck_cli")
	}
	if req.ImplementorCLI == "" {
		return fmt.Errorf("write-profile payload missing implementor_cli")
	}
	if req.TesterCLI == "" {
		return fmt.Errorf("write-profile payload missing tester_cli")
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

func optionalMapping(root *yaml.Node, key, path string) (*yaml.Node, bool, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		if root.Content[i+1].Kind != yaml.MappingNode {
			return nil, true, fmt.Errorf("%s must be a mapping", path)
		}
		return root.Content[i+1], true, nil
	}
	return nil, false, nil
}

func deleteMapping(root *yaml.Node, key string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		root.Content = append(root.Content[:i], root.Content[i+2:]...)
		return
	}
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
	staged, err := stageAtomic0600(path, payload)
	if err != nil {
		return err
	}
	defer func() {
		_ = staged.Discard()
	}()
	return staged.Commit()
}

func stageAtomic0600(path string, payload []byte) (Staged, error) {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	switch {
	case err == nil:
		if !info.IsDir() {
			return nil, fmt.Errorf("parent path %s is not a directory", dir)
		}
	case os.IsNotExist(err):
		// #nosec G301 -- the setup spec requires newly-created config dirs to be 0755.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create parent directory %s: %w", dir, err)
		}
		// #nosec G302 -- normalizes only newly-created config directories.
		if err := os.Chmod(dir, 0o755); err != nil {
			return nil, fmt.Errorf("chmod parent directory %s: %w", dir, err)
		}
	default:
		return nil, fmt.Errorf("stat parent directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".agent-runner-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary file in %s: %w", dir, err)
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
		return nil, fmt.Errorf("chmod temporary file %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write temporary file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temporary file %s: %w", tmpName, err)
	}
	cleanup = false
	return &stagedFile{targetPath: path, tempPath: tmpName}, nil
}

type stagedFile struct {
	targetPath string
	tempPath   string
}

func (s *stagedFile) Commit() error {
	if s.tempPath == "" {
		return fmt.Errorf("staged profile write is already finalized")
	}
	if err := os.Rename(s.tempPath, s.targetPath); err != nil {
		return fmt.Errorf("rename temporary file %s to %s: %w", s.tempPath, s.targetPath, err)
	}
	s.tempPath = ""
	return nil
}

func (s *stagedFile) Discard() error {
	if s.tempPath == "" {
		return nil
	}
	err := os.Remove(s.tempPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove temporary file %s: %w", s.tempPath, err)
	}
	s.tempPath = ""
	return nil
}
