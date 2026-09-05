package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDevScriptForwardsAuditCommandBeforeGlobalWorkingDirectory(t *testing.T) {
	temp := t.TempDir()
	binDir := filepath.Join(temp, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(temp, "go-args")
	goStub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$DEV_SH_GO_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "go"), []byte(goStub), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "dev.sh"), "audit", "help")
	cmd.Dir = temp
	cmd.Env = append(os.Environ(),
		"DEV_SH_GO_ARGS="+recordPath,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dev.sh audit help: %v\n%s", err, output)
	}
	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(recorded)), "\n")
	commandIndex := slices.Index(args, "./cmd/agent-runner")
	if commandIndex < 0 {
		t.Fatalf("go arguments omit command package: %v", args)
	}
	if got, want := args[commandIndex+1:], []string{"audit", "help"}; !slices.Equal(got, want) {
		t.Fatalf("agent-runner arguments = %v, want %v", got, want)
	}
}
