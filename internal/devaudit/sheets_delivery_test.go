//go:build dev_audit

package devaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// INT-005 exercises isolated credential import and source-file independence.
func TestINT005ConnectionStoreImportsOnlyRefreshMaterialWithPrivateModes(t *testing.T) {
	home := t.TempDir()
	client := filepath.Join(t.TempDir(), "client.json")
	token := filepath.Join(t.TempDir(), "token.json")
	writeJSON(t, client, map[string]any{"installed": map[string]any{
		"client_id": "client-id", "client_secret": "client-secret", "token_uri": "https://token.example.test/token",
	}})
	writeJSON(t, token, map[string]any{
		"client_id": "client-id", "client_secret": "client-secret", "refresh_token": "refresh-secret",
		"scopes": []string{"https://www.googleapis.com/auth/spreadsheets"}, "token": "discarded-access-token",
	})

	store := ConnectionStore{Home: home, allowInsecureTokenURI: true}
	if err := store.Import(SetupInput{ClientPath: client, TokenPath: token, SpreadsheetID: "sheet-id", Tab: "audit"}); err != nil {
		t.Fatalf("import connection: %v", err)
	}
	parent, err := os.Stat(filepath.Join(home, ".agent-runner"))
	if err != nil {
		t.Fatal(err)
	}
	if got := parent.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent mode = %o, want 0700", got)
	}
	recordPath := filepath.Join(home, ".agent-runner", connectionFileName)
	recordInfo, err := os.Stat(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := recordInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("record mode = %o, want 0600", got)
	}
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || bytes.Equal(data, mustRead(t, client)) || bytes.Equal(data, mustRead(t, token)) {
		t.Fatalf("connection record was not a copied projection: %s", data)
	}
	if containsAny(string(data), client, token, "discarded-access-token") {
		t.Fatalf("connection record leaked source paths or access token: %s", data)
	}
}

func TestConnectionStoreAcceptsSpaceDelimitedAuthorizedUserScopes(t *testing.T) {
	client := filepath.Join(t.TempDir(), "client.json")
	token := filepath.Join(t.TempDir(), "token.json")
	writeJSON(t, client, map[string]any{"installed": map[string]any{"client_id": "client", "client_secret": "secret", "token_uri": "https://token.example.test/token"}})
	writeJSON(t, token, map[string]any{"client_id": "client", "client_secret": "secret", "refresh_token": "refresh", "scopes": "openid https://www.googleapis.com/auth/spreadsheets"})
	if err := (ConnectionStore{Home: t.TempDir(), allowInsecureTokenURI: true}).Import(SetupInput{ClientPath: client, TokenPath: token, SpreadsheetID: "sheet", Tab: "tab"}); err != nil {
		t.Fatalf("import authorized-user scopes string: %v", err)
	}
}

func TestConnectionStoreRejectsUnprotectedConnectionRecord(t *testing.T) {
	store := ConnectionStore{Home: t.TempDir(), allowInsecureTokenURI: true}
	if err := store.Write(&Connection{SpreadsheetID: "sheet", Tab: "tab", ClientID: "client", ClientSecret: "secret", TokenURI: "https://token.example.test/token", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	path, err := store.path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); err == nil {
		t.Fatal("world-readable connection record was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); err == nil {
		t.Fatal("world-readable connection parent was accepted")
	}
}

func TestConnectionStoreRejectsNonGoogleTokenEndpoint(t *testing.T) {
	store := ConnectionStore{Home: t.TempDir()}
	err := store.Write(&Connection{SpreadsheetID: "sheet", Tab: "tab", ClientID: "client", ClientSecret: "secret", TokenURI: "http://attacker.example.test/token", RefreshToken: "refresh"})
	if err == nil {
		t.Fatal("non-Google HTTP OAuth endpoint was accepted")
	}
}

func TestA1RangeQuotesWorksheetNames(t *testing.T) {
	if got, want := a1Range("Reviewer's audit! 2026", "B:B"), "'Reviewer''s audit! 2026'!B:B"; got != want {
		t.Fatalf("quoted A1 range = %q, want %q", got, want)
	}
}

func TestProjectFromRemoteDoesNotExportLocalRemoteParents(t *testing.T) {
	if got, want := projectFromRemote("/Users/alice/private-repo.git", "/work/local-project"), "local-project"; got != want {
		t.Fatalf("project from local remote = %q, want %q", got, want)
	}
}

func TestReporterLockReturnsWhenContextExpires(t *testing.T) {
	store := ConnectionStore{Home: t.TempDir(), allowInsecureTokenURI: true}
	reporter := SheetsReporter{Store: store}
	destination := DestinationState{SpreadsheetID: "sheet", Tab: "tab"}
	unlock, err := reporter.lock(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := reporter.lock(ctx, destination); err == nil {
		t.Fatal("second reporter lock ignored context expiration")
	}
}

func TestMigrateReportDestinationRequiresExplicitDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-report.json")
	writeJSON(t, path, LocalReport{SchemaVersion: valueSchemaVersion, Destination: DestinationState{State: "configured", SpreadsheetID: "old", Tab: "old-tab"}, DeliveryState: "pending"})
	if err := MigrateReportDestination(dir, "new-sheet", "new-tab"); err != nil {
		t.Fatalf("migrate report destination: %v", err)
	}
	var report LocalReport
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if got, want := report.Destination, (DestinationState{State: "configured", SpreadsheetID: "new-sheet", Tab: "new-tab"}); got != want {
		t.Fatalf("migrated destination = %#v, want %#v", got, want)
	}
}

// INT-006 exercises direct OAuth/Sheets projection, frozen destinations, and
// ambiguous append idempotency against an in-memory HTTP boundary.
func TestINT006DeliverReportUsesExactAllowlistAndDeduplicatesAmbiguousAppend(t *testing.T) {
	var writes [][]string
	const sheetHeader = "schema_version,observation_id,observed_at_utc,project,workflow,source_run_id,execution_session_id,audit_run_id,trigger,source_outcome,step_id,step_outcome,lineage,duration_ms,cost_usd,total_tokens,source_models,git_attribution,commit_shas,files_changed,lines_added,lines_deleted,overall_value,change_effect,unique_contribution,downstream_evidence,confidence,evidence_coverage,judge_model,rubric_version,note"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer"})
		case r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/sheet/values/'audit'!1:1":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]string{strings.Split(sheetHeader, ",")}})
		case r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/sheet/values/'audit'!B:B":
			ids := [][]string{{"observation_id"}}
			for _, row := range writes {
				ids = append(ids, []string{row[1]})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": ids})
		case r.Method == http.MethodPost:
			var body struct {
				Values [][]string `json:"values"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode append: %v", err)
			}
			writes = append(writes, body.Values...)
			// Simulate Google committing the append before the response disappears.
			http.Error(w, "lost response", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	store := ConnectionStore{Home: t.TempDir(), allowInsecureTokenURI: true}
	if err := store.Write(&Connection{SpreadsheetID: "sheet", Tab: "audit", ClientID: "client", ClientSecret: "secret", TokenURI: server.URL + "/token", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	report := LocalReport{AuditRunID: "audit", DeliveryState: "pending", Destination: DestinationState{State: "configured", SpreadsheetID: "sheet", Tab: "audit"}, Values: ValueValidationResult{Observations: []ValueObservation{{
		ObservationSkeleton: ObservationSkeleton{SchemaVersion: valueSchemaVersion, ObservationID: "obs-1", ObservedAtUTC: "2026-09-01T00:00:00Z", Project: "Codagent-AI/agent-runner", Workflow: "core:example", SourceRunID: "source", ExecutionSessionID: "session", AuditRunID: "audit", Trigger: "automatic", SourceOutcome: "success", StepID: "build", StepOutcome: "success", Lineage: "new", Cost: CostEvidence{SourceModels: []string{"z", "a"}}, Git: GitEvidence{Attribution: "no_change"}},
		OverallValue:        "high", ChangeEffect: "intended", UniqueContribution: "unique", DownstreamEvidence: "confirmed", Confidence: "high", EvidenceCoverage: "complete", JudgeModel: "judge", RubricVersion: rubricVersion, Note: "High-level result.",
	}}}}
	// Reconfiguration applies to future audits only: this completed report must
	// continue to use the destination it froze at local-report assembly.
	if err := store.Write(&Connection{SpreadsheetID: "new-sheet", Tab: "new-tab", ClientID: "client", ClientSecret: "secret", TokenURI: server.URL + "/token", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}

	reporter := SheetsReporter{Store: store, HTTPClient: server.Client(), SheetsBaseURL: server.URL + "/v4"}
	if err := reporter.Deliver(context.Background(), &report); err != nil {
		t.Fatalf("ambiguous append was not resolved by observation-ID recheck: %v", err)
	}
	if got := len(writes); got != 1 {
		t.Fatalf("writes after ambiguous response = %d, want 1", got)
	}
	if err := reporter.Deliver(context.Background(), &report); err != nil {
		t.Fatalf("retry delivery: %v", err)
	}
	if got := len(writes); got != 1 {
		t.Fatalf("retry duplicated row: %d", got)
	}
	if got, want := writes[0], expectedDeliveryRow(); !reflect.DeepEqual(got, want) {
		t.Fatalf("row = %#v\nwant %#v", got, want)
	}
}

func TestProjectFromRemoteStripsHostProtocolCredentialsAndLocalPaths(t *testing.T) {
	if got, want := projectFromRemote("https://user:secret@github.com/Codagent-AI/agent-runner.git?private=yes", "/Users/private/worktree"), "Codagent-AI/agent-runner"; got != want {
		t.Fatalf("project from remote = %q, want %q", got, want)
	}
	if got, want := projectFromRemote("", "/Users/private/worktree"), "worktree"; got != want {
		t.Fatalf("project without remote = %q, want %q", got, want)
	}
}

func expectedDeliveryRow() []string {
	return []string{"step_value_v1", "obs-1", "2026-09-01T00:00:00Z", "Codagent-AI/agent-runner", "core:example", "source", "session", "audit", "automatic", "success", "build", "success", "new", "", "", "", "a,z", "no_change", "", "", "", "", "high", "intended", "unique", "confirmed", "high", "complete", "judge", "value-rubric-v1", "High-level result."}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && len(needle) <= len(value) && contains(value, needle) {
			return true
		}
	}
	return false
}
func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
