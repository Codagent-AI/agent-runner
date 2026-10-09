package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A stale checkout without go.mod belongs to the parent module. Exercise the
// configured commands in a small real module so broken stale packages cannot
// silently become part of the main checkout's build or tests.
func TestValidatorGoChecksExcludeStaleWorktrees(t *testing.T) {
	root := repoRoot(t)
	fixture := t.TempDir()
	write := func(name, body string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(fixture, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/codagent/agent-runner\n\ngo 1.26.1\n", 0o600)
	write("main.go", "package main\nfunc main() {}\n", 0o600)
	write("testonly/only_test.go", "package testonly\n", 0o600)
	write("tagged/tagged.go", "//go:build dev_audit\n\npackage tagged\n", 0o600)
	write("worktrees/stale/broken.go", "package stale\nfunc broken() { missing() }\n", 0o600)
	write("worktrees/stale/broken_test.go", "package stale\nimport \"testing\"\nfunc TestStale(t *testing.T) { t.Fatal(\"stale test ran\") }\n", 0o600)
	entries, err := os.ReadDir(filepath.Join(root, ".validator"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, ".validator", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		write(".validator/"+entry.Name(), string(body), 0o700)
	}
	// Cache isolation is covered by go_offline_test.go. Avoid copying the host's
	// module cache for this dependency-free fixture.
	write(".validator/go-offline.sh", "#!/bin/sh\nexec \"$@\"\n", 0o700)
	// External analyzers need not be installed for the regression test. Their
	// stubs reject recursive patterns and require the tagged package for lint.
	write("bin/golangci-lint", `#!/bin/sh
case "$*" in
  *./...*|*worktrees*|*github.com/codagent/agent-runner*) echo 'stale packages reached lint' >&2; exit 1;;
esac
case "$*" in
  *./tagged*) exit 0;;
  *) echo 'tagged package missing from lint' >&2; exit 1;;
esac
`, 0o700)
	write("bin/deadcode", `#!/bin/sh
case "$*" in
  *./...*|*worktrees*) echo 'worktrees/stale/broken.go: unreachable func';;
esac
`, 0o700)
	configBody, err := os.ReadFile(filepath.Join(root, ".validator", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		EntryPoints []struct {
			Path    string   `yaml:"path"`
			Exclude []string `yaml:"exclude"`
			Checks  []map[string]struct {
				Command string `yaml:"command"`
			} `yaml:"checks"`
		} `yaml:"entry_points"`
	}
	if err := yaml.Unmarshal(configBody, &config); err != nil {
		t.Fatal(err)
	}
	run := func(command string) ([]byte, error) {
		t.Helper()
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = fixture
		cmd.Env = append(os.Environ(), "GOWORK=off", "PATH="+filepath.Join(fixture, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		return cmd.CombinedOutput()
	}
	for _, entry := range config.EntryPoints {
		if entry.Path != "." {
			continue
		}
		t.Run("change exclusion", func(t *testing.T) {
			for _, excluded := range entry.Exclude {
				if excluded == "worktrees" {
					return
				}
			}
			t.Fatal("worktrees changes still trigger the main entry point")
		})
		for _, check := range entry.Checks {
			for name, details := range check {
				switch name {
				case "build", "test", "lint", "lint-strict", "deadcode":
				default:
					continue
				}
				t.Run(name, func(t *testing.T) {
					output, err := run(details.Command)
					if err != nil {
						t.Fatalf("check loaded stale worktree packages: %v\n%s", err, output)
					}
					if name == "test" {
						write("main_test.go", "package main\nimport \"testing\"\nfunc TestMainFailure(t *testing.T) { t.Fatal(\"main test ran\") }\n", 0o600)
						defer os.Remove(filepath.Join(fixture, "main_test.go"))
						output, err = run(details.Command)
						if err == nil || !strings.Contains(string(output), "main test ran") {
							t.Fatalf("main test failure was lost: %v\n%s", err, output)
						}
					}
				})
			}
		}
	}
}
