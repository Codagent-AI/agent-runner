//go:build dev_audit

package devaudit

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func outputReference(ref *EvidenceReference) bool {
	return ref.Contribution == "" && (ref.Category == "narrative" || (ref.Category == "validation" && strings.HasSuffix(ref.ID, "-output")))
}
func contributionReference(ref *EvidenceReference) bool {
	return ref.Status == "available" && oneOf(ref.Contribution, "append", "created", "diff", "commit_patch")
}
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n = max(n, 0)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
func addOmission(leaf *LeafEvidence, marker string) {
	for _, value := range leaf.OmittedCategories {
		if value == marker {
			return
		}
	}
	leaf.OmittedCategories = append(leaf.OmittedCategories, marker)
}

// boundLeafDetail budgets encoded JSON, including escaping and metadata. The
// full leaf remains in the index; only this model-facing projection is bounded.
func boundLeafDetail(leaf *LeafEvidence) LeafEvidence {
	result := *leaf
	result.Evidence = []EvidenceReference{}
	result.OmittedCategories = append([]string(nil), leaf.OmittedCategories...)
	boundFixedFacts(&result)
	selected, totals := selectReferenceShells(leaf.Evidence, leaf.Skeleton.ObservationID)
	result.Evidence = referenceShells(selected, totals, leaf.Skeleton.ObservationID)
	for i := len(selected); i < len(result.Evidence); i++ {
		addOmission(&result, "truncated_"+result.Evidence[i].Category)
	}
	budget := detailBudget{leaf: &result, details: make([]string, len(selected))}
	for i := range selected {
		budget.details[i] = selected[i].Detail
	}
	budget.reserveContent(selected)
	order := make([]int, len(selected))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return evidencePriority(selected[order[i]].Category) < evidencePriority(selected[order[j]].Category)
	})
	for _, i := range order {
		budget.allocate(i, defaultLeafDetailBytes-512-encodedJSONBytes(result))
	}
	for i := range selected {
		if len(result.Evidence[i].Detail) < len(budget.details[i]) {
			result.Evidence[i].Truncated = true
			addOmission(&result, "truncated_"+result.Evidence[i].Category)
		}
	}
	sort.Strings(result.OmittedCategories)
	return result
}
func boundFixedFacts(leaf *LeafEvidence) {
	git := &leaf.Skeleton.Git
	git.ChangedPaths = append([]string(nil), git.ChangedPaths...)
	if len(git.ChangedPaths) > 64 {
		git.ChangedPathsOmitted += len(git.ChangedPaths) - 64
		git.ChangedPaths = git.ChangedPaths[:64]
		addOmission(leaf, "truncated_git")
	}
	for encodedJSONBytes(leaf) > 6<<10 && len(git.ChangedPaths) > 0 {
		git.ChangedPaths = git.ChangedPaths[:len(git.ChangedPaths)-1]
		git.ChangedPathsOmitted++
		addOmission(leaf, "truncated_git")
	}
	if encodedJSONBytes(leaf) <= 6<<10 {
		return
	}
	s := &leaf.Skeleton
	for _, field := range []*string{&s.SchemaVersion, &s.ObservationID, &s.ObservedAtUTC, &s.Project, &s.Workflow, &s.SourceRunID, &s.ExecutionSessionID, &s.AuditRunID, &s.Trigger, &s.SourceOutcome, &s.StepID, &s.Lineage, &s.StepOutcome, &git.Attribution, &git.Reason} {
		*field = cutUTF8(*field, 128)
	}
	git.CommitSHAs = boundedFactList(git.CommitSHAs)
	git.DeferredSHAs = boundedFactList(git.DeferredSHAs)
	s.Cost.SourceModels = boundedFactList(s.Cost.SourceModels)
	addOmission(leaf, "truncated_git")
}
func boundedFactList(values []string) []string {
	result := append([]string(nil), values[:min(len(values), 8)]...)
	for i := range result {
		result[i] = cutUTF8(result[i], 64)
	}
	return result
}
func referenceRank(ref *EvidenceReference) int {
	if outputReference(ref) {
		return 0
	}
	if contributionReference(ref) {
		return 1
	}
	return 2 + evidencePriority(ref.Category)
}
func selectReferenceShells(refs []EvidenceReference, observationID string) (selected []EvidenceReference, totals map[string]int) {
	ordered := append([]EvidenceReference(nil), refs...)
	// Stable ties preserve final-attempt-first output order.
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := referenceRank(&ordered[i]), referenceRank(&ordered[j])
		if a != b {
			return a < b
		}
		if a != 1 {
			return false
		}
		if ordered[i].Path != ordered[j].Path {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Segment < ordered[j].Segment
	})
	totals = map[string]int{}
	counts := map[string]int{}
	for i := range ordered {
		totals[ordered[i].Category]++
	}
	selected = []EvidenceReference{}
	for i := range ordered {
		ref := &ordered[i]
		if counts[ref.Category] >= 8 {
			continue
		}
		candidate := append(append([]EvidenceReference(nil), selected...), *ref)
		if encodedJSONBytes(referenceShells(candidate, totals, observationID)) > 4<<10 {
			continue
		}
		selected = candidate
		counts[ref.Category]++
	}
	return selected, totals
}
func referenceShells(chosen []EvidenceReference, totals map[string]int, observationID string) []EvidenceReference {
	out := make([]EvidenceReference, 0, len(chosen)+len(totals))
	kept := map[string]int{}
	for i := range chosen {
		r := chosen[i]
		r.Detail = ""
		r.Reason = cutUTF8(r.Reason, 128)
		r.Path = ""
		r.ProducerExecutionSession = ""
		r.Lineage = ""
		out = append(out, r)
		kept[r.Category]++
	}
	cats := make([]string, 0, len(totals))
	for cat := range totals {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		if omitted := totals[cat] - kept[cat]; omitted > 0 {
			out = append(out, EvidenceReference{ID: "summary-" + cat, Category: cat, Status: "summarized", Omitted: omitted, Ref: fmt.Sprintf("evidence-index.json#leaf=%s&category=%s", observationID, cat)})
		}
	}
	return out
}

type detailBudget struct {
	leaf    *LeafEvidence
	details []string
}

func (b *detailBudget) allocate(i, limit int) {
	if i < 0 || i >= len(b.details) || limit <= 0 {
		return
	}
	original := b.details[i]
	old := b.leaf.Evidence[i].Detail
	low, high := len(old), len(original)
	before := encodedJSONBytes(b.leaf)
	for low < high {
		mid := (low + high + 1) / 2
		b.leaf.Evidence[i].Detail = cutUTF8(original, mid)
		size := encodedJSONBytes(b.leaf)
		if size-before <= limit && size <= defaultLeafDetailBytes-512 {
			low = mid
		} else {
			high = mid - 1
		}
	}
	b.leaf.Evidence[i].Detail = cutUTF8(original, low)
}
func (b *detailBudget) reserveContent(refs []EvidenceReference) {
	final, first := -1, -1
	for i := range refs {
		if final < 0 && outputReference(&refs[i]) && refs[i].Status == "available" {
			final = i
		}
		if first < 0 && contributionReference(&refs[i]) {
			first = i
		}
	}
	before := encodedJSONBytes(b.leaf)
	b.allocate(final, 6<<10)
	b.allocate(first, 2<<10)
	left := (12 << 10) - (encodedJSONBytes(b.leaf) - before)
	b.allocate(final, left)
	left = (12 << 10) - (encodedJSONBytes(b.leaf) - before)
	for i := range refs {
		if contributionReference(&refs[i]) {
			old := encodedJSONBytes(b.leaf)
			b.allocate(i, left)
			left -= encodedJSONBytes(b.leaf) - old
		}
	}
}
