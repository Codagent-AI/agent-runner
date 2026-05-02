package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardingModelsForCLIParsesClaudeModels(t *testing.T) {
	got := runModelsForCLIWithClaudeOutput(t, "opus\nsonnet\nclaude-3-5-haiku-20241022\n")
	want := `["opus","sonnet","claude-3-5-haiku-20241022"]`
	if got != want {
		t.Fatalf("models-for-cli output = %s, want %s", got, want)
	}
}

func TestOnboardingModelsForCLIIgnoresClaudeHelpText(t *testing.T) {
	got := runModelsForCLIWithClaudeOutput(t, `Here are some things you can try:
|------|------|
| Want | Help |
Run /login first.
`)
	if got != `[]` {
		t.Fatalf("models-for-cli output = %s, want []", got)
	}
}

func TestOnboardingCountListOmitsTrailingNewline(t *testing.T) {
	cmd := exec.Command("sh", filepath.Join("onboarding", "count-list.sh"))
	cmd.Stdin = strings.NewReader(`{"items":[]}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("count-list failed: %v\n%s", err, out)
	}
	if got := string(out); got != "0" {
		t.Fatalf("count-list output = %q, want %q", got, "0")
	}
}

func runModelsForCLIWithClaudeOutput(t *testing.T, claudeOutput string) string {
	t.Helper()

	binDir := t.TempDir()
	claudePath := filepath.Join(binDir, "claude")
	script := "#!/bin/sh\nprintf '%s' " + shellSingleQuote(claudeOutput) + "\n"
	if err := os.WriteFile(claudePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	cmd := exec.Command("sh", filepath.Join("onboarding", "models-for-cli.sh"))
	cmd.Stdin = strings.NewReader(`{"adapter":"claude"}`)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("models-for-cli failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
