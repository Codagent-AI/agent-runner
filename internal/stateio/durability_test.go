//go:build !windows

package stateio

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDurableJSONDoesNotSucceedWhenNewAncestorCannotBeSynced(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "new-parent")
	path := filepath.Join(parent, "new-child", "evidence.json")
	prior := syncJSONDirectory
	t.Cleanup(func() { syncJSONDirectory = prior })
	failure := errors.New("injected ancestor sync failure")
	syncJSONDirectory = func(path string) error {
		if path == parent {
			return failure
		}
		return prior(path)
	}
	if err := WriteJSONDurable(path, map[string]string{"receipt": "retained"}); !errors.Is(err, failure) {
		t.Fatalf("durable publication ignored new ancestor sync failure: %v", err)
	}
}

func TestDurableJSONSyncStopsAtExistingParent(t *testing.T) {
	dir := t.TempDir()
	prior := syncJSONDirectory
	t.Cleanup(func() { syncJSONDirectory = prior })
	syncJSONDirectory = func(path string) error {
		if path != dir {
			return errors.New("unrelated ancestor")
		}
		return prior(path)
	}
	if err := WriteJSONDurable(filepath.Join(dir, "evidence.json"), map[string]string{"receipt": "retained"}); err != nil {
		t.Fatalf("existing directory write synced unrelated ancestors: %v", err)
	}
}
