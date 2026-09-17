package measurements

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed testdata/validator-v1/export-record.schema.json
var envelopeSchema []byte

//go:embed testdata/validator-v1/model-attempt.schema.json
var attemptSchema []byte

//go:embed invocation-v1.schema.json
var invocationSchema []byte

var schemas = sync.OnceValues(func() (map[string]*jsonschema.Resolved, error) {
	result := map[string]*jsonschema.Resolved{}
	for name, raw := range map[string][]byte{"envelope": envelopeSchema, "model_attempt": attemptSchema, "invocation": invocationSchema} {
		var schema jsonschema.Schema
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, err
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, err
		}
		result[name] = resolved
	}
	return result, nil
})

type Value struct {
	Availability string   `json:"availability"`
	Value        *float64 `json:"value"`
	Reason       *string  `json:"reason"`
	Source       *string  `json:"source"`
	Origin       *string  `json:"origin"`
	Precision    *string  `json:"precision"`
	Derivation   *string  `json:"derivation"`
	IncludedIn   []string `json:"included_in"`
}
type StringEvidence struct {
	Availability string  `json:"availability"`
	Value        *string `json:"value"`
	Reason       *string `json:"reason"`
}
type Identity struct {
	Adapter    *string `json:"adapter"`
	Model      *string `json:"model"`
	Provider   *string `json:"provider"`
	Effort     *string `json:"effort"`
	Provenance string  `json:"provenance"`
}
type ObservedIdentity struct {
	ID         string         `json:"identity_id"`
	Model      string         `json:"model"`
	Provider   StringEvidence `json:"provider"`
	Effort     StringEvidence `json:"effort"`
	Provenance string         `json:"provenance"`
}
type Lifecycle struct {
	State     string  `json:"state"`
	StartedAt *string `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
}
type Cost struct {
	ID           string         `json:"cost_evidence_id"`
	Amount       Value          `json:"amount"`
	Currency     StringEvidence `json:"currency"`
	Scope        string         `json:"scope"`
	AllocationID string         `json:"allocation_id,omitempty"`
	Coverage     string         `json:"coverage"`
	Overlap      string         `json:"overlap"`
	Source       string         `json:"source"`
}
type Context struct {
	Consumer  string `json:"consumer"`
	ContextID string `json:"context_id"`
}
type Record struct {
	Type     string `json:"record_type"`
	ID       string `json:"record_id"`
	Revision int64  `json:"revision"`
	Version  int    `json:"measurement_schema_version"`
	Producer struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"producer"`
	Context Context `json:"original_consumer_context"`
	Payload Payload `json:"payload"`
	Digest  struct {
		Algorithm        string `json:"algorithm"`
		Canonicalization string `json:"canonicalization"`
		Value            string `json:"value"`
	} `json:"digest"`
}

// Payload is a read view, not a serializer. The complete verified raw record is
// retained alongside attribution, including all allocations and provenance.
type Payload struct {
	Type         string             `json:"record_type"`
	AttemptID    string             `json:"attempt_id"`
	InvocationID string             `json:"invocation_id"`
	SessionID    string             `json:"session_id"`
	Revision     int64              `json:"revision"`
	Version      int                `json:"measurement_schema_version"`
	Lifecycle    Lifecycle          `json:"lifecycle"`
	Adapter      string             `json:"adapter"`
	Outcome      string             `json:"outcome"`
	AttemptIDs   []string           `json:"attempt_ids"`
	ZeroDispatch bool               `json:"zero_dispatch"`
	Requested    Identity           `json:"requested_identity"`
	Resolved     Identity           `json:"resolved_identity"`
	Observed     []ObservedIdentity `json:"observed_identities"`
	Tokens       map[string]Value   `json:"tokens"`
	Costs        []Cost             `json:"provider_reported_costs"`
	Completeness map[string]string  `json:"completeness"`
	Diagnostics  []string           `json:"diagnostics"`
	Context      *Context           `json:"consumer_context"`
}

var prohibitedEvidence = regexp.MustCompile(`(?i)(prompt|response|credential|password|api[_ -]?key|account|user|organization|email|host|machine)`)
var nativeNames = strings.Fields("input_tokens cached_input_tokens output_tokens cache_read_tokens cache_write_tokens reasoning_tokens total_tokens request_count provider_session_id reported_cost claude_otel_input claude_otel_output claude_otel_cacheRead claude_otel_cacheCreation opencode_inputTokens opencode_outputTokens opencode_reasoningTokens opencode_cacheReadTokens opencode_cacheWriteTokens gemini_inputTokens gemini_outputTokens gemini_thoughtTokens gemini_cacheTokens copilot_in copilot_out copilot_cache")

func ValidateRecord(raw []byte, consumer, contextID string) (Record, error) {
	var record Record
	value, err := Decode(raw)
	if err != nil {
		return record, err
	}
	contracts, err := schemas()
	if err != nil {
		return record, fmt.Errorf("contract_unavailable")
	}
	if contracts["envelope"].Validate(value) != nil {
		return record, fmt.Errorf("invalid_envelope")
	}
	if err = json.Unmarshal(raw, &record); err != nil {
		return record, fmt.Errorf("invalid_record")
	}
	if record.Context.Consumer != consumer || record.Context.ContextID != contextID {
		return record, fmt.Errorf("scope_mismatch")
	}
	if containsProhibitedEvidence(value) {
		return record, fmt.Errorf("prohibited_evidence")
	}
	payload := value.(map[string]any)["payload"]
	if contracts[record.Type].Validate(payload) != nil {
		return record, fmt.Errorf("invalid_payload")
	}
	p := record.Payload
	id := p.InvocationID
	if record.Type == "model_attempt" {
		id = p.AttemptID
	}
	if id != record.ID || p.Type != record.Type || p.Revision != record.Revision || p.Version != record.Version {
		return record, fmt.Errorf("envelope_payload_mismatch")
	}
	if p.Context != nil && *p.Context != record.Context {
		return record, fmt.Errorf("payload_scope_mismatch")
	}
	for _, stamp := range []*string{p.Lifecycle.StartedAt, p.Lifecycle.EndedAt} {
		if stamp != nil {
			if _, err = time.Parse(time.RFC3339Nano, *stamp); err != nil {
				return record, fmt.Errorf("invalid_timestamp")
			}
		}
	}
	for _, diagnostic := range p.Diagnostics {
		if prohibitedEvidence.MatchString(diagnostic) {
			return record, fmt.Errorf("prohibited_diagnostic")
		}
	}
	if record.Type == "model_attempt" {
		if err := validateReferences(payload.(map[string]any), &record); err != nil {
			return record, err
		}
	}
	canonical, err := CanonicalRecord(raw)
	if err != nil {
		return record, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(canonical)) != record.Digest.Value {
		return record, fmt.Errorf("digest_mismatch")
	}
	return record, nil
}
func validateReferences(raw map[string]any, record *Record) error {
	identities := map[string]bool{}
	allocations := map[string]bool{}
	for _, identity := range record.Payload.Observed {
		if identities[identity.ID] {
			return fmt.Errorf("duplicate_identity")
		}
		identities[identity.ID] = true
	}
	for _, entry := range raw["allocations"].([]any) {
		row := entry.(map[string]any)
		id := row["allocation_id"].(string)
		if allocations[id] || !identities[row["observed_identity_ref"].(string)] {
			return fmt.Errorf("invalid_allocation_reference")
		}
		allocations[id] = true
	}
	for i := range record.Payload.Costs {
		cost := &record.Payload.Costs[i]
		if cost.Scope == "allocation" && !allocations[cost.AllocationID] || cost.Scope != "allocation" && cost.AllocationID != "" {
			return fmt.Errorf("invalid_cost_reference")
		}
	}
	for _, token := range record.Payload.Tokens {
		if token.Origin != nil && *token.Origin == "derived" && (token.Derivation == nil || *token.Derivation == "") {
			return fmt.Errorf("missing_derivation")
		}
		for _, field := range token.IncludedIn {
			if field == "" {
				return fmt.Errorf("invalid_inclusion")
			}
		}
	}
	for _, entry := range raw["provider_native_usage"].([]any) {
		row := entry.(map[string]any)
		if !slices.Contains(nativeNames, row["name"].(string)) {
			return fmt.Errorf("native_evidence_not_allowlisted")
		}
		if n, ok := row["value"].(float64); ok && n < 0 {
			return fmt.Errorf("invalid_native_value")
		}
	}
	return nil
}

func containsProhibitedEvidence(value any) bool {
	switch v := value.(type) {
	case string:
		return prohibitedEvidence.MatchString(v)
	case map[string]any:
		for _, child := range v {
			if containsProhibitedEvidence(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsProhibitedEvidence(child) {
				return true
			}
		}
	}
	return false
}
