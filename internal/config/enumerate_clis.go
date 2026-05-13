package config

import (
	"errors"
	"os"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"
)

// EnumerateCLIs returns the deduplicated, sorted union of cli values from
// every agent in every profile across the given global and project config
// files. Missing files are treated as empty layers (no error).
func EnumerateCLIs(globalPath, projectPath string) ([]string, error) {
	seen := map[string]bool{}
	for _, path := range []string{globalPath, projectPath} {
		if path == "" {
			continue
		}
		clis, err := collectCLIs(path)
		if err != nil {
			return nil, err
		}
		for _, cli := range clis {
			seen[cli] = true
		}
	}
	result := make([]string, 0, len(seen))
	for cli := range seen {
		result = append(result, cli)
	}
	sort.Strings(result)
	return slices.Clip(result), nil
}

func collectCLIs(path string) ([]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- config path from internal caller
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var raw struct {
		Profiles map[string]*ProfileSet `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var clis []string
	for _, ps := range raw.Profiles {
		if ps == nil {
			continue
		}
		for _, agent := range ps.Agents {
			if agent != nil && agent.CLI != "" {
				clis = append(clis, agent.CLI)
			}
		}
	}
	return clis, nil
}
