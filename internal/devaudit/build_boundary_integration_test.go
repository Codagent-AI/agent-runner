//go:build dev_audit

package devaudit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedDevelopmentBuildPathsEncodeLinkerAssignments(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		file   string
		values []string
	}{
		{
			name: "make build",
			file: "Makefile",
			values: []string{
				"github.com/codagent/agent-runner/internal/devaudit.BuildRootEncoded=$(DEV_AUDIT_ROOT_ENCODED)",
				"github.com/codagent/agent-runner/internal/devaudit.BuildRevision=$(DEV_AUDIT_REVISION)",
				"github.com/codagent/agent-runner/internal/devaudit.BuildDirty=$(DEV_AUDIT_DIRTY)",
			},
		},
		{
			name: "development script",
			file: "dev.sh",
			values: []string{
				"github.com/codagent/agent-runner/internal/devaudit.BuildRootEncoded=${DEV_AUDIT_ROOT_ENCODED}",
				"github.com/codagent/agent-runner/internal/devaudit.BuildRevision=${DEV_AUDIT_REVISION}",
				"github.com/codagent/agent-runner/internal/devaudit.BuildDirty=${DEV_AUDIT_DIRTY}",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range tt.values {
				if !strings.Contains(string(data), value) {
					t.Errorf("%s does not encode linker assignment %q", tt.file, value)
				}
			}
		})
	}
}

func TestInjectedBuildRootDecodesArbitraryPathCharacters(t *testing.T) {
	originalRoot, originalEncoded := BuildRoot, BuildRootEncoded
	BuildRoot = ""
	BuildRootEncoded = "L3RtcC9QYXVsJ3MgIldvcmsiL2FnZW50LXJ1bm5lcg=="
	t.Cleanup(func() { BuildRoot, BuildRootEncoded = originalRoot, originalEncoded })
	if got, want := injectedBuildRoot(), `/tmp/Paul's "Work"/agent-runner`; got != want {
		t.Fatalf("injected build root = %q, want %q", got, want)
	}
}

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
		cmd.Env = append(os.Environ(), "GOFLAGS=")
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
