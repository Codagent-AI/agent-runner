//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func crosscheckFixture(t *testing.T) (*Request, ValuePackage) {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.Type()&os.ModeSymlink == 0 {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	snapshot := filepath.Join(root, "snapshot")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := metrics.Artifact{SchemaVersion: metrics.SchemaVersion, RunID: "source", Workflow: "core:example", Sessions: []metrics.SessionRecord{{ExecutionSessionID: "session"}}, Steps: []metrics.StepRecord{{Prefix: "[implement]", ID: "implement", Kind: "step", Type: "agent", Outcome: "failed", ExecutionSessionID: "session"}}}
	if err := stateio.WriteJSONAtomic(filepath.Join(snapshot, metrics.FileName), artifact); err != nil {
		t.Fatal(err)
	}
	request := &Request{AuditRunID: "audit", AuditSessionDir: filepath.Join(root, "audit"), SourceSessionDir: filepath.Join(root, "source"), SnapshotPath: snapshot, SourceRunID: "source", ExecutionSessionID: "session", SourceWorkflow: "core:example", Trigger: "automatic", Crosscheck: AgentProvenance{CLI: "claude", Model: "fable"}}
	prepared, err := PrepareEvidence(*request)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	return request, prepared.Packages[0]
}

func stubCrosscheck(t *testing.T, response, stderr, exitCode string, check func([]string)) {
	t.Helper()
	original := crosscheckCommand
	t.Cleanup(func() { crosscheckCommand = original })
	crosscheckCommand = func(args []string, workspace, output string) (*exec.Cmd, error) {
		if check != nil {
			check(args)
		}
		return exec.Command("sh", "-c", `printf '%s' "$1"; printf '%s' "$2" >&2; exit "$3"`, "stub", response, stderr, exitCode), nil
	}
}

func TestClaudeCrosscheckConsumesStructuredResultForBothStages(t *testing.T) {
	for _, stage := range []string{"value", "correctness"} {
		t.Run(stage, func(t *testing.T) {
			request, pkg := crosscheckFixture(t)
			output := `{"candidates":[]}`
			if stage == "value" {
				output = `{"batch_id":"` + pkg.BatchID + `","observations":[]}`
			}
			// The result prose is deliberately invalid as audit JSON. Only structured_output is authoritative.
			response := `{"type":"result","subtype":"success","is_error":false,"result":"The audit is complete","structured_output":` + output + `}`
			stubCrosscheck(t, response, "", "0", func(args []string) {
				schema := ""
				for i, arg := range args {
					if arg == "--verbose" {
						t.Error("Claude JSON audit must not request verbose transcript output")
					}
					if arg == "--output-format" && args[i+1] != "json" {
						t.Error("Claude audit must request one JSON result")
					}
					if arg == "--json-schema" && i+1 < len(args) {
						schema = args[i+1]
					}
				}
				if !json.Valid([]byte(schema)) {
					t.Errorf("Claude invocation missing JSON schema")
				}
			})
			if stage == "value" {
				_, err := invokeCrosscheckValueBatch(request, pkg)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := invokeCrosscheckCorrectness(request)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCrosscheckFailuresRetainRedactedProviderDiagnostics(t *testing.T) {
	for _, stage := range []string{"value", "correctness"} {
		for _, code := range []string{"0", "1"} {
			t.Run(stage+code, func(t *testing.T) {
				request, pkg := crosscheckFixture(t)
				stubCrosscheck(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"You've hit your session limit"}`, "provider unavailable token=private-token", code, nil)
				var err error
				if stage == "value" {
					_, err = invokeCrosscheckValueBatch(request, pkg)
				} else {
					_, err = invokeCrosscheckCorrectness(request)
				}
				if err == nil || !strings.Contains(err.Error(), "session limit") {
					t.Fatalf("lost provider diagnostic: %v", err)
				}
				if code == "1" && !strings.Contains(err.Error(), "provider unavailable [redacted]") {
					t.Fatalf("lost stderr diagnostic: %v", err)
				}
				if strings.Contains(err.Error(), "private-token") {
					t.Fatalf("unredacted diagnostic: %v", err)
				}
			})
		}
	}
}

func TestAuditFailureWarningNamesFailedStage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	request, _ := crosscheckFixture(t)
	if err := stateio.WriteState(&model.RunState{}, request.SourceSessionDir); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.SourceSessionDir, lifecycleFileName), Lifecycle{Version: 1, SourceRunID: request.SourceRunID, Links: []Link{{AuditRunID: request.AuditRunID}}}); err != nil {
		t.Fatal(err)
	}
	stubCrosscheck(t, `{"type":"result","is_error":true,"result":"You've hit your session limit"}`, "", "1", nil)
	if err := executeAuditWorkflow(request); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := ReadLifecycle(filepath.Join(request.SourceSessionDir, lifecycleFileName))
	if err != nil {
		t.Fatal(err)
	}
	warning := lifecycle.Links[0].Warning
	if !strings.Contains(warning, "value-audit") || !strings.Contains(warning, "session limit") {
		t.Fatalf("misleading audit warning: %s", warning)
	}
}
