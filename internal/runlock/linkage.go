package runlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ClaimLinkage serializes audit sibling creation and linkage updates with
// retention's final group membership check. It never creates a source run.
func ClaimLinkage(sourceDir string) (func(), error) {
	path := filepath.Join(sourceDir, "audit-linkage.lock")
	statePath := filepath.Join(sourceDir, "state.json")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(statePath); err != nil {
			return nil, fmt.Errorf("source run: %w", err)
		}
		f, outcome, err := lockStablePath(path)
		if err != nil {
			return nil, err
		}
		switch outcome {
		case lockBusy:
			time.Sleep(10 * time.Millisecond)
			continue
		case lockReplaced:
			continue
		}
		if _, err := os.Stat(statePath); err != nil {
			release(f)
			return nil, fmt.Errorf("source run: %w", err)
		}
		return func() { release(f) }, nil
	}
	return nil, errors.New("timed out waiting for audit linkage")
}
