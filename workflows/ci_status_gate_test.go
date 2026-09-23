package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
