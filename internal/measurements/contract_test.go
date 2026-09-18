package measurements

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestPinnedCanonicalFixtures(t *testing.T) {
	b, err := os.ReadFile("testdata/validator-v1/fixture-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]json.RawMessage
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"cases", "semantic_cases", "export_cases", "protocol_cases"} {
		var cases []struct {
			Name      string `json:"name"`
			Original  string `json:"original_json"`
			Canonical string `json:"canonical_utf8"`
			Digest    string `json:"expected_digest"`
		}
		if err = json.Unmarshal(manifest[group], &cases); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				raw, err := os.ReadFile("testdata/validator-v1/" + c.Original)
				if err != nil {
					t.Fatal(err)
				}
				got, err := CanonicalRecord(raw)
				if c.Name == "duplicate-key-rejected" {
					if err == nil {
						t.Fatal("duplicate accepted")
					}
					return
				}
				if c.Canonical == "" {
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile("testdata/validator-v1/" + c.Canonical)
				if err != nil {
					t.Fatal(err)
				}
				if group == "export_cases" {
					want, err = CanonicalRecord(want)
					if err != nil {
						t.Fatal(err)
					}
				}
				if !bytes.Equal(got, bytes.TrimSpace(want)) {
					t.Fatal("canonical bytes mismatch")
				}
				if fmt.Sprintf("%x", sha256.Sum256(got)) != c.Digest {
					t.Fatal("digest mismatch")
				}
			})
		}
	}
}

func TestRecordContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/validator-v1/fixtures/partial-token-export.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := ValidateRecord(raw, "runner", "fixture-context")
	if err != nil {
		t.Fatal(err)
	}
	if r.Payload.Tokens["normalized_total"].Availability != "partial" {
		t.Fatal("lost partial evidence")
	}
	for _, field := range []string{"prompt", "future_optional"} {
		t.Run(field, func(t *testing.T) {
			var value map[string]any
			_ = json.Unmarshal(raw, &value)
			value["payload"].(map[string]any)[field] = "canary"
			altered, _ := json.Marshal(value)
			if _, err := ValidateRecord(altered, "runner", "fixture-context"); err == nil {
				t.Fatal("unknown field accepted")
			}
		})
	}
	if _, err := ValidateRecord(raw, "runner", "other"); err == nil {
		t.Fatal("scope mismatch accepted")
	}
}

func TestCanonicalPreservesLiteralEscapedUnicode(t *testing.T) {
	raw := []byte(`{"value":"\\u2028","separator":"\u2028"}`)
	b, err := CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err = json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &after); err != nil {
		t.Fatalf("canonicalization corrupted a literal backslash escape: %v", err)
	}
	x, _ := json.Marshal(before)
	y, _ := json.Marshal(after)
	if !bytes.Equal(x, y) {
		t.Fatal("canonicalization changed string value")
	}
}

func TestRecordRejectsProhibitedFreeFormEvidence(t *testing.T) {
	raw, err := os.ReadFile("testdata/validator-v1/fixtures/partial-token-export.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	value["payload"].(map[string]any)["tokens"].(map[string]any)["input_total"].(map[string]any)["derivation"] = "credential=private-canary"
	raw, _ = json.Marshal(value)
	canonical, err := CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	value["digest"].(map[string]any)["value"] = fmt.Sprintf("%x", sha256.Sum256(canonical))
	raw, _ = json.Marshal(value)
	if _, err := ValidateRecord(raw, "runner", "fixture-context"); err == nil {
		t.Fatal("prohibited free-form evidence retained")
	}
}
