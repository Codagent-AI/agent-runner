package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// smokeContainerHeredoc returns the body of the heredoc that the in-container
// smoke script writes to target, terminated by marker.
func smokeContainerHeredoc(t *testing.T, target, marker string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "docker-dev-audit-smoke-container.sh"))
	if err != nil {
		t.Fatal(err)
	}
	opener := "cat > " + target + " <<'" + marker + "'\n"
	_, rest, ok := strings.Cut(string(data), opener)
	if !ok {
		t.Fatalf("container script has no %q heredoc", opener)
	}
	body, _, ok := strings.Cut(rest, "\n"+marker+"\n")
	if !ok {
		t.Fatalf("heredoc %q is not terminated", marker)
	}
	return body + "\n"
}

func TestDevAuditSmokeFixtureConfiguresLeadAuditorAsFakeCodex(t *testing.T) {
	var config struct {
		Profiles map[string]struct {
			Agents map[string]struct {
				CLI string `yaml:"cli"`
			} `yaml:"agents"`
		} `yaml:"profiles"`
	}
	body := smokeContainerHeredoc(t, `"$project/.agent-runner/config.yaml"`, "YAML")
	if err := yaml.Unmarshal([]byte(body), &config); err != nil {
		t.Fatalf("parse fixture config: %v\n%s", err, body)
	}
	agents := config.Profiles["default"].Agents
	for _, name := range []string{"lead", "crosscheck"} {
		if got := agents[name].CLI; got != "codex" {
			t.Errorf("fixture agent %q cli = %q, want codex (the fake); config:\n%s", name, got, body)
		}
	}
}

func TestDevAuditSmokeFakeCodexReadsPromptFromStdin(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "codex")
	writeExecutable(t, fake, smokeContainerHeredoc(t, `"$fake_bin/codex"`, "FAKE"))
	output := filepath.Join(dir, "last-message.txt")

	cmd := exec.Command(fake, "exec", "--output-last-message", output, "-")
	cmd.Stdin = strings.NewReader("Investigate only reproducible Agent Runner defects in this run.\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fake codex: %v\n%s", err, out)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"candidates":[]}` + "\n"; string(got) != want {
		t.Fatalf("last message = %q, want %q", got, want)
	}
}
