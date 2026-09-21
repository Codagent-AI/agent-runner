package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGoOfflineScriptFindsToolsInHomeGoBin(t *testing.T) {
	home := t.TempDir()
	toolBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(toolBin, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	stub := "#!/bin/sh\nprintf ok > " + strconv.Quote(marker) + "\n"
	if err := os.WriteFile(filepath.Join(toolBin, "fakelint"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	minimalPath := strings.Join([]string{
		filepath.Dir(goPath),
		filepath.Dir(bashPath),
		"/usr/bin",
		"/bin",
	}, string(os.PathListSeparator))

	root := repoRoot(t)
	cmd := exec.Command("bash", filepath.Join(root, ".validator", "go-offline.sh"), "fakelint")
	cmd.Dir = root
	cmd.Env = append(withoutHostLauncherEnv(os.Environ()),
		"HOME="+home,
		"PATH="+minimalPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go-offline.sh fakelint: %v\n%s", err, output)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v\n%s", err, output)
	}
	if string(got) != "ok" {
		t.Fatalf("marker = %q, want ok", got)
	}
}
