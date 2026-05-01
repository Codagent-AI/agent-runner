// Package config loads, validates, and resolves agent configurations from
// project-local and optional global config files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Agent represents a named agent as declared in config YAML.
// All fields are optional; an agent without Extends must supply at least
// DefaultMode and CLI.
type Agent struct {
	DefaultMode  string `yaml:"default_mode,omitempty"`
	CLI          string `yaml:"cli,omitempty"`
	Model        string `yaml:"model,omitempty"`
	Effort       string `yaml:"effort,omitempty"`
	SystemPrompt string `yaml:"system_prompt,omitempty"`
	Extends      string `yaml:"extends,omitempty"`
}

// ResolvedAgent is a fully-merged agent after walking the extends chain.
// DefaultMode and CLI are guaranteed populated. Model, Effort, and
// SystemPrompt may be empty (meaning "not set").
type ResolvedAgent struct {
	DefaultMode  string
	CLI          string
	Model        string
	Effort       string
	SystemPrompt string
}

// ProfileSet is a named collection of agents, optionally inheriting from
// another profile set.
type ProfileSet struct {
	Extends string            `yaml:"extends,omitempty"`
	Agents  map[string]*Agent `yaml:"agents"`
}

// Config is the top-level configuration after loading, merging, and active-profile
// selection. ActiveAgents holds the agents for the selected profile set.
type Config struct {
	ActiveProfile string
	Profiles      map[string]*ProfileSet
	ActiveAgents  map[string]*Agent
}

// parsedFile is the internal representation of one loaded config file.
type parsedFile struct {
	ActiveProfile string
	Profiles      map[string]*ProfileSet
}

// legacyAgentKeys are direct fields of the old flat agent bundle shape.
// Their presence at the profile-set level indicates a legacy config.
var legacyAgentKeys = map[string]bool{
	"default_mode":  true,
	"cli":           true,
	"model":         true,
	"effort":        true,
	"system_prompt": true,
}

var validEffort = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}
var validDefaultMode = map[string]bool{"interactive": true, "headless": true}
var validCLI = map[string]bool{"claude": true, "codex": true, "copilot": true, "cursor": true, "opencode": true}

var userHomeDir = os.UserHomeDir

// Load returns a configuration by layering three sources: built-in defaults,
// an optional global config at ~/.agent-runner/config.yaml, and an optional
// project config at path. Missing files are treated as empty layers; Load
// SHALL NOT create either file on disk. Layers are applied
// defaults → global → project, so global can override defaults and project
// can override both. Within a profile set, an agent whose name appears in a
// higher layer replaces the lower-layer agent wholesale (no field-level
// merge). Legacy flat shapes are rejected with an actionable error.
func Load(path string) (*Config, error) {
	var globalFile *parsedFile
	if globalPath, err := globalConfigPath(); err == nil {
		gf, gErr := loadFileOptional(globalPath, true)
		if gErr != nil {
			return nil, fmt.Errorf("loading global config %s: %w", globalPath, gErr)
		}
		globalFile = gf
	}

	projectFile, err := loadFileOptional(path, false)
	if err != nil {
		return nil, fmt.Errorf("loading project config %s: %w", path, err)
	}

	cfg, err := buildConfig(defaultParsedFile(), globalFile, projectFile)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func globalConfigPath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for global config: %w", err)
	}
	return filepath.Join(home, ".agent-runner", "config.yaml"), nil
}

func loadFileOptional(path string, isGlobal bool) (*parsedFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- config path is from internal caller
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return parseConfigFile(data, path, isGlobal)
}

func parseConfigFile(data []byte, path string, isGlobal bool) (*parsedFile, error) {
	// First pass: detect legacy flat shape before any typed unmarshaling.
	if err := checkLegacyShape(data, path); err != nil {
		return nil, err
	}

	// Extract active_profile and enforce project-only rule.
	var header struct {
		ActiveProfile string `yaml:"active_profile"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if isGlobal && header.ActiveProfile != "" {
		return nil, fmt.Errorf("active_profile is not allowed in the global config")
	}

	// Second pass: parse into typed struct.
	var raw struct {
		Profiles map[string]*ProfileSet `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Normalize nil ProfileSets (e.g. `profiles:\n  name:\n`).
	for name, ps := range raw.Profiles {
		if ps == nil {
			raw.Profiles[name] = &ProfileSet{}
		}
	}

	return &parsedFile{
		ActiveProfile: header.ActiveProfile,
		Profiles:      raw.Profiles,
	}, nil
}

// checkLegacyShape inspects each profile-set value for legacy agent-bundle
// fields. If found, it returns an error instructing the user to restructure.
func checkLegacyShape(data []byte, path string) error {
	var raw struct {
		Profiles map[string]interface{} `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	for name, value := range raw.Profiles {
		if value == nil {
			continue
		}
		valueMap, ok := value.(map[string]interface{})
		if !ok {
			continue // not a mapping; typed parse will error later
		}
		if err := validateProfileSetShape(path, name, valueMap); err != nil {
			return err
		}
		if isLegacyAgentMap(valueMap) {
			return fmt.Errorf(
				"%s: entry %q looks like a legacy agent bundle; "+
					"restructure it under profiles.<set_name>.agents.%s",
				path, name, name,
			)
		}
	}
	return nil
}

func validateProfileSetShape(path, name string, m map[string]interface{}) error {
	if extends, ok := m["extends"]; ok && extends != nil {
		if _, ok := extends.(string); !ok {
			return fmt.Errorf("%s: profile set %q: extends must be a single profile set name", path, name)
		}
	}
	return nil
}

// isLegacyAgentMap returns true when a profile-set value map has legacy
// agent-bundle fields (default_mode, cli, model, etc.) but no "agents" key.
func isLegacyAgentMap(m map[string]interface{}) bool {
	if _, hasAgents := m["agents"]; hasAgents {
		return false
	}
	for key := range m {
		if legacyAgentKeys[key] {
			return true
		}
	}
	return false
}

// buildConfig layers the given parsed files (defaults → global → project),
// selects the active profile set from the project layer, and returns a
// fully-populated Config. nil layers are skipped. active_profile is read
// only from the last layer (project).
func buildConfig(defaults, global, project *parsedFile) (*Config, error) {
	merged := mergeProfileSets(defaults, global, project)
	resolved, err := resolveProfileSetExtends(merged)
	if err != nil {
		return nil, err
	}

	var activeProfile string
	if project != nil {
		activeProfile = project.ActiveProfile
	}

	selectedName := activeProfile
	if selectedName == "" {
		selectedName = "default"
	}

	selectedSet, ok := resolved[selectedName]
	if !ok {
		if activeProfile != "" {
			return nil, fmt.Errorf("active_profile %q does not exist in the merged config", selectedName)
		}
		return nil, fmt.Errorf("no profile set named %q found; define a 'default' profile set or set active_profile explicitly", selectedName)
	}

	activeAgents := selectedSet.Agents
	if activeAgents == nil {
		activeAgents = map[string]*Agent{}
	}

	return &Config{
		ActiveProfile: activeProfile,
		Profiles:      resolved,
		ActiveAgents:  activeAgents,
	}, nil
}

// mergeProfileSets layers parsed files. For each profile set name, agents from
// later layers override agents of the same name from earlier layers. Profile
// set names appearing in only one layer pass through.
func mergeProfileSets(layers ...*parsedFile) map[string]*ProfileSet {
	result := map[string]*ProfileSet{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		for name, ps := range layer.Profiles {
			existing, ok := result[name]
			if !ok {
				existing = &ProfileSet{Agents: map[string]*Agent{}}
				result[name] = existing
			}
			if ps == nil {
				continue
			}
			if ps.Extends != "" {
				existing.Extends = ps.Extends
			}
			for agentName, agent := range ps.Agents {
				existing.Agents[agentName] = agent
			}
		}
	}
	return result
}

func resolveProfileSetExtends(profileSets map[string]*ProfileSet) (map[string]*ProfileSet, error) {
	resolved := make(map[string]*ProfileSet, len(profileSets))

	var resolveOne func(name string, path []string) (*ProfileSet, error)
	resolveOne = func(name string, path []string) (*ProfileSet, error) {
		if ps, ok := resolved[name]; ok {
			return ps, nil
		}

		if cycleStart := pathIndex(path, name); cycleStart >= 0 {
			cycle := append(append([]string{}, path[cycleStart:]...), name)
			return nil, fmt.Errorf("cycle in profile set extends chain: %s", strings.Join(cycle, " -> "))
		}

		ps, ok := profileSets[name]
		if !ok {
			return nil, fmt.Errorf("profile set %q does not exist", name)
		}

		path = append(path, name)
		effectiveAgents := map[string]*Agent{}
		if ps.Extends != "" {
			parent, ok := profileSets[ps.Extends]
			if !ok || parent == nil {
				return nil, fmt.Errorf("profile set %q: extends %q which does not exist", name, ps.Extends)
			}
			parentResolved, err := resolveOne(ps.Extends, path)
			if err != nil {
				return nil, err
			}
			for agentName, agent := range parentResolved.Agents {
				effectiveAgents[agentName] = agent
			}
		}
		for agentName, agent := range ps.Agents {
			effectiveAgents[agentName] = agent
		}

		resolvedPS := &ProfileSet{
			Extends: ps.Extends,
			Agents:  effectiveAgents,
		}
		resolved[name] = resolvedPS
		return resolvedPS, nil
	}

	for name := range profileSets {
		if _, err := resolveOne(name, nil); err != nil {
			return nil, err
		}
	}

	return resolved, nil
}

func pathIndex(path []string, target string) int {
	for i, name := range path {
		if name == target {
			return i
		}
	}
	return -1
}

// Resolve walks the extends chain for the named agent in the active profile set
// and returns a fully-merged ResolvedAgent. Child fields override parent fields.
func (c *Config) Resolve(name string) (*ResolvedAgent, error) {
	if _, ok := c.ActiveAgents[name]; !ok {
		return nil, fmt.Errorf("agent %q not found in active profile", name)
	}

	var chain []string
	visited := map[string]bool{}
	cur := name
	for cur != "" {
		if visited[cur] {
			return nil, fmt.Errorf("cycle in extends chain: %s", strings.Join(append(chain, cur), " -> "))
		}
		visited[cur] = true
		chain = append(chain, cur)

		a, ok := c.ActiveAgents[cur]
		if !ok {
			return nil, fmt.Errorf("agent %q (referenced by extends) not found in active profile", cur)
		}
		if a == nil {
			return nil, fmt.Errorf("agent %q is nil", cur)
		}
		cur = a.Extends
	}

	ra := &ResolvedAgent{}
	for i := len(chain) - 1; i >= 0; i-- {
		a := c.ActiveAgents[chain[i]]
		if a.DefaultMode != "" {
			ra.DefaultMode = a.DefaultMode
		}
		if a.CLI != "" {
			ra.CLI = a.CLI
		}
		if a.Model != "" {
			ra.Model = a.Model
		}
		if a.Effort != "" {
			ra.Effort = a.Effort
		}
		if a.SystemPrompt != "" {
			ra.SystemPrompt = a.SystemPrompt
		}
	}
	return ra, nil
}

// validate checks all agents in all profile sets for completeness and correctness.
func (c *Config) validate() error {
	for setName, ps := range c.Profiles {
		if ps == nil {
			continue
		}
		if err := validateAgentMap(setName, ps.Agents); err != nil {
			return err
		}
	}
	return nil
}

// validateAgentMap validates all agents in a single profile set's agents map.
func validateAgentMap(setName string, agents map[string]*Agent) error {
	for name, a := range agents {
		if a == nil {
			return fmt.Errorf("agent %q in profile set %q must not be empty", name, setName)
		}
		if a.Extends != "" {
			if _, ok := agents[a.Extends]; !ok {
				return fmt.Errorf("agent %q in profile set %q: extends %q which does not exist", name, setName, a.Extends)
			}
		}
		if a.Extends == "" {
			if a.DefaultMode == "" {
				return fmt.Errorf("agent %q in profile set %q: base agent (no extends) must have default_mode", name, setName)
			}
			if a.CLI == "" {
				return fmt.Errorf("agent %q in profile set %q: base agent (no extends) must have cli", name, setName)
			}
		}
		if a.DefaultMode != "" && !validDefaultMode[a.DefaultMode] {
			return fmt.Errorf("agent %q in profile set %q: invalid default_mode %q (must be interactive or headless)", name, setName, a.DefaultMode)
		}
		if a.Effort != "" && !validEffort[a.Effort] {
			return fmt.Errorf("agent %q in profile set %q: invalid effort %q (must be low, medium, high, or xhigh)", name, setName, a.Effort)
		}
		if a.CLI != "" && !validCLI[a.CLI] {
			return fmt.Errorf("agent %q in profile set %q: invalid cli %q (must be claude, codex, copilot, cursor, or opencode)", name, setName, a.CLI)
		}
	}

	for name := range agents {
		if err := detectAgentCycle(agents, name); err != nil {
			return fmt.Errorf("profile set %q: %w", setName, err)
		}
	}
	return nil
}

// detectAgentCycle walks the extends chain from the given agent name and returns
// an error if a cycle is found.
func detectAgentCycle(agents map[string]*Agent, name string) error {
	visited := map[string]bool{}
	cur := name
	var path []string
	for cur != "" {
		if visited[cur] {
			return fmt.Errorf("cycle in extends chain: %s", strings.Join(append(path, cur), " -> "))
		}
		visited[cur] = true
		path = append(path, cur)
		a := agents[cur]
		if a == nil {
			return fmt.Errorf("agent %q is nil", cur)
		}
		cur = a.Extends
	}
	return nil
}

func defaultParsedFile() *parsedFile {
	agents := map[string]*Agent{
		"interactive_base": {DefaultMode: "interactive", CLI: "claude", Model: "opus", Effort: "high"},
		"headless_base":    {DefaultMode: "headless", CLI: "claude", Model: "opus", Effort: "high"},
		"planner":          {Extends: "interactive_base"},
		"implementor":      {Extends: "headless_base"},
		"summarizer":       {DefaultMode: "headless", CLI: "claude", Model: "haiku", Effort: "low"},
	}
	return &parsedFile{
		Profiles: map[string]*ProfileSet{
			"default": {Agents: agents},
		},
	}
}
