//go:build dev_audit

package devaudit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

// JudgeAttempt is written by Runner outside the judge's writable output tree.
type JudgeAttempt struct {
	AttemptID       string            `json:"attempt_id"`
	AuditRunID      string            `json:"audit_run_id"`
	Stage           string            `json:"stage"`
	BatchID         string            `json:"batch_id,omitempty"`
	CLI             string            `json:"cli"`
	Model           string            `json:"model"`
	Effort          string            `json:"reasoning_effort"`
	SessionID       string            `json:"session_id"`
	LaunchedAt      string            `json:"launched_at"`
	Outcome         string            `json:"outcome"`
	FailureCategory string            `json:"failure_category,omitempty"`
	Usage           model.UsageRecord `json:"usage"`
	CostUSD         *float64          `json:"estimated_api_cost_usd"`
	Legacy          bool              `json:"legacy,omitempty"`
}

type JudgeUsageSummary struct {
	CLI                   string                    `json:"cli"`
	Model                 string                    `json:"model"`
	Effort                string                    `json:"reasoning_effort"`
	Attempts              []JudgeAttempt            `json:"attempts"`
	AttemptCount          int                       `json:"attempt_count"`
	Tokens                map[string]*int64         `json:"tokens"`
	TokenCategoryCoverage map[string]model.Coverage `json:"token_category_coverage"`
	TotalTokens           *int64                    `json:"total_tokens"`
	TokenCoverage         model.Coverage            `json:"token_coverage"`
	CostUSD               *float64                  `json:"estimated_api_cost_usd"`
	CostCoverage          model.Coverage            `json:"cost_coverage"`
}

type judgeAttemptHandle struct {
	path   string
	record JudgeAttempt
}

var writeJudgeOutput = stateio.WriteJSONAtomic

func unavailableJudgeUsage(reason model.UnavailableReason) model.UsageRecord {
	return model.UsageRecord{Status: model.UsageUnavailable, Reason: reason, Source: "audit-judge"}
}

func judgeAttemptUsage(adapter cli.Adapter, raw []byte) (usageRecord model.UsageRecord, reportedCost *float64) {
	extractor, ok := adapter.(cli.UsageExtractor)
	if !ok {
		return unavailableJudgeUsage(model.UnavailableUnsupportedAdapter), nil
	}
	if len(raw) == 0 {
		return unavailableJudgeUsage(model.UnavailableNoUsageEvent), nil
	}
	extracted, err := extractor.ExtractUsage(string(raw))
	if err != nil {
		return unavailableJudgeUsage(model.UnavailableParseFailure), nil
	}
	usage := extracted.Usage
	if usage.Status != model.UsageCollected {
		return unavailableJudgeUsage(model.UnavailableNoUsageEvent), nil
	}
	if usage.Tokens == nil && usage.RawCumulative != nil {
		usage.Tokens = usage.RawCumulative
	}
	if usage.TokenTotals == nil {
		usage.TokenTotals = usage.RawCumulativeTokenTotals
	}
	cost := extracted.EstimatedCostUSD
	if cost == nil {
		cost = usage.RawCumulativeCostUSD
	}
	return usage, cost
}

func recordJudgeExit(request *Request, adapter cli.Adapter, stage, batchID string, raw []byte, workspace string, spawnTime time.Time) (*judgeAttemptHandle, error) {
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	// Correctness has no batch, so its empty segment drops out of the path.
	id := path.Join(stage, batchID, fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(idBytes)))
	recordPath := filepath.Join(request.AuditSessionDir, "judge-usage", filepath.FromSlash(id)+".json")
	record := baseJudgeAttempt(request, stage, batchID)
	record.AttemptID, record.Outcome = id, "exited"
	record.SessionID = auditSessionID(adapter, raw, workspace, spawnTime)
	record.LaunchedAt = spawnTime.UTC().Format(time.RFC3339Nano)
	record.Usage, record.CostUSD = judgeAttemptUsage(adapter, raw)
	if err := stateio.WriteJSONDurable(recordPath, record); err != nil {
		return nil, err
	}
	return &judgeAttemptHandle{path: recordPath, record: record}, nil
}

// baseJudgeAttempt carries the frozen judge identity shared by recorded,
// recovered, and synthesized attempts.
func baseJudgeAttempt(request *Request, stage, batchID string) JudgeAttempt {
	return JudgeAttempt{AuditRunID: request.AuditRunID, Stage: stage, BatchID: batchID, CLI: request.Auditor.CLI, Model: request.Auditor.Model, Effort: request.Auditor.Effort}
}

func judgeGroupKey(stage, batchID string) string { return stage + "/" + batchID }

func (h *judgeAttemptHandle) finish(outcome, category string) error {
	if h == nil {
		return nil
	}
	h.record.Outcome, h.record.FailureCategory = outcome, category
	return stateio.WriteJSONDurable(h.path, h.record)
}

func summarizeJudgeUsage(request *Request) (*JudgeUsageSummary, error) {
	summary := &JudgeUsageSummary{CLI: request.Auditor.CLI, Model: request.Auditor.Model, Effort: request.Auditor.Effort, Attempts: []JudgeAttempt{}, Tokens: map[string]*int64{}, TokenCategoryCoverage: map[string]model.Coverage{}}
	groups, err := loadJudgeAttempts(request, summary)
	if err != nil {
		return nil, err
	}
	if err := reconcileJudgeOutputs(request, summary, groups); err != nil {
		return nil, err
	}
	sort.Slice(summary.Attempts, func(i, j int) bool {
		if summary.Attempts[i].LaunchedAt == summary.Attempts[j].LaunchedAt {
			return summary.Attempts[i].AttemptID < summary.Attempts[j].AttemptID
		}
		return summary.Attempts[i].LaunchedAt < summary.Attempts[j].LaunchedAt
	})
	aggregateJudgeAttempts(summary)
	return summary, nil
}

func loadJudgeAttempts(request *Request, summary *JudgeUsageSummary) (map[string][]int, error) {
	groups := map[string][]int{}
	root, err := os.OpenRoot(filepath.Join(request.AuditSessionDir, "judge-usage"))
	if os.IsNotExist(err) {
		return groups, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		parts := strings.Split(path, "/")
		var stage, batch string
		switch {
		case len(parts) == 2 && parts[0] == "correctness":
			stage = "correctness"
		case len(parts) == 3 && parts[0] == "value":
			stage, batch = "value", parts[1]
		default:
			return nil
		}
		attempt := baseJudgeAttempt(request, stage, batch)
		attempt.Outcome, attempt.Usage = "unknown", unavailableJudgeUsage(model.UnavailableParseFailure)
		data, err := root.ReadFile(path)
		if err == nil {
			var stored JudgeAttempt
			if json.Unmarshal(data, &stored) == nil {
				attempt = stored
			}
		}
		// The path is authoritative even if the file is corrupt or contains mismatched fields.
		attempt.AttemptID, attempt.Stage, attempt.BatchID = strings.TrimSuffix(path, ".json"), stage, batch
		key := judgeGroupKey(stage, batch)
		groups[key] = append(groups[key], len(summary.Attempts))
		summary.Attempts = append(summary.Attempts, attempt)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return groups, nil
}

func reconcileJudgeOutputs(request *Request, summary *JudgeUsageSummary, groups map[string][]int) error {
	packagesData, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "value-packages.json")) // #nosec G304 -- fixed Runner-owned audit artifact.
	if err != nil {
		return err
	}
	var packages []ValuePackage
	if err := json.Unmarshal(packagesData, &packages); err != nil {
		return err
	}
	knownBatches := make(map[string]bool, len(packages))
	for _, pkg := range packages {
		knownBatches[pkg.BatchID] = true
	}
	outputs, err := filepath.Glob(filepath.Join(request.AuditSessionDir, "model-output", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range outputs {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		stage, batch := "value", name
		if name == "correctness" {
			stage, batch = "correctness", ""
		} else if !knownBatches[name] {
			continue
		}
		key := judgeGroupKey(stage, batch)
		indices := groups[key]
		if len(indices) == 0 {
			legacy := baseJudgeAttempt(request, stage, batch)
			legacy.AttemptID, legacy.Outcome, legacy.Usage, legacy.Legacy = "legacy/"+key, "succeeded", unavailableJudgeUsage(model.UnavailableNoUsageEvent), true
			summary.Attempts = append(summary.Attempts, legacy)
			continue
		}
		recoverJudgeOutputAttempt(summary.Attempts, indices)
	}
	return nil
}

// recoverJudgeOutputAttempt attributes a completed output with no succeeded
// record to the latest attempt whose final outcome was never recorded.
func recoverJudgeOutputAttempt(attempts []JudgeAttempt, indices []int) {
	for _, index := range indices {
		if attempts[index].Outcome == "succeeded" {
			return
		}
	}
	for i := len(indices) - 1; i >= 0; i-- {
		if attempt := &attempts[indices[i]]; attempt.Outcome == "exited" || attempt.Outcome == "unknown" {
			attempt.Outcome = "succeeded (recovered)"
			return
		}
	}
}

func aggregateJudgeAttempts(summary *JudgeUsageSummary) {
	summary.AttemptCount = len(summary.Attempts)
	totalCount, costCount := 0, 0
	categories := map[string]int{}
	for _, key := range []string{model.TokenInput, model.TokenCachedInput, model.TokenCacheWrite, model.TokenOutput, model.TokenReasoning} {
		summary.Tokens[key] = nil
		summary.TokenCategoryCoverage[key] = model.CoverageNone
	}
	for index := range summary.Attempts {
		attempt := &summary.Attempts[index]
		if attempt.Usage.Status == model.UsageCollected {
			for key, value := range attempt.Usage.Tokens {
				if summary.Tokens[key] == nil {
					summary.Tokens[key] = new(int64)
				}
				*summary.Tokens[key] += value
				categories[key]++
			}
			if attempt.Usage.TokenTotals != nil {
				if summary.TotalTokens == nil {
					summary.TotalTokens = new(int64)
				}
				*summary.TotalTokens += attempt.Usage.TokenTotals.Total
				totalCount++
			}
		}
		if attempt.CostUSD != nil {
			if summary.CostUSD == nil {
				summary.CostUSD = new(float64)
			}
			*summary.CostUSD += *attempt.CostUSD
			costCount++
		}
	}
	for key, count := range categories {
		summary.TokenCategoryCoverage[key] = judgeCoverage(count, summary.AttemptCount)
	}
	summary.TokenCoverage = judgeCoverage(totalCount, summary.AttemptCount)
	summary.CostCoverage = judgeCoverage(costCount, summary.AttemptCount)
}

func judgeCoverage(reported, attempts int) model.Coverage {
	if reported == 0 {
		return model.CoverageNone
	}
	if reported == attempts {
		return model.CoverageComplete
	}
	return model.CoveragePartial
}
