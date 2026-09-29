//go:build dev_audit

package devaudit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
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
	stem := fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(idBytes))
	parts := []string{request.AuditSessionDir, "judge-usage", stage}
	if stage == "value" {
		parts = append(parts, batchID)
	}
	path := filepath.Join(append(parts, stem+".json")...)
	usage, cost := judgeAttemptUsage(adapter, raw)
	id := stage + "/"
	if stage == "value" {
		id += batchID + "/"
	}
	id += stem
	record := JudgeAttempt{AttemptID: id, AuditRunID: request.AuditRunID, Stage: stage, BatchID: batchID, CLI: request.Auditor.CLI, Model: request.Auditor.Model, Effort: request.Auditor.Effort, SessionID: auditSessionID(adapter, raw, workspace, spawnTime), LaunchedAt: spawnTime.UTC().Format(time.RFC3339Nano), Outcome: "exited", Usage: usage, CostUSD: cost}
	if err := stateio.WriteJSONDurable(path, record); err != nil {
		return nil, err
	}
	return &judgeAttemptHandle{path: path, record: record}, nil
}

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
	root := filepath.Join(request.AuditSessionDir, "judge-usage")
	groups := map[string][]int{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 2 && len(parts) != 3 {
			return nil
		}
		stage, batch := parts[0], ""
		if stage == "value" && len(parts) == 3 {
			batch = parts[1]
		} else if stage != "correctness" || len(parts) != 2 {
			return nil
		}
		attempt := JudgeAttempt{AttemptID: strings.TrimSuffix(filepath.ToSlash(rel), ".json"), AuditRunID: request.AuditRunID, Stage: stage, BatchID: batch, CLI: request.Auditor.CLI, Model: request.Auditor.Model, Effort: request.Auditor.Effort, Outcome: "unknown", Usage: unavailableJudgeUsage(model.UnavailableParseFailure)}
		data, err := os.ReadFile(path) // #nosec G304 -- audit-owned ledger path discovered under the audit directory.
		if err == nil {
			var stored JudgeAttempt
			if json.Unmarshal(data, &stored) == nil {
				attempt = stored
			}
		}
		// The path is authoritative even if the file is corrupt or contains mismatched fields.
		attempt.AttemptID, attempt.Stage, attempt.BatchID = strings.TrimSuffix(filepath.ToSlash(rel), ".json"), stage, batch
		groups[stage+"/"+batch] = append(groups[stage+"/"+batch], len(summary.Attempts))
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
		key := stage + "/" + batch
		indices := groups[key]
		if len(indices) == 0 {
			summary.Attempts = append(summary.Attempts, JudgeAttempt{AttemptID: "legacy/" + key, AuditRunID: request.AuditRunID, Stage: stage, BatchID: batch, CLI: request.Auditor.CLI, Model: request.Auditor.Model, Effort: request.Auditor.Effort, Outcome: "succeeded", Usage: unavailableJudgeUsage(model.UnavailableNoUsageEvent), Legacy: true})
			continue
		}
		found := false
		for _, index := range indices {
			if summary.Attempts[index].Outcome == "succeeded" {
				found = true
				break
			}
		}
		if !found {
			latest := indices[len(indices)-1]
			for _, index := range indices {
				if summary.Attempts[index].Outcome == "exited" || summary.Attempts[index].Outcome == "unknown" {
					latest = index
				}
			}
			if summary.Attempts[latest].Outcome == "exited" || summary.Attempts[latest].Outcome == "unknown" {
				summary.Attempts[latest].Outcome = "succeeded (recovered)"
			}
		}
	}
	return nil
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
