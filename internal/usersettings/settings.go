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
	Theme Theme
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

	for i := 0; i+1 < len(yamlRoot.Content); i += 2 {
		key := yamlRoot.Content[i]
		value := yamlRoot.Content[i+1]
		if key.Value != "theme" || value.Kind != yaml.ScalarNode {
			continue
		}
		switch Theme(value.Value) {
		case ThemeLight:
			return Settings{Theme: ThemeLight}, nil
		case ThemeDark:
			return Settings{Theme: ThemeDark}, nil
		default:
			return Settings{}, nil
		}
	}

	return Settings{}, nil
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

	payload := []byte(fmt.Sprintf("theme: %s\n", settings.Theme))
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
