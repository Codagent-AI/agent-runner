package intakeroute

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/model"
)

func TestValidateRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		request string
		wantErr string
	}{
		{name: "malformed JSON", request: "{", wantErr: "decode route request"},
		{name: "unknown field", request: `{"workflow":"build","extra":true}`, wantErr: "decode route request"},
		{name: "unknown workflow", request: `{"workflow":"missing","handoff":"notes"}`, wantErr: `workflow "missing" not found`},
		{name: "missing required parameter", request: `{"workflow":"build","handoff":"notes"}`, wantErr: `missing required parameter "change_name"`},
		{name: "undeclared parameter", request: `{"workflow":"build","params":{"extra":"x","change_name":"x"},"handoff":"notes"}`, wantErr: `unexpected parameter "extra"`},
		{name: "missing handoff", request: `{"workflow":"build","params":{"change_name":"x"}}`, wantErr: "route handoff is required"},
		{name: "empty handoff", request: `{"workflow":"build","params":{"change_name":"x"},"handoff":""}`, wantErr: "route handoff is required"},
		{name: "oversized request", request: `{"workflow":"build","params":{"change_name":"` + strings.Repeat("x", MaxRequestBytes) + `"}}`, wantErr: "route request exceeds 64 KiB"},
		{name: "oversized handoff", request: `{"workflow":"build","params":{"change_name":"x"},"handoff":"` + strings.Repeat("x", MaxHandoffBytes+1) + `"}`, wantErr: "route handoff exceeds 8 KiB"},
		{name: "intake routes to itself", request: `{"workflow":"core:intake","handoff":"notes"}`, wantErr: "intake cannot route to itself"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDir := t.TempDir()
			requestPath := filepath.Join(runDir, "route-request.json")
			if err := os.WriteFile(requestPath, []byte(tt.request), 0o600); err != nil {
				t.Fatal(err)
			}

			opts := testValidateOptions(runDir, "")
			opts.RequestPath = requestPath
			_, err := Validate(opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
			assertNoRouteArtifacts(t, runDir)
		})
	}
}

func TestValidateLaunchSealedRejectsOversizedHandoff(t *testing.T) {
	sealed := &Sealed{
		State: Frozen, ParentRunID: "parent", Workflow: "build",
		SourceRef: "builtin:core/build-v2.0.yaml", Params: map[string]string{},
		Handoff:  strings.Repeat("x", MaxHandoffBytes+1),
		StagedAt: "2026-08-12T00:00:00Z", FrozenAt: "2026-08-12T00:00:01Z",
	}

	err := ValidateLaunchSealed(sealed)
	if err == nil || !strings.Contains(err.Error(), "handoff exceeds 8 KiB") {
		t.Fatalf("ValidateLaunchSealed() error = %v, want oversized handoff rejection", err)
	}
}

func TestValidateStagesExactResolvedRouteAndSealsHandoff(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"},"handoff":"original handoff"}`)

	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	defer prepared.Discard()
	if got, want := prepared.Sealed().SourceRef, "builtin:core/build-v2.0.yaml"; got != want {
		t.Fatalf("SourceRef = %q, want %q", got, want)
	}
	if got, want := prepared.Sealed().Params, map[string]string{"change_name": "intake"}; !mapsEqual(got, want) {
		t.Fatalf("Params = %#v, want %#v", got, want)
	}

	store := NewStore(runDir)
	if err := store.Stage(prepared); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	sealed, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if sealed.State != Staged {
		t.Fatalf("State = %q, want %q", sealed.State, Staged)
	}
	if got := sealed.Handoff; got != "original handoff" {
		t.Fatalf("sealed handoff = %q, want original handoff", got)
	}
}

func TestValidateReturnsStructuredErrors(t *testing.T) {
	tests := []struct {
		name    string
		request string
		want    string
	}{
		{name: "decoding", request: `{`, want: "decode"},
		{name: "resolution", request: `{"workflow":"missing","handoff":"notes"}`, want: "workflow_resolution"},
		{name: "parameters", request: `{"workflow":"build","handoff":"notes"}`, want: "parameter"},
		{name: "missing handoff", request: `{"workflow":"build","params":{"change_name":"x"}}`, want: "handoff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDir := t.TempDir()
			writeRequest(t, runDir, tt.request)
			_, err := Validate(testValidateOptions(runDir, "handoff.md"))
			structured, ok := err.(interface{ ViolationCode() string })
			if !ok {
				t.Fatalf("Validate() error = %T %v, want structured validation error", err, err)
			}
			if got := structured.ViolationCode(); got != tt.want {
				t.Fatalf("ViolationCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPreparedSealedCopyCannotChangePublishedParams(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"},"handoff":"notes"}`)
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")
	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	sealedCopy := prepared.Sealed()
	sealedCopy.Params["unexpected"] = "must not leak"
	if err := NewStore(runDir).Stage(prepared); err != nil {
		t.Fatal(err)
	}
	sealed, err := NewStore(runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sealed.Params, map[string]string{"change_name": "intake"}; !mapsEqual(got, want) {
		t.Fatalf("published Params = %#v, want %#v", got, want)
	}
}

func TestPreparedDiscardLeavesRunUnchanged(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"},"handoff":"notes"}`)
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")

	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := prepared.Discard(); err != nil {
		t.Fatalf("Discard() error = %v", err)
	}
	assertNoRouteArtifacts(t, runDir)
}

func TestRejectedOrUnpublishableRoutePreservesExistingSidecar(t *testing.T) {
	runDir := t.TempDir()
	store := NewStore(runDir)
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"first"},"handoff":"first notes"}`)
	writeFile(t, filepath.Join(runDir, "handoff.md"), "first notes")
	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(runDir, "intake-route.json"))

	writeRequest(t, runDir, `{"workflow":"missing","handoff":"notes"}`)
	if _, err := Validate(testValidateOptions(runDir, "handoff.md")); err == nil {
		t.Fatal("Validate() error = nil, want rejection")
	}
	if got := readFile(t, filepath.Join(runDir, "intake-route.json")); got != before {
		t.Fatalf("sidecar after rejected request = %s, want %s", got, before)
	}

	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"replacement"},"handoff":"replacement notes"}`)
	prepared, err = Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	broken := &Store{path: runDir}
	if err := broken.Stage(prepared); err == nil {
		t.Fatal("Stage() error = nil, want publication failure")
	}
	if got := readFile(t, filepath.Join(runDir, "intake-route.json")); got != before {
		t.Fatalf("sidecar after publication failure = %s, want %s", got, before)
	}
	assertNoTemporarySnapshots(t, runDir)
}

func TestStoreReplacesStagedRouteButNeverFrozenRoute(t *testing.T) {
	runDir := t.TempDir()
	store := NewStore(runDir)
	stage := func(handoff, contents string) {
		writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"`+handoff+`"},"handoff":"`+contents+`"}`)
		prepared, err := Validate(testValidateOptions(runDir, handoff+".md"))
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		if err := store.Stage(prepared); err != nil {
			t.Fatalf("Stage() error = %v", err)
		}
	}
	stage("first", "first notes")
	stage("second", "second notes")
	sealed, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := sealed.Handoff; got != "second notes" {
		t.Fatalf("replacement handoff = %q", got)
	}
	if err := store.Freeze(); err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}
	if err := store.Freeze(); err != nil {
		t.Fatalf("second Freeze() error = %v", err)
	}
	stagePrepared, err := Validate(testValidateOptions(runDir, "second.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer stagePrepared.Discard()
	if err := store.Stage(stagePrepared); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Stage() error = %v, want ErrFrozen", err)
	}
	frozen, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if frozen.State != Frozen || frozen.FrozenAt == "" {
		t.Fatalf("frozen route = %#v, want frozen with timestamp", frozen)
	}
}

func TestStoreStageIsIdempotentForPublishedPreparedRoute(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"},"handoff":"notes"}`)
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")
	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(runDir)
	if err := store.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(runDir, "intake-route.json"))
	if err := store.Stage(prepared); err != nil {
		t.Fatalf("second Stage() error = %v", err)
	}
	if got := readFile(t, filepath.Join(runDir, "intake-route.json")); got != before {
		t.Fatalf("sidecar after idempotent Stage = %s, want %s", got, before)
	}
}

func TestValidateUnknownWorkflowListsRoutableWorkflows(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")
	writeRequest(t, runDir, `{"workflow":"guess","handoff":"notes"}`)

	opts := testValidateOptions(runDir, "handoff.md")
	opts.Catalog = NewCatalog([]discovery.WorkflowEntry{
		{CanonicalName: "spec-driven:change", SourcePath: "builtin:spec-driven/change-v2.0.yaml", Description: "Define, plan, and implement a change."},
		{CanonicalName: "core:debug", SourcePath: "builtin:core/debug-v1.0.yaml", Description: "Debug a failed run."},
		{CanonicalName: "core:intake", SourcePath: "builtin:core/intake-v1.0.yaml", Hidden: true},
		{CanonicalName: "core:finalize-pr", SourcePath: "builtin:core/finalize-pr-v1.0.yaml", Hidden: true},
		{CanonicalName: "broken", SourcePath: "project:broken.yaml", ParseError: "bad yaml"},
	})

	_, err := Validate(opts)
	if err == nil {
		t.Fatal("Validate() error = nil, want workflow resolution failure")
	}
	want := strings.Join([]string{
		`workflow "guess" not found; routable workflows:`,
		"  core:debug - Debug a failed run.",
		"  spec-driven:change - Define, plan, and implement a change.",
	}, "\n")
	if got := err.Error(); got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
	assertNoRouteArtifacts(t, runDir)
}

func TestRenderCatalogDescribesOnlyRoutableWorkflows(t *testing.T) {
	catalog := NewCatalog([]discovery.WorkflowEntry{
		{CanonicalName: "spec-driven:change", SourcePath: "builtin:spec-driven/change-v2.0.yaml",
			Description: "Define, plan, and implement a change.",
			Params:      []model.Param{{Name: "change_name"}, {Name: "base_branch", Required: boolPtr(false)}}},
		{CanonicalName: "core:debug", SourcePath: "builtin:core/debug-v1.0.yaml",
			Description: "Debug a failed run.",
			Params:      []model.Param{{Name: "failed_run_id", Required: boolPtr(false)}}},
		{CanonicalName: "local", SourcePath: "project:local.yaml"},
		{CanonicalName: "core:intake", SourcePath: "builtin:core/intake-v1.0.yaml", Description: "Plan with an agent.", Hidden: true},
		{CanonicalName: "core:finalize-pr", SourcePath: "builtin:core/finalize-pr-v1.0.yaml", Description: "Finalize a PR.", Hidden: true},
		{CanonicalName: "broken", SourcePath: "project:broken.yaml", ParseError: "bad yaml"},
	})

	want := strings.Join([]string{
		"# Workflows you can route to",
		"",
		"Put the canonical name in the route request's `workflow` field. Supply every required",
		"parameter and no parameter that is not listed here.",
		"",
		"## core:debug",
		"Debug a failed run.",
		"Required parameters: none",
		"Optional parameters: failed_run_id",
		"",
		"## local",
		"Required parameters: none",
		"Optional parameters: none",
		"",
		"## spec-driven:change",
		"Define, plan, and implement a change.",
		"Required parameters: change_name",
		"Optional parameters: base_branch",
		"",
	}, "\n")
	if got := RenderCatalog(catalog, "core:intake"); got != want {
		t.Fatalf("RenderCatalog() =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteCatalogPublishesRunOwnedCatalogFile(t *testing.T) {
	runDir := t.TempDir()
	opts := testValidateOptions(runDir, "handoff.md")

	path, err := WriteCatalog(opts)
	if err != nil {
		t.Fatalf("WriteCatalog() error = %v", err)
	}
	if want := CatalogPathFor(runDir); path != want {
		t.Fatalf("WriteCatalog() path = %q, want %q", path, want)
	}
	if got, want := readFile(t, path), RenderCatalog(opts.Catalog, opts.IntakeWorkflow); got != want {
		t.Fatalf("catalog file =\n%s\nwant\n%s", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("catalog mode = %v, want %v", got, want)
	}
}

// The agent writes its handoff and route request into the run directory, so it
// can also replace the catalog path with a symbolic link before a retried
// attempt republishes it. Publishing must not follow that link out of the run.
func TestWriteCatalogDoesNotFollowASymlinkAtItsPath(t *testing.T) {
	runDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "precious.txt")
	writeFile(t, outside, "untouched")
	if err := os.Symlink(outside, CatalogPathFor(runDir)); err != nil {
		t.Fatal(err)
	}

	path, err := WriteCatalog(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatalf("WriteCatalog() error = %v", err)
	}
	if got := readFile(t, outside); got != "untouched" {
		t.Fatalf("file outside the run directory = %q, want it untouched", got)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("catalog path mode = %v, want a regular file replacing the symlink", info.Mode())
	}
	if got, want := readFile(t, path), RenderCatalog(testCatalog(), "core:intake"); got != want {
		t.Fatalf("catalog file =\n%s\nwant\n%s", got, want)
	}
}

func TestValidateIgnoresThePublishedCatalogFile(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")
	writeRequest(t, runDir, `{"workflow":"guess","handoff":"notes"}`)
	// The agent can write anywhere in the run directory, so the published
	// catalog is advisory only. Validation must resolve against the real
	// catalog, never against this file.
	writeFile(t, CatalogPathFor(runDir), "# Workflows you can route to\n\n## guess\nAnything I like.\n")

	if _, err := Validate(testValidateOptions(runDir, "handoff.md")); err == nil {
		t.Fatal("Validate() error = nil, want the forged catalog entry rejected")
	} else if !strings.Contains(err.Error(), `workflow "guess" not found`) {
		t.Fatalf("Validate() error = %v, want workflow resolution failure", err)
	}
}

func TestValidateUnknownWorkflowTruncatesLongRoutableList(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")
	writeRequest(t, runDir, `{"workflow":"guess","handoff":"notes"}`)

	entries := make([]discovery.WorkflowEntry, 0, maxListedWorkflows+3)
	for i := 0; i < maxListedWorkflows+3; i++ {
		name := fmt.Sprintf("user:w%03d", i)
		entries = append(entries, discovery.WorkflowEntry{CanonicalName: name, SourcePath: "user:" + name + ".yaml"})
	}
	opts := testValidateOptions(runDir, "handoff.md")
	opts.Catalog = NewCatalog(entries)

	_, err := Validate(opts)
	if err == nil {
		t.Fatal("Validate() error = nil, want workflow resolution failure")
	}
	if got := err.Error(); !strings.HasSuffix(got, "\n  (+3 more)") {
		t.Fatalf("Validate() error = %q, want a truncated routable list", got)
	}
	if got, notWant := err.Error(), fmt.Sprintf("user:w%03d", maxListedWorkflows); strings.Contains(got, notWant) {
		t.Fatalf("Validate() error = %q, want it to omit %q", got, notWant)
	}
}

func TestValidateUnknownWorkflowOmitsListWhenNothingIsRoutable(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "handoff.md"), "notes")
	writeRequest(t, runDir, `{"workflow":"guess","handoff":"notes"}`)

	opts := testValidateOptions(runDir, "handoff.md")
	opts.Catalog = NewCatalog([]discovery.WorkflowEntry{
		{CanonicalName: "core:intake", SourcePath: "builtin:core/intake-v1.0.yaml", Hidden: true},
	})

	_, err := Validate(opts)
	if err == nil {
		t.Fatal("Validate() error = nil, want workflow resolution failure")
	}
	if got, want := err.Error(), `workflow "guess" not found`; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func testCatalog() Catalog {
	return NewCatalog([]discovery.WorkflowEntry{
		{CanonicalName: "build", SourcePath: "builtin:core/build-v2.0.yaml", Params: []model.Param{{Name: "change_name"}, {Name: "optional", Required: boolPtr(false)}}},
		{CanonicalName: "core:intake", SourcePath: "builtin:core/intake-v1.0.yaml", Hidden: true},
	})
}

func testValidateOptions(runDir, handoff string) *ValidateOptions {
	_ = handoff
	return &ValidateOptions{
		RunDir: runDir, ParentRunID: "intake-run", IntakeWorkflow: "core:intake",
		Catalog: testCatalog(),
	}
}
func writeRequest(t *testing.T, runDir, request string) {
	t.Helper()
	writeFile(t, filepath.Join(runDir, "route-request.json"), request)
}
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func boolPtr(v bool) *bool { return &v }
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func assertNoRouteArtifacts(t *testing.T, runDir string) {
	t.Helper()
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "intake-route.json" || strings.HasPrefix(entry.Name(), ".intake-route-") {
			t.Fatalf("unexpected route artifact %q", entry.Name())
		}
	}
}

func assertNoTemporarySnapshots(t *testing.T, runDir string) {
	t.Helper()
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".intake-route-") {
			t.Fatalf("unexpected temporary route artifact %q", entry.Name())
		}
	}
}

func TestLoadStrictRejectsUnknownFields(t *testing.T) {
	runDir := t.TempDir()
	path := filepath.Join(runDir, "intake-route.json")
	if err := os.WriteFile(path, []byte(`{"state":"frozen","parent_run_id":"intake","workflow":"target","source_ref":"builtin:core/target-v1.0.yaml","params":{},"handoff":"context","staged_at":"now","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadStrict(path)
	if err == nil || !strings.Contains(err.Error(), "decode intake route sidecar") {
		t.Fatalf("LoadStrict() error = %v, want strict decode error", err)
	}
}
