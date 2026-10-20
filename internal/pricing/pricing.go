// Package pricing values observed requests using an embedded Standard-tier policy.
package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sync"

	"github.com/codagent/agent-runner/internal/model"
)

//go:embed prices.json
var prices []byte

type Rates struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cached_input"`
	CacheWrite  float64 `json:"cache_write"`
	Output      float64 `json:"output"`
}
type Entry struct {
	Provider             string   `json:"provider"`
	Model                string   `json:"model"`
	Aliases              []string `json:"aliases"`
	EffectiveDate        string   `json:"effective_date"`
	Source               string   `json:"source"`
	Tier                 string   `json:"tier"`
	ThresholdInputTokens int64    `json:"threshold_input_tokens"`
	Short                Rates    `json:"short"`
	Long                 Rates    `json:"long"`
}
type Table struct {
	Version string  `json:"version"`
	Policy  string  `json:"policy"`
	Entries []Entry `json:"entries"`
}

var load = sync.OnceValues(func() (*Table, error) {
	var t Table
	if err := json.Unmarshal(prices, &t); err != nil {
		return nil, err
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return &t, nil
})

func Load() (*Table, error) { return load() }
func (t *Table) validate() error {
	if t.Version == "" || t.Policy != "openai-standard-request-level" || len(t.Entries) == 0 {
		return fmt.Errorf("invalid price table metadata")
	}
	names := map[string]bool{}
	for i := range t.Entries {
		e := &t.Entries[i]
		if e.Provider == "" || e.Model == "" || e.Source == "" || e.EffectiveDate == "" || e.Tier != "standard" || e.ThresholdInputTokens <= 0 {
			return fmt.Errorf("invalid price entry %q", e.Model)
		}
		for _, name := range append([]string{e.Model}, e.Aliases...) {
			key := e.Provider + "/" + name
			if name == "" || names[key] {
				return fmt.Errorf("duplicate or empty price name %q", key)
			}
			names[key] = true
		}
		short, long := e.Short.values(), e.Long.values()
		for i, v := range short {
			if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) || long[i] < v || math.IsNaN(long[i]) || math.IsInf(long[i], 0) {
				return fmt.Errorf("invalid rates for %s", e.Model)
			}
		}
	}
	return nil
}
func (r Rates) values() []float64 { return []float64{r.Input, r.CachedInput, r.CacheWrite, r.Output} }
func (t *Table) Lookup(provider, name string) (Entry, bool) {
	for i := range t.Entries {
		e := &t.Entries[i]
		if e.Provider != provider {
			continue
		}
		if e.Model == name || slices.Contains(e.Aliases, name) {
			return *e, true
		}
	}
	return Entry{}, false
}

type Result struct {
	AmountUSD           float64
	Requests            int
	LongContextRequests int
	Limitations         []string
	Reason              model.UnavailableReason
}

//nolint:gocritic // An entry is an immutable price snapshot passed by value.
func Estimate(entry Entry, requests []model.RequestUsage) Result {
	result := Result{Requests: len(requests)}
	// Arbitrary precision integers prevent overflow on large runs as well as float drift.
	sum := new(big.Int)
	unobserved := false
	short, long := entry.Short.microRates(), entry.Long.microRates()
	for _, r := range requests {
		write := int64(0)
		if r.CacheWrite == nil {
			unobserved = true
		} else {
			write = *r.CacheWrite
		}
		if r.Input < 0 || r.Cached < 0 || write < 0 || r.Output < 0 || r.Cached > r.Input || write > r.Input-r.Cached {
			return Result{Reason: model.UnavailableRequestEvidenceMismatch}
		}
		rates := short
		if r.Input > entry.ThresholdInputTokens {
			rates = long
			result.LongContextRequests++
		}
		for i, tokens := range []int64{r.Input - r.Cached - write, r.Cached, write, r.Output} {
			term := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(rates[i]))
			sum.Add(sum, term)
		}
	}
	result.AmountUSD, _ = new(big.Rat).SetFrac(sum, big.NewInt(1e12)).Float64()
	if unobserved {
		result.Limitations = []string{"cache_writes_unobserved"}
	}
	return result
}

func (r Rates) microRates() [4]int64 {
	var result [4]int64
	for i, v := range r.values() {
		result[i] = int64(math.Round(v * 1e6))
	}
	return result
}
