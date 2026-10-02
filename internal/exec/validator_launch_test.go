package exec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

const capabilitiesReply = `{"ok":true,"operation":"capabilities","protocol_version":1,"capabilities_version":1,"producer":{"name":"agent-validator"},"protocol_versions":[1],"measurement_schema_versions":[1],"operations":["export","acknowledge"],"limits":{"maximum_export_count":100,"maximum_export_bytes":1000000}}`

func fakeCapabilitiesExecutable(t *testing.T, reply string, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "validator")
	script := "#!/bin/sh\ncat <<'JSON'\n" + reply + "\nJSON\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProbeValidatorCapabilitiesAllowsAdditiveFields(t *testing.T) {
	reply := capabilitiesReply[:len(capabilitiesReply)-1] + `,"reviewer_override":{"supported":true}}`
	path := fakeCapabilitiesExecutable(t, reply, 0)
	if err := probeValidatorCapabilities(&validatorMetricsLaunch{Executable: path, Project: t.TempDir()}); err != nil {
		t.Fatalf("additive capabilities field rejected: %v", err)
	}
}

func TestValidatorProtocolDecodingKeepsClosedOperationsAndJSONGuards(t *testing.T) {
	var response struct {
		OK bool `json:"ok"`
	}
	if err := decodeValidatorProtocol([]byte(`{"ok":true,"future":1}`), &response, false); err != nil {
		t.Fatalf("additive reply rejected: %v", err)
	}
	if err := protocolDecode([]byte(`{"ok":true,"future":1}`), &response); err == nil {
		t.Fatal("closed reply accepted unknown field")
	}
	for _, raw := range []string{`{"ok":true,"ok":false}`, `{"ok":true,"future":1e9999}`} {
		if err := decodeValidatorProtocol([]byte(raw), &response, false); err == nil {
			t.Fatalf("invalid JSON accepted: %s", raw)
		}
	}
	if err := decodeValidatorProtocol(make([]byte, 4000001), &response, false); err == nil {
		t.Fatal("oversized reply accepted")
	}
}

func TestProbeValidatorCapabilitiesRejectsUnsupportedReplies(t *testing.T) {
	tests := map[string]string{
		"protocol":             `"protocol_version":1`,
		"capabilities version": `"capabilities_version":1`,
		"measurement schema":   `"measurement_schema_versions":[1]`,
		"missing export":       `"operations":["export","acknowledge"]`,
		"missing acknowledge":  `"operations":["export","acknowledge"]`,
		"small count":          `"maximum_export_count":100`,
		"small bytes":          `"maximum_export_bytes":1000000`,
	}
	replacements := map[string]string{
		"protocol":             `"protocol_version":2`,
		"capabilities version": `"capabilities_version":2`,
		"measurement schema":   `"measurement_schema_versions":[2]`,
		"missing export":       `"operations":["acknowledge"]`,
		"missing acknowledge":  `"operations":["export"]`,
		"small count":          `"maximum_export_count":99`,
		"small bytes":          `"maximum_export_bytes":999999`,
	}
	for name, old := range tests {
		t.Run(name, func(t *testing.T) {
			reply := []byte(capabilitiesReply)
			reply = []byte(stringReplace(string(reply), old, replacements[name]))
			path := fakeCapabilitiesExecutable(t, string(reply), 0)
			if err := probeValidatorCapabilities(&validatorMetricsLaunch{Executable: path, Project: t.TempDir()}); err == nil {
				t.Fatal("unsupported capabilities accepted")
			}
		})
	}
}

func TestBlockedValidatorContextsRetainIDsAndErrorCodes(t *testing.T) {
	tests := []struct {
		name, reply string
		exit        int
		missing     bool
		code        string
	}{
		{name: "missing executable", missing: true, code: "validator_executable_unavailable"},
		{name: "command failed", exit: 1, code: "capabilities_unavailable"},
		{name: "protocol unsupported", reply: stringReplace(capabilitiesReply, `"protocol_version":1`, `"protocol_version":2`), code: "capabilities_unsupported"},
		{name: "export missing", reply: stringReplace(capabilitiesReply, `"operations":["export","acknowledge"]`, `"operations":["acknowledge"]`), code: "capabilities_unsupported"},
		{name: "limits unsupported", reply: stringReplace(capabilitiesReply, `"maximum_export_count":100`, `"maximum_export_count":99`), code: "capabilities_limits_unsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "missing-validator")
			if !tc.missing {
				reply := tc.reply
				if reply == "" {
					reply = capabilitiesReply
				}
				executable = fakeCapabilitiesExecutable(t, reply, tc.exit)
			}
			t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", executable)
			collector := metrics.NewCollector(dir, "run", "workflow", time.Now())
			ctx := &model.ExecutionContext{SessionDir: dir, WorkingDir: dir, AuditLogger: metrics.NewPipeline(collector, nil)}
			for _, id := range []string{"validate-1", "validate-2"} {
				_, environment, err := prepareNestedMetricsEnvironment(&model.Step{ID: id, MetricsSource: "agent-validator"}, ctx)
				if err != nil || len(environment) != 0 {
					t.Fatalf("blocked launch affected validation: environment=%v error=%v", environment, err)
				}
			}
			raw, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
			if err != nil {
				t.Fatal(err)
			}
			var artifact metrics.Artifact
			if err := json.Unmarshal(raw, &artifact); err != nil {
				t.Fatal(err)
			}
			if len(artifact.ValidatorContexts) != 2 {
				t.Fatalf("contexts = %d, want 2", len(artifact.ValidatorContexts))
			}
			ids := map[string]bool{}
			for _, context := range artifact.ValidatorContexts {
				if context.Attribution.ContextID == "" || ids[context.Attribution.ContextID] {
					t.Fatalf("missing or repeated context ID: %+v", context.Attribution)
				}
				ids[context.Attribution.ContextID] = true
				want := []string{"instrumentation_unavailable", tc.code}
				if diff := cmp.Diff(want, context.Gaps); diff != "" {
					t.Fatalf("gaps mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func stringReplace(s, old, replacement string) string {
	return strings.Replace(s, old, replacement, 1)
}
