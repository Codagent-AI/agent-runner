package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The collector runs through the same bundled shell entry point as a workflow step.
func TestCIWaitGreenSnapshot(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ci-wait.sh", "ci_wait.py"} {
		data, err := ReadAsset("core/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stub := `#!/bin/sh
case "$*" in
  "pr view --json number,url") echo '{"number":12,"url":"https://github.com/example/project/pull/12"}' ;;
  "api graphql"*) cat <<'JSON'
{"data":{"repository":{"pullRequest":{"url":"https://github.com/example/project/pull/12","headRefOid":"abcdef123456","mergeable":"MERGEABLE","author":{"login":"alice","__typename":"User"},"timelineItems":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"headRef":{"target":{"checkSuites":{"nodes":[],"pageInfo":{"hasNextPage":false}},"statusCheckRollup":{"contexts":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}},"reviews":{"nodes":[],"pageInfo":{"hasPreviousPage":false}},"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"comments":{"nodes":[],"pageInfo":{"hasPreviousPage":false}}}}}}
JSON
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(dir, "ci-wait.sh"))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader(`{"deadline_seconds":"2","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("collector: %v\n%s", err, out)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(out)), "CI_PASSED") {
		t.Fatalf("collector output: %s", out)
	}
}
