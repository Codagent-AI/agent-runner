package metrics

import (
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestExternalUserTurnAttribution(t *testing.T) {
	c := &Collector{baselines: map[string]model.TokenCounts{}, totalBaselines: map[string]model.TokenTotals{}}
	identity := model.ExecutionIdentity{CLI: "codex", SessionID: "session"}
	input := model.UsageRecord{CLI: "codex", Turns: []model.UsageRecord{
		{Status: model.UsageCollected, CLI: "codex", RawCumulative: model.TokenCounts{model.TokenInput: 10}},
		{Status: model.UsageUnavailable, CLI: "codex", Reason: model.UnavailableNoUsageEvent},
		{Status: model.UsageCollected, CLI: "codex", RawCumulative: model.TokenCounts{model.TokenInput: 30}},
		{Status: model.UsageCollected, CLI: "codex", RawCumulative: model.TokenCounts{model.TokenInput: 35}},
	}}
	got := c.attribute(&identity, &input)
	if got.Status != model.UsageCollected || got.Completeness != model.CompletenessPartial || got.Tokens[model.TokenInput] != 15 {
		t.Fatalf("usage = %+v", got)
	}
}
