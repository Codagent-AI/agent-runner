package builtinworkflows

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCIStatusGatesHandleIncompleteReview(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		marker      string
		wantCode    int
		wantMessage string
	}{
		{"status passes green CI", "ci-status-gate.sh", "CI_PASSED", 0, ""},
		{"status accepts incomplete review", "ci-status-gate.sh", "CI_REVIEW_INCOMPLETE", 0, ""},
		{"status rejects comments", "ci-status-gate.sh", "CI_COMMENTS", 1, ""},
		{"status rejects failed checks", "ci-status-gate.sh", "CI_FAILED", 1, ""},
		{"status rejects pending checks", "ci-status-gate.sh", "CI_PENDING", 1, ""},
		{"status rejects unknown marker", "ci-status-gate.sh", "UNKNOWN", 1, ""},
		{"fix gate skips incomplete review", "ci-fix-needed-gate.sh", "CI_REVIEW_INCOMPLETE", 0, ""},
		{"review gate warns on incomplete review", "ci-review-incomplete-gate.sh", "CI_REVIEW_INCOMPLETE", 1, "CI review gate: review bot did not complete review; finishing with warning"},
		{"review gate accepts passed", "ci-review-incomplete-gate.sh", "CI_PASSED", 0, ""},
		{"review gate accepts comments", "ci-review-incomplete-gate.sh", "CI_COMMENTS", 0, ""},
		{"review gate accepts failed checks", "ci-review-incomplete-gate.sh", "CI_FAILED", 0, ""},
		{"review gate accepts pending checks", "ci-review-incomplete-gate.sh", "CI_PENDING", 0, ""},
		{"review gate accepts unknown marker", "ci-review-incomplete-gate.sh", "UNKNOWN", 0, ""},
		{"review gate uses final non-empty line", "ci-review-incomplete-gate.sh", "CI_REVIEW_INCOMPLETE\nAdditional prose", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, err := ReadAsset("core/" + tt.script)
			if err != nil {
				t.Fatalf("ReadAsset(%s): %v", tt.script, err)
			}
			scriptPath := filepath.Join(t.TempDir(), tt.script)
			if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
				t.Fatalf("write script: %v", err)
			}

			cmd := exec.Command("sh", scriptPath)
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote("CI report\n"+tt.marker+"\n\n") + `}`)
			out, err := cmd.CombinedOutput()
			gotCode := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run script: %v", err)
				}
				gotCode = exitErr.ExitCode()
			}
			if gotCode != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; output: %s", gotCode, tt.wantCode, out)
			}
			if tt.wantMessage != "" && !strings.Contains(string(out), tt.wantMessage) {
				t.Fatalf("output = %q, want %q", out, tt.wantMessage)
			}
		})
	}
}

func TestCIReuseReportRequiresPassingMarkerAndCurrentFullHead(t *testing.T) {
	const head = "abcdef1234567890abcdef1234567890abcdef12"
	const other = "1234567890abcdef1234567890abcdef12345678"
	script, err := ReadAsset("core/ci-reuse-report.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ci-reuse-report.sh")
	if err := os.WriteFile(path, script, 0o700); err != nil {
		t.Fatal(err)
	}
	collector, err := ReadAsset("core/ci_wait.py")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ci_wait.py"), collector, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := ciFixture()
	ciPR(fixture)["headRefOid"] = head
	snapshot, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "gh")
	stubBody := `#!/bin/sh
[ "$CI_GH_FAIL" = 1 ] && exit 1
case "$*" in
  "pr view --json number,url,headRefOid")
    [ "$CI_GH_HANG" = head ] && exec sleep 10
    echo "{\"number\":12,\"url\":\"https://github.com/example/project/pull/12\",\"headRefOid\":\"$CI_HEAD\"}" ;;
  "pr view --json number,url") echo '{"number":12,"url":"https://github.com/example/project/pull/12"}' ;;
  "api graphql"*) [ "$CI_GH_HANG" = snapshot ] && exec sleep 10; cat "$CI_SNAPSHOT" ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(stubBody), 0o700); err != nil {
		t.Fatal(err)
	}
	noJQDir := filepath.Join(dir, "no-jq")
	if err := os.Mkdir(noJQDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"cat", "sed", "tail", "head", "python3", "dirname"} {
		found, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(found, filepath.Join(noJQDir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(stub, filepath.Join(noJQDir, "gh")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, report, current, bots string
		fail                        bool
		noJQ                        bool
		wantCode                    int
	}{
		{"passed current head", "**Head:** " + head + "\nCI_PASSED\n", head, "", false, false, 0},
		{"passed without jq", "**Head:** " + head + "\nCI_PASSED\n", head, "", false, true, 0},
		{"incomplete current head", "**Head:** " + head + "\nCI_REVIEW_INCOMPLETE\n", head, "coderabbitai", false, false, 0},
		{"changed head", "**Head:** " + head + "\nCI_PASSED\n", other, "", false, false, 1},
		{"failed report", "**Head:** " + head + "\nCI_FAILED\n", head, "", false, false, 1},
		{"missing report", "", head, "", false, false, 1},
		{"truncated head", "**Head:** abcdef123456\nCI_PASSED\n", head, "", false, false, 1},
		{"lookup failure", "**Head:** " + head + "\nCI_PASSED\n", head, "", true, false, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", path)
			searchPath := dir + ":" + os.Getenv("PATH")
			if tt.noJQ {
				searchPath = noJQDir
			}
			cmd.Env = append(os.Environ(), "PATH="+searchPath, "CI_HEAD="+tt.current, "CI_SNAPSHOT="+filepath.Join(dir, "snapshot.json"))
			if tt.fail {
				cmd.Env = append(cmd.Env, "CI_GH_FAIL=1")
			}
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote(tt.report) + `,"review_bots":` + strconv.Quote(tt.bots) + `}`)
			out, err := cmd.CombinedOutput()
			code := 0
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tt.wantCode {
				t.Fatalf("exit=%d want=%d output=%s", code, tt.wantCode, out)
			}
			if code == 0 && string(out) != tt.report {
				t.Fatalf("reused report=%q want=%q", out, tt.report)
			}
		})
	}
	for _, hang := range []string{"head", "snapshot"} {
		t.Run("stalled "+hang+" lookup is bounded", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", path)
			cmd.WaitDelay = 100 * time.Millisecond
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"),
				"CI_HEAD="+head, "CI_SNAPSHOT="+filepath.Join(dir, "snapshot.json"), "CI_GH_HANG="+hang)
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote("**Head:** "+head+"\nCI_PASSED\n") + `,"deadline_seconds":0.5,"call_timeout_seconds":0.2}`)
			start := time.Now()
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || time.Since(start) > time.Second || err == nil {
				t.Fatalf("verification was not bounded: elapsed=%s err=%v output=%s", time.Since(start), err, out)
			}
		})
	}
}
