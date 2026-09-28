//go:build dev_audit

package devaudit

import (
	"os"
	"path/filepath"
	"testing"
)

func testAuditEvidenceSymlinkReadOnly(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	snapshot := filepath.Join(root, "snapshot")
	source := filepath.Join(snapshot, "runner-source")
	workspace := filepath.Join(root, "model-workspace")
	output := filepath.Join(root, "model-output")
	for _, dir := range []string{source, workspace} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(snapshot, filepath.Join(workspace, "evidence")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(snapshot, "source-workflow.yaml"), filepath.Join(source, "runner.go")} {
		if err := os.WriteFile(path, []byte("evidence\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := `set -eu
for file in evidence/source-workflow.yaml evidence/runner-source/runner.go; do
  test "$(cat "$file")" = evidence
  if printf changed > "$file"; then exit 40; fi
done
`
	command, err := sandboxedCrosscheckCommand([]string{"/bin/sh", "-c", script}, workspace, output)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("evidence access: %v\n%s", err, data)
	}
	for _, path := range []string{filepath.Join(snapshot, "source-workflow.yaml"), filepath.Join(source, "runner.go")} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "evidence\n" {
			t.Fatalf("evidence changed: %s: %q, %v", path, data, err)
		}
	}
}
