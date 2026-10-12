package agentcall

import "testing"

func TestFollowUpAndTimeoutValidation(t *testing.T) {
	for _, raw := range []string{`{"prompt":"x","follow_up":"c1","timeout":"1s"}`, `{"prompt":"x","agent":"a","timeout":"24h"}`} {
		if _, err := DecodeRequest([]byte(raw)); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"prompt":"x","follow_up":"c1","agent":"a"}`, `{"prompt":"x","follow_up":"c1","cli":"codex"}`, `{"prompt":"x","agent":"a","timeout":"999ms"}`, `{"prompt":"x","agent":"a","timeout":"25h"}`} {
		if _, err := DecodeRequest([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
