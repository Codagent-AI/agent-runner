package repositorylock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireAll_UsesCanonicalRootIdentityAndReportsLiveOwner(t *testing.T) {
	root := t.TempDir()
	lockRoot = func() (string, error) { return root, nil }
	t.Cleanup(func() { lockRoot = defaultLockRoot })

	repository := t.TempDir()
	link := filepath.Join(t.TempDir(), "repository-link")
	if err := os.Symlink(repository, link); err != nil {
		t.Fatal(err)
	}
	if err := AcquireAll([]Target{{Root: repository, RunID: "first-run"}}); err != nil {
		t.Fatalf("first AcquireAll() error = %v", err)
	}
	err := AcquireAll([]Target{{Root: link, RunID: "second-run"}})
	if err == nil {
		t.Fatal("second AcquireAll() succeeded while first run owns canonical root")
	}
	if !strings.Contains(err.Error(), "first-run") || !strings.Contains(err.Error(), repository) {
		t.Fatalf("contention error = %v, want owning run and canonical root", err)
	}
}

func TestAcquireAll_RecoversStaleOwner(t *testing.T) {
	root := t.TempDir()
	lockRoot = func() (string, error) { return root, nil }
	t.Cleanup(func() { lockRoot = defaultLockRoot })

	repository := t.TempDir()
	canonical, err := canonicalRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, lockDirectory, lockName(canonical))
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"root":"`+canonical+`","run_id":"stale-run","pid":99999999}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AcquireAll([]Target{{Root: repository, RunID: "recovered-run"}}); err != nil {
		t.Fatalf("AcquireAll() error = %v", err)
	}
	contents, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "recovered-run") {
		t.Fatalf("lock metadata = %s, want recovered owner", contents)
	}
}

func TestAcquireAll_DoesNotReleaseExistingSameRunLockWhenLaterTargetFails(t *testing.T) {
	root := t.TempDir()
	lockRoot = func() (string, error) { return root, nil }
	t.Cleanup(func() { lockRoot = defaultLockRoot })

	held := t.TempDir()
	contended := t.TempDir()
	if err := AcquireAll([]Target{{Root: held, RunID: "run-a"}}); err != nil {
		t.Fatalf("acquire held lock: %v", err)
	}
	if err := AcquireAll([]Target{{Root: contended, RunID: "run-b"}}); err != nil {
		t.Fatalf("acquire contended lock: %v", err)
	}
	if err := AcquireAll([]Target{{Root: held, RunID: "run-a"}, {Root: contended, RunID: "run-a"}}); err == nil {
		t.Fatal("AcquireAll() succeeded despite later contended repository")
	}

	canonicalHeld, err := canonicalRoot(held)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(root, lockDirectory, lockName(canonicalHeld)))
	if err != nil {
		t.Fatalf("read original lock after failed group acquisition: %v", err)
	}
	if !strings.Contains(string(contents), `"run_id":"run-a"`) {
		t.Fatalf("original lock metadata = %s, want run-a owner", contents)
	}
}
