package interactive

import (
	"os"
	"testing"
)

func TestProcessIdentityRejectsDifferentStartTime(t *testing.T) {
	identity, err := ReadProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ReadProcessIdentity: %v", err)
	}
	if ProcessIdentityMatches(os.Getpid(), identity+"-reused") {
		t.Fatal("different process start identity matched")
	}
	if !ProcessIdentityMatches(os.Getpid(), identity) {
		t.Fatal("current process identity did not match")
	}
}
