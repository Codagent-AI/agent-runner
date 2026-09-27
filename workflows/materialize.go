package builtinworkflows

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaterializeNamespace writes every asset of a builtin namespace under
// <sessionDir>/bundled/<namespace>. Scripts call helpers beside them
// ("$script_dir/<name>"), so a namespace is always materialized whole rather
// than script by script. Assets that are already current are left untouched,
// so calling this again (on resume, or for each script step) replaces only
// missing or stale files.
func MaterializeNamespace(sessionDir, namespace string) error {
	assets, err := ListAssets(namespace)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(sessionDir, "bundled", namespace), 0o700); err != nil {
		return fmt.Errorf("create bundled asset root: %w", err)
	}
	for _, asset := range assets {
		if _, err := WriteAsset(sessionDir, namespace, asset); err != nil {
			return err
		}
	}
	return nil
}

// WriteAsset materializes one asset of a namespace and returns its path. Scripts
// are executable. An asset whose bytes and mode already match is left in place;
// otherwise the content goes to a temporary file that is renamed over the
// target, because a running bash reads its script as it goes and must never see
// it truncated and rewritten in place.
func WriteAsset(sessionDir, namespace, relAsset string) (string, error) {
	data, err := ReadAsset(path.Join(namespace, relAsset))
	if err != nil {
		return "", err
	}
	target := filepath.Join(sessionDir, "bundled", namespace, filepath.FromSlash(relAsset))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("create bundled asset directory: %w", err)
	}
	mode := os.FileMode(0o600)
	if strings.HasSuffix(relAsset, ".sh") {
		mode = 0o700
	}
	if assetCurrent(target, data, mode) {
		return target, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*")
	if err != nil {
		return "", fmt.Errorf("write bundled asset %s: %w", target, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(tmp.Name(), mode)); err != nil {
		return "", fmt.Errorf("write bundled asset %s: %w", target, err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", fmt.Errorf("write bundled asset %s: %w", target, err)
	}
	return target, nil
}

func assetCurrent(target string, data []byte, mode os.FileMode) bool {
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		return false
	}
	existing, err := os.ReadFile(target) // #nosec G304 -- target is the session's bundled copy of a vetted embedded asset path.
	return err == nil && bytes.Equal(existing, data)
}
