package exec

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func TestCaptureScriptOutput(t *testing.T) {
	tests := []struct {
		name            string
		format          string
		stdout          string
		want            model.CapturedValue
		wantErrContains string
	}{
		{name: "empty format preserves whitespace", format: "", stdout: "hi\n", want: model.NewCapturedString("hi\n")},
		{name: "text format preserves whitespace", format: "text", stdout: "  spaced  ", want: model.NewCapturedString("  spaced  ")},
		{name: "json array", format: "json", stdout: `["claude","codex"]`, want: model.NewCapturedList([]string{"claude", "codex"})},
		{name: "json array trims before parse", format: "json", stdout: "  [\"a\"]\n", want: model.NewCapturedList([]string{"a"})},
		{name: "json object", format: "json", stdout: `{"cli":"codex"}`, want: model.NewCapturedMap(map[string]string{"cli": "codex"})},
		{name: "json array non-string element", format: "json", stdout: `["a",1]`, wantErrContains: "array contains non-string at index 1"},
		{name: "json object non-string field", format: "json", stdout: `{"k":1}`, wantErrContains: `object field "k" is not a string`},
		{name: "json scalar rejected", format: "json", stdout: `42`, wantErrContains: "must be an array of strings or object of strings"},
		{name: "invalid json", format: "json", stdout: `{not json`, wantErrContains: "script json capture:"},
		{name: "invalid utf-8", format: "json", stdout: string([]byte{'"', 0xff, '"'}), wantErrContains: "was not valid UTF-8"},
		{name: "exceeds size cap", format: "json", stdout: strings.Repeat("a", 1024*1024+1), wantErrContains: "exceeds 1 MiB"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := captureScriptOutput(tc.format, tc.stdout)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("captureScriptOutput() error = nil, want error containing %q", tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("captureScriptOutput() error = %q, want substring %q", err.Error(), tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("captureScriptOutput() returned error: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("captured mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
