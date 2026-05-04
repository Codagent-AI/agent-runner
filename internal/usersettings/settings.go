// Package usersettings owns the global per-user settings file.
package usersettings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Theme string

const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

type Settings struct {
	Theme      Theme
	Setup      SetupSettings
	Onboarding OnboardingSettings

	raw string
}

type SetupSettings struct {
	CompletedAt string `yaml:"completed_at,omitempty"`
}

type OnboardingSettings struct {
	CompletedAt string `yaml:"completed_at,omitempty"`
	Dismissed   string `yaml:"dismissed,omitempty"`
}

var (
	writePayload = func(f *os.File, payload []byte) error {
		_, err := f.Write(payload)
		return err
	}
	renameFile = os.Rename
)

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".agent-runner", "settings.yaml"), nil
}

func Load() (Settings, error) {
	path, err := Path()
	if err != nil {
		return Settings{}, err
	}

	dir := filepath.Dir(path)
	settingsRoot, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	defer func() { _ = settingsRoot.Close() }()

	body, err := settingsRoot.ReadFile(filepath.Base(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return Settings{}, nil
	}
	if len(doc.Content) == 0 {
		return Settings{}, nil
	}

	yamlRoot := doc.Content[0]
	if yamlRoot.Kind != yaml.MappingNode {
		return Settings{}, nil
	}

	settings := Settings{raw: string(body)}
	for i := 0; i+1 < len(yamlRoot.Content); i += 2 {
		key := yamlRoot.Content[i]
		value := yamlRoot.Content[i+1]
		if key.Value == "theme" && value.Kind == yaml.ScalarNode {
			switch Theme(value.Value) {
			case ThemeLight:
				settings.Theme = ThemeLight
			case ThemeDark:
				settings.Theme = ThemeDark
			}
			continue
		}
		if key.Value == "setup" && value.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(value.Content); j += 2 {
				k := value.Content[j]
				v := value.Content[j+1]
				if v.Kind != yaml.ScalarNode {
					continue
				}
				if k.Value == "completed_at" {
					settings.Setup.CompletedAt = v.Value
				}
			}
		}
		if key.Value == "onboarding" && value.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(value.Content); j += 2 {
				k := value.Content[j]
				v := value.Content[j+1]
				if v.Kind != yaml.ScalarNode {
					continue
				}
				switch k.Value {
				case "completed_at":
					settings.Onboarding.CompletedAt = v.Value
				case "dismissed":
					settings.Onboarding.Dismissed = v.Value
				}
			}
		}
	}

	return settings, nil
}

func Save(settings Settings) error {
	path, err := Path()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	// #nosec G301 -- the user-settings spec requires ~/.agent-runner to be 0755.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create settings directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "settings-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings file in %s: %w", dir, err)
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
		return fmt.Errorf("chmod temporary settings file %s: %w", tmpName, err)
	}

	payload, err := marshalSettings(settings)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := writePayload(tmp, payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary settings file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary settings file %s: %w", tmpName, err)
	}
	if err := renameFile(tmpName, path); err != nil {
		return fmt.Errorf("rename temporary settings file %s to %s: %w", tmpName, path, err)
	}

	cleanup = false
	return nil
}

func marshalSettings(settings Settings) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	if settings.raw != "" {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(settings.raw), &doc); err == nil && len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
			root = doc.Content[0]
		}
	}

	if settings.Theme != "" {
		setScalar(root, "theme", string(settings.Theme))
	} else {
		removeKey(root, "theme")
	}

	if settings.Setup.CompletedAt != "" {
		setup := mappingValue(root, "setup")
		setTimestampScalar(setup, "completed_at", settings.Setup.CompletedAt)
		removeKey(setup, "dismissed")
	} else {
		removeKey(root, "setup")
	}

	if settings.Onboarding.CompletedAt != "" || settings.Onboarding.Dismissed != "" {
		onboarding := mappingValue(root, "onboarding")
		if settings.Onboarding.CompletedAt != "" {
			setTimestampScalar(onboarding, "completed_at", settings.Onboarding.CompletedAt)
		} else {
			removeKey(onboarding, "completed_at")
		}
		if settings.Onboarding.Dismissed != "" {
			setTimestampScalar(onboarding, "dismissed", settings.Onboarding.Dismissed)
		} else {
			removeKey(onboarding, "dismissed")
		}
	} else {
		removeKey(root, "onboarding")
	}

	var doc yaml.Node
	doc.Kind = yaml.DocumentNode
	doc.Content = []*yaml.Node{root}
	return yaml.Marshal(&doc)
}

func mappingValue(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			if root.Content[i+1].Kind != yaml.MappingNode {
				root.Content[i+1] = &yaml.Node{Kind: yaml.MappingNode}
			}
			return root.Content[i+1]
		}
	}
	value := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, scalarNode(key), value)
	return value
}

func setScalar(root *yaml.Node, key, value string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = scalarNode(value)
			return
		}
	}
	root.Content = append(root.Content, scalarNode(key), scalarNode(value))
}

func setTimestampScalar(root *yaml.Node, key, value string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: value}
			return
		}
	}
	root.Content = append(root.Content, scalarNode(key), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: value})
}

func removeKey(root *yaml.Node, key string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
