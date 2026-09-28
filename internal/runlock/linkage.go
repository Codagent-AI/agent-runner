package runlock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ClaimLinkage serializes audit sibling creation and linkage updates with
// retention's final group membership check. It never creates a source run.
func ClaimLinkage(sourceDir string) (func(), error) {
	path := filepath.Join(sourceDir, "audit-linkage.lock")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(sourceDir, "state.json")); err != nil {
			return nil, fmt.Errorf("source run: %w", err)
		}
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- source run directory is selected by caller.
		if err != nil {
			return nil, err
		}
		locked, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if !locked {
			_ = f.Close()
			time.Sleep(10 * time.Millisecond)
			continue
		}
		opened, err := f.Stat()
		current, pathErr := os.Lstat(path)
		if err != nil || pathErr != nil || !os.SameFile(opened, current) {
			unlock(f)
			_ = f.Close()
			if pathErr != nil && !errors.Is(pathErr, fs.ErrNotExist) {
				return nil, pathErr
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(sourceDir, "state.json")); err != nil {
			unlock(f)
			_ = f.Close()
			return nil, fmt.Errorf("source run: %w", err)
		}
		return func() { unlock(f); _ = f.Close() }, nil
	}
	return nil, errors.New("timed out waiting for audit linkage")
}
