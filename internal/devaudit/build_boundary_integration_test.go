//go:build dev_audit

package devaudit

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// INT-001: local tagged binaries expose the private command while an ordinary
// temporary binary has neither command routing nor an embedded workflow.
func TestDevelopmentAuditBuildBoundary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	prod := filepath.Join(temp, "agent-runner-prod")
	dev := filepath.Join(temp, "agent-runner-dev")
	build := func(output string, tagged bool) {
		args := []string{"build", "-o", output}
		if tagged {
			args = append(args, "-tags", "dev_audit")
		}
		args = append(args, "./cmd/agent-runner")
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	build(prod, false)
	build(dev, true)
	if output, err := exec.Command(dev, "audit", "help").CombinedOutput(); err != nil || !strings.Contains(string(output), "audit status") {
		t.Fatalf("tagged audit help: %v\n%s", err, output)
	}
	if output, err := exec.Command(prod, "audit", "help").CombinedOutput(); err == nil || strings.Contains(string(output), "audit status") {
		t.Fatalf("untagged binary exposed audit command: %v\n%s", err, output)
	}
}
