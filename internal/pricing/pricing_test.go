package pricing

import (
	"encoding/json"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestEmbeddedPricesAndArithmetic(t *testing.T) {
	table, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	zero, write := int64(0), int64(20000)
	tests := []struct {
		name, model string
		requests    []model.RequestUsage
		want        float64
		long        int
	}{
		{"sol", "gpt-6.1-sol", nil, .98, 0},
		{"luna", "gpt-6-luna", nil, .115, 0},
		{"bands", "gpt-6.1-sol", []model.RequestUsage{{Input: 100000, Output: 1000, CacheWrite: &zero}, {Input: 300000, Cached: 200000, Output: 2000, CacheWrite: &zero}}, .68, 1},
		{"writes", "gpt-6.1-sol", []model.RequestUsage{{Input: 100000, Cached: 40000, CacheWrite: &write}}, .134, 0},
		{"reasoning included in output", "gpt-6.1-sol", []model.RequestUsage{{Output: 50000, CacheWrite: &zero}}, .5, 0},
		{"threshold", "gpt-6-luna", []model.RequestUsage{{Input: 272000, CacheWrite: &zero}}, .0272, 0},
		{"above threshold", "gpt-6-luna", []model.RequestUsage{{Input: 272001, CacheWrite: &zero}}, .0544002, 1},
	}
	for i := 0; i < 10; i++ {
		tests[0].requests = append(tests[0].requests, model.RequestUsage{Input: 100000, Cached: 80000, Output: 5000, CacheWrite: &zero})
	}
	for i := 0; i < 25; i++ {
		tests[1].requests = append(tests[1].requests, model.RequestUsage{Input: 80000, Cached: 60000, Output: 4000, CacheWrite: &zero})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := table.Lookup("openai", tt.model)
			if !ok {
				t.Fatal("missing model")
			}
			got := Estimate(entry, tt.requests)
			if got.Reason != "" || got.AmountUSD != tt.want || got.LongContextRequests != tt.long {
				t.Fatalf("got %+v, want %v", got, tt.want)
			}
		})
	}
	if _, ok := table.Lookup("openai", "gpt-6.1-sol-preview"); ok {
		t.Fatal("fuzzy match")
	}
	entry, _ := table.Lookup("openai", "gpt-6.1-sol")
	if got := Estimate(entry, []model.RequestUsage{{Input: 1}}); len(got.Limitations) != 1 {
		t.Fatal(got)
	}
	if got := Estimate(entry, []model.RequestUsage{{Input: 1, Cached: 2}}); got.Reason != model.UnavailableRequestEvidenceMismatch {
		t.Fatal(got)
	}
}

func TestPriceTableValidationAndExactAliases(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*Table)
	}{
		{"source", func(t *Table) { t.Entries[0].Source = "" }},
		{"date", func(t *Table) { t.Entries[0].EffectiveDate = "" }},
		{"threshold", func(t *Table) { t.Entries[0].ThresholdInputTokens = 0 }},
		{"rate", func(t *Table) { t.Entries[0].Short.CacheWrite = 0 }},
		{"long below short", func(t *Table) { t.Entries[0].Long.Output = 1 }},
		{"duplicate model", func(t *Table) { t.Entries = append(t.Entries, t.Entries[0]) }},
		{"duplicate alias", func(t *Table) { t.Entries[0].Aliases = []string{t.Entries[1].Model} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var table Table
			if err := json.Unmarshal(prices, &table); err != nil {
				t.Fatal(err)
			}
			tt.change(&table)
			if table.validate() == nil {
				t.Fatal("accepted invalid table")
			}
		})
	}
	var table Table
	_ = json.Unmarshal(prices, &table)
	table.Entries[0].Aliases = []string{"explicit"}
	if err := table.validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"explicit", "gpt-6.1-sol"} {
		if _, ok := table.Lookup("openai", name); !ok {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"Explicit", "explicit-preview", " gpt-6.1-sol"} {
		if _, ok := table.Lookup("openai", name); ok {
			t.Fatal(name)
		}
	}
}
