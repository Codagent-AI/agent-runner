package main

import "testing"

func TestVersionDefault(t *testing.T) {
	if version != "dev" {
		t.Errorf("expected default version %q, got %q", "dev", version)
	}
}
