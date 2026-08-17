package intakeroute

import (
	"strings"
	"testing"
)

func TestValidateRequiresHandoffTextInRouteRequest(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"}}`)

	_, err := Validate(testValidateOptions(runDir, ""))
	if err == nil || !strings.Contains(err.Error(), "route handoff is required") {
		t.Fatalf("Validate() error = %v, want required handoff failure", err)
	}
}

func TestValidateSealsHandoffTextFromRouteRequest(t *testing.T) {
	runDir := t.TempDir()
	writeRequest(t, runDir, `{"workflow":"build","params":{"change_name":"intake"},"handoff":"agreed context"}`)

	prepared, err := Validate(testValidateOptions(runDir, ""))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	defer prepared.Discard()
	if got := prepared.Sealed().Handoff; got != "agreed context" {
		t.Fatalf("sealed handoff = %q, want agreed context", got)
	}

	store := NewStore(runDir)
	if err := store.Stage(prepared); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	sealed, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := sealed.Handoff; got != "agreed context" {
		t.Fatalf("published handoff = %q, want agreed context", got)
	}
}
