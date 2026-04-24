// Package config loads, validates, and resolves agent profiles from
// .agent-runner/config.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile represents a named agent profile as declared in config YAML.
// All fields are optional; a profile without Extends must supply at least
// DefaultMode and CLI.
type Profile struct {
	DefaultMode  string `yaml:"default_mode,omitempty"`
	CLI          string `yaml:"cli,omitempty"`
	Model        string `yaml:"model,omitempty"`
	Effort       string `yaml:"effort,omitempty"`
	SystemPrompt string `yaml:"system_prompt,omitempty"`
	Extends      string `yaml:"extends,omitempty"`
}

// ResolvedProfile is a fully-merged profile after walking the extends chain.
// DefaultMode and CLI are guaranteed populated. Model, Effort, and
// SystemPrompt may be empty (meaning "not set").
type ResolvedProfile struct {
	DefaultMode  string
	CLI          string
	Model        string
	Effort       string
	SystemPrompt string
}

// Config is the top-level configuration loaded from .agent-runner/config.yaml.
type Config struct {
	Profiles map[string]*Profile `yaml:"profiles"`
}

var validEffort = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

var validDefaultMode = map[string]bool{
	"interactive": true,
	"headless":    true,
}

var validCLI = map[string]bool{
	"claude":  true,
	"codex":   true,
	"copilot": true,
}

// LoadOrGenerate loads the config file at path. If the file does not exist,
// it writes a default config and returns that. After loading, all profiles
// are validated.
func LoadOrGenerate(path string) (*Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- config path is from internal caller
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// File does not exist — generate default.
		cfg := defaultConfig()
		if writeErr := writeDefault(path, cfg); writeErr != nil {
			return nil, fmt.Errorf("generating default config: %w", writeErr)
		}
		return cfg, nil
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Resolve walks the extends chain for the named profile and returns a
// fully-merged ResolvedProfile. Child fields override parent fields.
func (c *Config) Resolve(name string) (*ResolvedProfile, error) {
	if _, ok := c.Profiles[name]; !ok {
		return nil, fmt.Errorf("profile %q not found", name)
	}

	// Collect the chain from child → root.
	var chain []string
	visited := map[string]bool{}
	cur := name
	for cur != "" {
		if visited[cur] {
			return nil, fmt.Errorf("cycle in extends chain: %s", strings.Join(append(chain, cur), " -> "))
		}
		visited[cur] = true
		chain = append(chain, cur)

		p, ok := c.Profiles[cur]
		if !ok {
			return nil, fmt.Errorf("profile %q (referenced by extends) not found", cur)
		}
		if p == nil {
			return nil, fmt.Errorf("profile %q is nil", cur)
		}
		cur = p.Extends
	}

	// Merge from root (last) to child (first) so child overrides parent.
	rp := &ResolvedProfile{}
	for i := len(chain) - 1; i >= 0; i-- {
		p := c.Profiles[chain[i]]
		if p.DefaultMode != "" {
			rp.DefaultMode = p.DefaultMode
		}
		if p.CLI != "" {
			rp.CLI = p.CLI
		}
		if p.Model != "" {
			rp.Model = p.Model
		}
		if p.Effort != "" {
			rp.Effort = p.Effort
		}
		if p.SystemPrompt != "" {
			rp.SystemPrompt = p.SystemPrompt
		}
	}
	return rp, nil
}

// validate checks all profiles for completeness and correctness.
func (c *Config) validate() error {
	if len(c.Profiles) == 0 {
		return fmt.Errorf("config must define at least one profile")
	}

	for name, p := range c.Profiles {
		if p == nil {
			return fmt.Errorf("profile %q: must not be empty", name)
		}
		// Check extends references exist.
		if p.Extends != "" {
			if _, ok := c.Profiles[p.Extends]; !ok {
				return fmt.Errorf("profile %q: extends %q which does not exist", name, p.Extends)
			}
		}

		// Base profile completeness.
		if p.Extends == "" {
			if p.DefaultMode == "" {
				return fmt.Errorf("profile %q: base profile (no extends) must have default_mode", name)
			}
			if p.CLI == "" {
				return fmt.Errorf("profile %q: base profile (no extends) must have cli", name)
			}
		}

		// Field value validation.
		if p.DefaultMode != "" && !validDefaultMode[p.DefaultMode] {
			return fmt.Errorf("profile %q: invalid default_mode %q (must be interactive or headless)", name, p.DefaultMode)
		}
		if p.Effort != "" && !validEffort[p.Effort] {
			return fmt.Errorf("profile %q: invalid effort %q (must be low, medium, or high)", name, p.Effort)
		}
		if p.CLI != "" && !validCLI[p.CLI] {
			return fmt.Errorf("profile %q: invalid cli %q (must be claude, codex, or copilot)", name, p.CLI)
		}
	}

	// Cycle detection across all profiles.
	for name := range c.Profiles {
		if err := c.detectCycle(name); err != nil {
			return err
		}
	}

	return nil
}

// detectCycle walks the extends chain from the given profile name and returns
// an error if a cycle is found.
func (c *Config) detectCycle(name string) error {
	visited := map[string]bool{}
	cur := name
	var path []string
	for cur != "" {
		if visited[cur] {
			return fmt.Errorf("cycle in extends chain: %s", strings.Join(append(path, cur), " -> "))
		}
		visited[cur] = true
		path = append(path, cur)
		p := c.Profiles[cur]
		if p == nil {
			return fmt.Errorf("profile %q is nil", cur)
		}
		cur = p.Extends
	}
	return nil
}

func defaultConfig() *Config {
	return &Config{
		Profiles: map[string]*Profile{
			"interactive_base": {
				DefaultMode: "interactive",
				CLI:         "claude",
				Model:       "opus",
				Effort:      "high",
			},
			"headless_base": {
				DefaultMode: "headless",
				CLI:         "claude",
				Model:       "sonnet",
				Effort:      "high",
			},
			"planner": {
				Extends: "interactive_base",
			},
			"implementor": {
				Extends: "headless_base",
			},
			"summarizer": {
				DefaultMode: "headless",
				CLI:         "claude",
				Model:       "haiku",
				Effort:      "low",
			},
		},
	}
}

func writeDefault(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
