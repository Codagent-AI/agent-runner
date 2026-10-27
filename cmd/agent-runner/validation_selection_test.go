package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// E2E-001: a bad validation_workflow selection is rejected while the run is
// prepared, before any step runs or any run directory is created.
func TestPrepareFreshRunValidatesValidationWorkflowSelection(t *testing.T) {
	absMissing := filepath.Join(t.TempDir(), "missing-v1.0.yaml")
	for _, tc := range []struct {
		name      string
		selection string // empty leaves the param unset
		wantErr   string // empty means the run prepares
	}{
		{"unset", "", ""},
		{"none", "none", ""},
		{"agent-validator", "agent-validator", ""},
		{"unknown name", "foo", "foo"},
		{"incompatible builtin", "builtin:core/verify-change-v1.0.yaml", "verify-change-v1.0.yaml"},
		{"missing absolute path", absMissing, "missing-v1.0.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			project := t.TempDir()
			t.Chdir(project)
			sessionDir := filepath.Join(project, "session")
			keyed := map[string]string{
				"change_name":                     "demo",
				"change_dir":                      "openspec/changes/demo",
				"change_label":                    "OpenSpec change",
				"change_kind":                     "openspec",
				"artifact_validation_instruction": "x",
			}
			if tc.selection != "" {
				keyed["validation_workflow"] = tc.selection
			}

			h, err := prepareFreshRun(&freshRunRequest{
				SourceRef: "builtin:core/implement-change-v3.0.yaml", Keyed: keyed, SessionDir: sessionDir,
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("prepareFreshRun: %v", err)
				}
				if h.SessionDir != sessionDir {
					t.Errorf("SessionDir = %q, want %q", h.SessionDir, sessionDir)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want one mentioning %q", err, tc.wantErr)
			}
			if _, statErr := os.Stat(sessionDir); statErr == nil {
				t.Errorf("session dir %s exists after a rejected selection", sessionDir)
			}
		})
	}
}
