package usersettings

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// RunRetention holds optional user overrides. Nil values use the policy defaults.
type RunRetention struct {
	Enabled             *bool
	MaxAgeDays          *int
	ResumableMaxAgeDays *int
	MaxRunsPerProject   *int
	Invalid             []string
}

// LoadRunRetention reads this setting independently of unrelated settings.
// A malformed or unreadable file is unknown and must never authorize deletion.
func LoadRunRetention() (RunRetention, bool, error) {
	path, err := Path()
	if err != nil {
		return RunRetention{}, false, err
	}
	return LoadRunRetentionAt(path)
}

// LoadRunRetentionAt reads retention policy from an explicit settings path.
func LoadRunRetentionAt(path string) (RunRetention, bool, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- fixed user settings path.
	if errors.Is(err, os.ErrNotExist) {
		return RunRetention{}, true, nil
	}
	if err != nil {
		return RunRetention{}, false, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return RunRetention{}, false, err
	}
	// An empty or comment-only file cannot hold an opt-out and is equivalent to
	// a missing file under the settings-file contract.
	if len(doc.Content) == 0 {
		return RunRetention{}, true, nil
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return RunRetention{}, false, errors.New("settings root must be a mapping")
	}
	root := doc.Content[0]
	var result RunRetention
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "run_retention" {
			continue
		}
		mapping := root.Content[i+1]
		if mapping.Kind != yaml.MappingNode {
			return RunRetention{}, false, errors.New("run_retention must be a mapping")
		}
		for j := 0; j+1 < len(mapping.Content); j += 2 {
			key, value := mapping.Content[j].Value, mapping.Content[j+1]
			switch key {
			case "enabled":
				if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
					result.Invalid = append(result.Invalid, "enabled")
				} else {
					b := value.Value == "true"
					result.Enabled = &b
				}
			case "max_age_days", "resumable_max_age_days", "max_runs_per_project":
				n, parseErr := strconv.Atoi(value.Value)
				tooLargeAge := key != "max_runs_per_project" && n > int(math.MaxInt64/int64(24*time.Hour))
				if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || parseErr != nil || n < 0 || tooLargeAge {
					result.Invalid = append(result.Invalid, fmt.Sprintf("%s: invalid value %q", key, value.Value))
					continue
				}
				switch key {
				case "max_age_days":
					result.MaxAgeDays = &n
				case "resumable_max_age_days":
					result.ResumableMaxAgeDays = &n
				case "max_runs_per_project":
					result.MaxRunsPerProject = &n
				}
			}
		}
	}
	return result, true, nil
}
