package config

import (
	"slices"
	"sort"
)

// EnumerateCLIs returns the deduplicated, sorted union of cli values from
// every agent in every profile across the given global and project config
// files. Missing files are treated as empty layers (no error).
func EnumerateCLIs(globalPath, projectPath string) ([]string, error) {
	var globalFile *parsedFile
	if globalPath != "" {
		gf, err := loadFileOptional(globalPath, true)
		if err != nil {
			return nil, err
		}
		globalFile = gf
	}

	var projectFile *parsedFile
	if projectPath != "" {
		pf, err := loadFileOptional(projectPath, false)
		if err != nil {
			return nil, err
		}
		projectFile = pf
	}

	cfg, err := buildConfig(defaultParsedFile(), globalFile, projectFile)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	collectCLIs(seen, globalFile)
	collectCLIs(seen, projectFile)

	result := make([]string, 0, len(seen))
	for cli := range seen {
		result = append(result, cli)
	}
	sort.Strings(result)
	return slices.Clip(result), nil
}

// ConfiguredCLIs returns the configured CLI set for the standard global config
// plus the provided project config path.
func ConfiguredCLIs(projectPath string) ([]string, error) {
	globalPath, err := globalConfigPath()
	if err != nil {
		return nil, err
	}
	return EnumerateCLIs(globalPath, projectPath)
}

func collectCLIs(seen map[string]bool, file *parsedFile) {
	if file == nil {
		return
	}
	for _, ps := range file.Profiles {
		if ps == nil {
			continue
		}
		for _, agent := range ps.Agents {
			if agent != nil && agent.CLI != "" {
				seen[agent.CLI] = true
			}
		}
	}
}
