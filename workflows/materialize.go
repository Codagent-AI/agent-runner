package builtinworkflows

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaterializeNamespace writes every asset of a builtin namespace under
// <sessionDir>/bundled/<namespace>, once per session. Scripts call helpers beside
// them, so a namespace is always materialized whole rather than script by script.
func MaterializeNamespace(sessionDir, namespace string) error {
	root := filepath.Join(sessionDir, "bundled", namespace)
	marker := filepath.Join(root, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	assets, err := ListAssets(namespace)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create bundled asset root: %w", err)
	}
	for _, asset := range assets {
		data, err := ReadAsset(path.Join(namespace, asset))
		if err != nil {
			return err
		}
		if err := WriteBundledAsset(filepath.Join(root, filepath.FromSlash(asset)), asset, data); err != nil {
			return err
		}
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		return fmt.Errorf("write bundled asset completion marker: %w", err)
	}
	return nil
}

// WriteBundledAsset writes one materialized asset, executable when it is a script.
func WriteBundledAsset(target, asset string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create bundled asset directory: %w", err)
	}
	mode := os.FileMode(0o600)
	if strings.HasSuffix(asset, ".sh") {
		mode = 0o700
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		return fmt.Errorf("write bundled asset %s: %w", target, err)
	}
	return nil
}
