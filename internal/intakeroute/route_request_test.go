package intakeroute

import (
	"path/filepath"
	"strings"
	"testing"
)

// The runner tells the agent where to write the handoff and then validates
// against that same path, so a handoff field in the request could only ever
// echo a constant back. These tests pin the field out of the contract.

func TestValidateAcceptsRequestWithoutHandoffField(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"}}`)
	writeFile(t, filepath.Join(runDir, "handoff.md"), "agreed notes")

	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatalf("Validate() error = %v, want success without a handoff field", err)
	}
	defer prepared.Discard()

	if got, want := prepared.Sealed().SourceRef, "builtin:core/build-v2.0.yaml"; got != want {
		t.Fatalf("SourceRef = %q, want %q", got, want)
	}
}

func TestValidateSealsRunnerOwnedHandoffWithoutBeingToldWhere(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"}}`)
	writeFile(t, filepath.Join(runDir, "handoff.md"), "original handoff")

	prepared, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	defer prepared.Discard()

	store := NewStore(runDir)
	if err := store.Stage(prepared); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	writeFile(t, filepath.Join(runDir, "handoff.md"), "modified after sealing")

	sealed, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := readFile(t, sealed.HandoffPath); got != "original handoff" {
		t.Fatalf("snapshot = %q, want the handoff as it was at seal time", got)
	}
}

func TestValidateRejectsRequestCarryingHandoffField(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "handoff.md"), "agreed notes")
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"},"handoff":"handoff.md"}`)

	_, err := Validate(testValidateOptions(runDir, "handoff.md"))
	if err == nil {
		t.Fatal("Validate() succeeded, want rejection of the removed handoff field")
	}
	if !strings.Contains(err.Error(), "decode route request") {
		t.Fatalf("Validate() error = %v, want an unknown-field decode failure", err)
	}
	assertNoRouteArtifacts(t, runDir)
}
