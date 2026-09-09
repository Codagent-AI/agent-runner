package metrics

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/measurements"
)

func revisionFixture(t *testing.T, revision, total int) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("../measurements/testdata/validator-v1/fixtures/partial-token-export.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	value["revision"] = revision
	value["original_consumer_context"] = map[string]any{"consumer": "agent-runner", "context_id": "ctx"}
	p := value["payload"].(map[string]any)
	p["revision"] = revision
	token := p["tokens"].(map[string]any)["normalized_total"].(map[string]any)
	token["value"] = total
	token["availability"] = "available"
	token["reason"] = nil
	raw, _ = json.Marshal(value)
	b, err := measurements.CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	value["digest"].(map[string]any)["value"] = fmt.Sprintf("%x", sha256.Sum256(b))
	raw, _ = json.Marshal(value)
	return raw
}
func TestValidatorRevisionReplacesInsteadOfAddingWork(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "wf", time.Now())
	attr := Attribution{ContextID: "ctx", ExecutionSessionID: "original", StepID: "validate"}
	for _, revision := range []int{1, 2, 1, 2} {
		if err := c.IncorporateValidator(attr, "store", []json.RawMessage{revisionFixture(t, revision, revision*10)}, DeliveryContext{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.artifact.MeasurementHeads) != 1 || len(c.artifact.Steps) != 1 || c.Totals().TokenTotals.Total != 20 {
		t.Fatal("revision replay added work")
	}
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{revisionFixture(t, 2, 99)}, DeliveryContext{}); err == nil {
		t.Fatal("conflicting revision accepted")
	}
	if c.Totals().TokenTotals != nil {
		t.Fatal("conflicting head retained numeric contribution")
	}
}
func TestValidatorMissingExpectedAttemptsKeepsRecoveryIncomplete(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "wf", time.Now())
	attr := Attribution{ContextID: "ctx", ExecutionSessionID: "original", StepID: "validate"}
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{revisionFixture(t, 1, 18)}, DeliveryContext{Delivery: "complete", History: "complete", ScopeComplete: true}); err != nil {
		t.Fatal(err)
	}
	if c.artifact.ValidatorDelivery.Collection == "complete" {
		t.Fatal("attempt without invocation established complete collection")
	}
}

func TestPinnedValidatorSemanticProjections(t *testing.T) {
	for _, name := range []string{"two-model-allocation-cost", "overlapping-cost-scopes", "prepared-terminal-replacement", "requested-only-partial-approximate", "unknown-cost-currency-scope", "stable-allocation-reorder", "expanded-usage-old-cost", "conflicting-current-revisions", "partial-token-subtotal"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../measurements/testdata/validator-v1/fixtures/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Records  []map[string]any `json:"records"`
				Expected struct {
					AttemptCount int `json:"attempt_count"`
					Total        struct {
						Availability string   `json:"availability"`
						Value        *float64 `json:"value"`
					} `json:"normalized_total"`
					CompleteCost *bool  `json:"complete_attempt_cost"`
					ExportError  string `json:"export_error"`
				} `json:"expected"`
			}
			if err = json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			c := NewCollector(t.TempDir(), "run", "wf", time.Now())
			attr := Attribution{ContextID: "ctx", ExecutionSessionID: "original", StepID: "validate"}
			for _, payload := range fixture.Records {
				if payload["consumer_context"] != nil {
					payload["consumer_context"] = map[string]any{"consumer": "agent-runner", "context_id": "ctx"}
				}
				record := map[string]any{"record_type": payload["record_type"], "record_id": payload["attempt_id"], "revision": payload["revision"], "measurement_schema_version": payload["measurement_schema_version"], "producer": map[string]any{"name": "agent-validator", "version": "fixture-only"}, "original_consumer_context": map[string]any{"consumer": "agent-runner", "context_id": "ctx"}, "payload": payload}
				raw, _ = json.Marshal(record)
				canonical, err := measurements.CanonicalRecord(raw)
				if err != nil {
					t.Fatal(err)
				}
				record["digest"] = map[string]any{"algorithm": "sha256", "canonicalization": "rfc8785", "value": fmt.Sprintf("%x", sha256.Sum256(canonical))}
				raw, _ = json.Marshal(record)
				err = c.IncorporateValidator(attr, "store", []json.RawMessage{raw}, DeliveryContext{})
				if err != nil && fixture.Expected.ExportError == "" {
					t.Fatal(err)
				}
			}
			if len(c.artifact.Steps) != fixture.Expected.AttemptCount {
				t.Fatalf("dispatch count=%d want=%d", len(c.artifact.Steps), fixture.Expected.AttemptCount)
			}
			total := c.artifact.MeasurementTotals["normalized_total"]
			if total.Availability != fixture.Expected.Total.Availability {
				t.Fatalf("availability=%s want=%s", total.Availability, fixture.Expected.Total.Availability)
			}
			if fixture.Expected.Total.Value != nil && total.KnownSubtotal != *fixture.Expected.Total.Value {
				t.Fatalf("subtotal=%v want=%v", total.KnownSubtotal, *fixture.Expected.Total.Value)
			}
			if fixture.Expected.CompleteCost != nil && !*fixture.Expected.CompleteCost && c.Totals().EstimatedAPICostUSD != nil {
				t.Fatal("scoped or partial cost became complete USD")
			}
		})
	}
}

func TestUnsupportedCurrentScopeDoesNotProjectOlderCompatibleHead(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "wf", time.Now())
	attr := Attribution{ContextID: "ctx", ExecutionSessionID: "original", StepID: "validate"}
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{revisionFixture(t, 1, 18)}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if err := c.IncorporateValidator(attr, "store", nil, DeliveryContext{Delivery: "blocked", Gaps: []string{"unsupported_measurement_version"}}); err != nil {
		t.Fatal(err)
	}
	if len(c.artifact.Steps) != 0 || c.Totals().TokenTotals != nil {
		t.Fatal("unsupported current scope projected an older compatible attempt")
	}
	if len(c.artifact.MeasurementHeads) != 1 || c.artifact.ValidatorDelivery.Delivery != "partial" {
		t.Fatal("unsupported scope lost historical evidence or its gap")
	}
}
