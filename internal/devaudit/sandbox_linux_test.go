//go:build dev_audit && linux

package devaudit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These are real OS enforcement tests. The supported Docker test command sets
// AGENT_RUNNER_REQUIRE_LINUX_SANDBOX=1 so unavailable confinement cannot skip.
func requireLinuxAuditSandbox(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	workspace, output := filepath.Join(root, "workspace"), filepath.Join(root, "output")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := sandboxedCrosscheckCommand([]string{"/bin/true"}, workspace, output)
	var result []byte
	if err == nil {
		result, err = command.CombinedOutput()
	}
	if err != nil {
		if os.Getenv("AGENT_RUNNER_REQUIRE_LINUX_SANDBOX") == "1" {
			t.Fatalf("required Linux sandbox unavailable: %v: %s", err, result)
		}
		t.Skipf("Linux sandbox unavailable on this host: %v: %s", err, result)
	}
}

func TestLinuxAuditFilesystemConfinement(t *testing.T) {
	requireLinuxAuditSandbox(t)
	root := t.TempDir()
	workspace, output := filepath.Join(root, "workspace"), filepath.Join(root, "output")
	for _, dir := range []string{workspace, output} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	targets := []string{"project", "live-run", "evidence", "runner-source", "home", "temp"}
	for _, name := range targets {
		path := filepath.Join(root, name)
		// All targets are writable by this UID before confinement.
		if err := os.WriteFile(path, []byte("protected\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		control := exec.Command("/bin/sh", "-c", `printf 'protected\n' > "$1"`, "control", path)
		if data, err := control.CombinedOutput(); err != nil {
			t.Fatalf("unconfined control: %v: %s", err, data)
		}
	}
	inherited, err := os.OpenFile(filepath.Join(root, "project"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	script := `
set -eu
root=$1
output=$2
printf 'allowed\n' > "$output/result"
for name in project live-run evidence runner-source home temp; do
  if printf changed > "$root/$name"; then echo "overwrite escaped"; exit 40; fi
  if rm "$root/$name"; then echo "delete escaped"; exit 41; fi
done
if mkdir "$root/new-directory"; then exit 42; fi
if touch "$root/new-file"; then exit 43; fi
if mv "$root/project" "$output/moved"; then exit 44; fi
if ln "$root/project" "$output/hardlink"; then exit 45; fi
ln -s "$root/project" "$output/escape"
if printf changed > "$output/escape"; then exit 46; fi
if printf changed > "$output/../project"; then exit 47; fi
if /bin/sh -c 'printf changed > "$1"' child "$root/project"; then exit 48; fi
if printf changed >&3; then echo "inherited descriptor escaped"; exit 49; fi
`
	command, err := sandboxedCrosscheckCommand([]string{"/bin/sh", "-c", script, "probe", root, output}, workspace, output)
	if err != nil {
		t.Fatal(err)
	}
	command.ExtraFiles = []*os.File{inherited}
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("confinement probe: %v\n%s", err, data)
	}
	for _, name := range targets {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != "protected\n" {
			t.Fatalf("%s changed: %q, %v", name, data, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(output, "result")); err != nil || string(data) != "allowed\n" {
		t.Fatalf("permitted output failed: %q, %v", data, err)
	}
}

func TestLinuxAuditUnavailableSandboxDoesNotRunPayload(t *testing.T) {
	root := t.TempDir()
	workspace, output := filepath.Join(root, "workspace"), filepath.Join(root, "output")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	marker := filepath.Join(output, "model-started")
	args := []string{"/bin/sh", "-c", `printf started > "$1"`, "model", marker}
	if _, err := sandboxedCrosscheckCommand(args, workspace, output); err == nil {
		t.Fatal("missing sandbox unexpectedly accepted")
	}
	// A present backend can also be rejected by kernel/container policy.
	// Inject only that setup failure; never replace the production launcher.
	if err := os.WriteFile(filepath.Join(root, "bwrap"), []byte("#!/bin/sh\necho 'namespace setup denied' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := sandboxedCrosscheckCommand(args, workspace, output)
	if err != nil {
		t.Fatal(err)
	}
	data, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(data), "namespace setup denied") {
		t.Fatalf("setup rejection lost: %v: %s", err, data)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("model payload executed after setup failure: %v", err)
	}
}

func TestLinuxAuditDisposableRuntime(t *testing.T) {
	requireLinuxAuditSandbox(t)
	testSandboxedCodexRuntime(t)
}

func TestLinuxAuditDockerRetainsSeccomp(t *testing.T) {
	if os.Getenv("AGENT_RUNNER_REQUIRE_LINUX_SANDBOX") != "1" {
		t.Skip("supported Docker invocation only")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(status), "Seccomp:\t2\n") {
		t.Fatal("supported audit container is not running with a seccomp filter")
	}
}
