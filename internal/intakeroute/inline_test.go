package intakeroute

import (
	"strings"
	"testing"
)

func TestInlineHandoffValueReturnsContentsWithinBound(t *testing.T) {
	raw := []byte("goal: add a flag\ndecision: reuse the existing parser\n")

	got := InlineHandoffValue(raw, "/runs/abc/intake-handoff.md")

	if got != string(raw) {
		t.Fatalf("expected contents verbatim, got %q", got)
	}
}

func TestInlineHandoffValueEmptyContentsStayEmpty(t *testing.T) {
	if got := InlineHandoffValue(nil, "/runs/abc/intake-handoff.md"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestInlineHandoffValueTruncatesAtLineBoundary(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	raw := []byte(strings.Repeat(line, (MaxInlineHandoffBytes/len(line))+20))
	path := "/runs/abc/intake-handoff.md"

	got := InlineHandoffValue(raw, path)

	if len(got) >= len(raw) {
		t.Fatalf("expected truncation, got %d bytes from %d", len(got), len(raw))
	}
	if !strings.Contains(got, path) {
		t.Fatalf("expected marker naming %q, got tail %q", path, tail(got))
	}
	body, marker, ok := strings.Cut(got, "\n[")
	if !ok {
		t.Fatalf("expected a bracketed truncation marker, got tail %q", tail(got))
	}
	if !strings.HasSuffix(body, "\n"+strings.TrimSuffix(line, "\n")) && body != "" {
		t.Fatalf("expected the kept portion to end on a whole line, got tail %q", tail(body))
	}
	if strings.Contains(body, "\n\n") {
		t.Fatalf("expected no partial line before the marker, got tail %q", tail(body))
	}
	if !strings.Contains(marker, "truncated") {
		t.Fatalf("expected the marker to say the handoff was truncated, got %q", marker)
	}
	if len(body) > MaxInlineHandoffBytes {
		t.Fatalf("kept portion %d exceeds bound %d", len(body), MaxInlineHandoffBytes)
	}
}

func TestInlineHandoffValueTruncatesContentWithNoNewline(t *testing.T) {
	raw := []byte(strings.Repeat("y", MaxInlineHandoffBytes+500))
	path := "/runs/abc/intake-handoff.md"

	got := InlineHandoffValue(raw, path)

	if len(got) >= len(raw) {
		t.Fatalf("expected truncation, got %d bytes", len(got))
	}
	if !strings.Contains(got, path) {
		t.Fatalf("expected marker naming %q, got tail %q", path, tail(got))
	}
}

func TestMaxInlineHandoffBytesIsFarBelowDurabilityBound(t *testing.T) {
	if MaxInlineHandoffBytes >= MaxHandoffBytes {
		t.Fatalf("inline bound %d must be smaller than durability bound %d", MaxInlineHandoffBytes, MaxHandoffBytes)
	}
	if MaxInlineHandoffBytes != 8<<10 {
		t.Fatalf("expected the inline bound to be 8 KiB, got %d", MaxInlineHandoffBytes)
	}
}

func tail(s string) string {
	if len(s) <= 200 {
		return s
	}
	return "..." + s[len(s)-200:]
}
