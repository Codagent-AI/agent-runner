package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOwnedClaudePath(t *testing.T) {
	t.Run("root owned", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root owns the root directory")
		}
		owned, err := ownedClaudePath("/")
		if err != nil || owned {
			t.Fatalf("ownedClaudePath(/) = %v, %v; want false, nil", owned, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		owned, err := ownedClaudePath(filepath.Join(t.TempDir(), "missing"))
		if err != nil || !owned {
			t.Fatalf("missing path ownership = %v, %v; want true, nil", owned, err)
		}
	})
	t.Run("current user file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		owned, err := ownedClaudePath(path)
		if err != nil || !owned {
			t.Fatalf("file ownership = %v, %v; want true, nil", owned, err)
		}
	})
}

func TestClaudeSettingsRootNotOwned(t *testing.T) {
	root := t.TempDir()
	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	work := filepath.Join(root, "sub")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	original := ownedClaudePathFn
	t.Cleanup(func() { ownedClaudePathFn = original })
	ownedClaudePathFn = func(path string) (bool, error) {
		if path == root {
			return false, nil
		}
		return ownedClaudePath(path)
	}
	got, err := claudeSettingsRoot(work, t.TempDir(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if got != work {
		t.Fatalf("settings root = %q, want working directory %q", got, work)
	}
}
