//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
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
	request := &Request{AuditRunID: "audit", AuditSessionDir: filepath.Join(root, "audit"), SourceSessionDir: filepath.Join(root, "source"), SnapshotPath: snapshot, SourceRunID: "source", ExecutionSessionID: "session", SourceWorkflow: "core:example", Trigger: "automatic", Auditor: AgentProvenance{CLI: "claude", Model: "fable"}}
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

func TestCrosscheckPromptTravelsOnStdinWithinLinuxArgumentLimit(t *testing.T) {
	const linuxMaxArgStrlen = 128 * 1024
	large := strings.Repeat("x", 2*linuxMaxArgStrlen)
	for _, cliName := range []string{"claude", "codex"} {
		t.Run(cliName, func(t *testing.T) {
			var adapterArgs []string
			if cliName == "claude" {
				adapterArgs = []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--disallowedTools", "AskUserQuestion", "--", large}
			} else {
				adapterArgs = []string{"codex", "exec", "--json", large}
			}
			structured, _, cleanup, err := withCrosscheckOutputSchema(cliName, adapterArgs, t.TempDir(), "value", map[string]any{"type": "object"})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			argv, stdin := crosscheckPromptOnStdin(cliName, structured)
			if stdin != large {
				t.Fatalf("prompt did not move to stdin")
			}
			for _, arg := range argv {
				if len(arg) > linuxMaxArgStrlen {
					t.Fatalf("argv string of %d bytes exceeds the Linux limit", len(arg))
				}
			}
			if cliName == "claude" {
				last := argv[len(argv)-2:]
				if last[0] != "--json-schema" || !json.Valid([]byte(last[1])) {
					t.Fatalf("claude argv must end with the schema flag, got %q", last)
				}
				for _, arg := range argv {
					if arg == "--" {
						t.Fatalf("claude argv keeps a flag terminator without a prompt: %q", argv)
					}
				}
			} else if argv[len(argv)-1] != "-" {
				t.Fatalf("codex argv must read instructions from stdin, got %q", argv)
			}
		})
	}
}

func TestClaudeCrosscheckSchemaPrecedesPromptSeparator(t *testing.T) {
	args := []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--", "judge this"}
	structured, _, cleanup, err := withCrosscheckOutputSchema("claude", args, t.TempDir(), "value", map[string]any{"type": "object"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	n := len(structured)
	if structured[n-4] != "--json-schema" || structured[n-2] != "--" || structured[n-1] != "judge this" {
		t.Fatalf("schema flag must precede the prompt separator: %q", structured)
	}
}

func TestCrosscheckSendsPromptOnStdin(t *testing.T) {
	for _, stage := range []string{"value", "correctness"} {
		t.Run(stage, func(t *testing.T) {
			request, pkg := crosscheckFixture(t)
			output := `{"candidates":[]}`
			if stage == "value" {
				output = `{"batch_id":"` + pkg.BatchID + `","observations":[]}`
			}
			stdinFile := filepath.Join(t.TempDir(), "stdin")
			original := crosscheckCommand
			t.Cleanup(func() { crosscheckCommand = original })
			crosscheckCommand = func(args []string, workspace, outputDir string) (*exec.Cmd, error) {
				response := `{"type":"result","subtype":"success","is_error":false,"result":"done","structured_output":` + output + `}`
				return exec.Command("sh", "-c", `cat > "$1"; printf '%s' "$2"`, "stub", stdinFile, response), nil
			}
			var err error
			if stage == "value" {
				_, err = invokeCrosscheckValueBatch(request, pkg)
			} else {
				_, err = invokeCrosscheckCorrectness(request)
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(stdinFile)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) == 0 {
				t.Fatalf("%s prompt was not written to stdin", stage)
			}
		})
	}
}

func TestValuePackagesKeepOutputSchemaWithinArgumentLimit(t *testing.T) {
	leaves := make([]LeafEvidence, 400)
	for i := range leaves {
		leaves[i] = LeafEvidence{
			Skeleton: ObservationSkeleton{ObservationID: fmt.Sprintf("observation-%04d", i), StepID: fmt.Sprintf("step-%04d", i)},
			Evidence: []EvidenceReference{{ID: fmt.Sprintf("evidence-%04d", i), Category: "git", Status: "available"}},
		}
	}
	packages, err := buildValuePackages(leaves)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) < 2 {
		t.Fatalf("expected small leaves to split by schema size, got %d package", len(packages))
	}
	total := 0
	for _, pkg := range packages {
		total += len(pkg.Leaves)
		schema, err := json.Marshal(valueOutputSchema(pkg))
		if err != nil {
			t.Fatal(err)
		}
		if len(schema) > defaultSchemaBytes {
			t.Fatalf("%s schema is %d bytes", pkg.BatchID, len(schema))
		}
	}
	if total != len(leaves) {
		t.Fatalf("packages hold %d of %d leaves", total, len(leaves))
	}
}

func TestClaudeCrosscheckTempDirectoryIsInsideWritableOutput(t *testing.T) {
	request, _ := crosscheckFixture(t)
	adapter, err := cli.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(request.AuditSessionDir, "model-output")
	env, cleanup, err := cliEnvironment(adapter, request, nil, t.TempDir(), outputDir)
	if err != nil {
		t.Fatal(err)
	}
	temp := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "TMPDIR="); ok {
			temp = value
		}
	}
	if !strings.HasPrefix(temp, outputDir+string(filepath.Separator)) {
		t.Fatalf("Claude TMPDIR = %q, want below %q", temp, outputDir)
	}
	if info, err := os.Stat(temp); err != nil || !info.IsDir() {
		t.Fatalf("Claude TMPDIR missing: %v", err)
	}
	cleanup()
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("Claude TMPDIR survived cleanup: %v", err)
	}
}
