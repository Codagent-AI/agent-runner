// Package profilerecommend builds deterministic role recommendations from an
// already-collected CLI and model discovery snapshot.
package profilerecommend

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Role is one of the four canonical setup-managed agent roles.
type Role string

const (
	Lead        Role = "lead"
	Crosscheck  Role = "crosscheck"
	Implementor Role = "implementor"
	Tester      Role = "tester"
)

var canonicalRoles = [...]Role{Lead, Crosscheck, Implementor, Tester}

// Roles returns the canonical roles in setup customization order.
func Roles() [4]Role {
	return canonicalRoles
}

// Family is the bounded model family classification used for recommendation
// diversity. Unknown is reserved for model-less multi-provider CLI defaults.
type Family string

const (
	Unknown Family = ""
	Claude  Family = "claude"
	GPT     Family = "gpt"
	Other   Family = "other"
)

func (f Family) known() bool {
	return f == Claude || f == GPT
}

// Tier is the bounded power tier recognized by the recommendation policy.
type Tier string

const (
	UnrecognizedTier Tier = ""
	Flagship         Tier = "flagship"
	Balanced         Tier = "balanced"
)

// Fallback explains why a selection did not use its role's preferred tier.
type Fallback string

const (
	NoFallback                Fallback = ""
	AlternateTier             Fallback = "alternate-tier"
	DefaultUnrecognizedModels Fallback = "cli-default-unrecognized-models"
	DefaultNoModels           Fallback = "cli-default-no-models"
	DefaultDiscoveryError     Fallback = "cli-default-discovery-error"
)

// CLIDiscovery is the discovery result for one detected CLI. Models remain in
// source order and DiscoveryError is displayable error text, if discovery
// failed or was incomplete.
type CLIDiscovery struct {
	CLI            string
	Models         []string
	DiscoveryError string
}

// Snapshot is an immutable-by-API copy of ordered discovery input.
type Snapshot struct {
	discoveries []CLIDiscovery
}

// NewSnapshot copies the detector-ordered CLI results and each CLI's
// discovery-ordered models.
func NewSnapshot(discoveries []CLIDiscovery) Snapshot {
	return Snapshot{discoveries: cloneDiscoveries(discoveries)}
}

// Discoveries returns a copy of the detector-ordered discovery results.
func (s Snapshot) Discoveries() []CLIDiscovery {
	return cloneDiscoveries(s.discoveries)
}

// Discovery returns a copy of the first result for cli.
func (s Snapshot) Discovery(cli string) (CLIDiscovery, bool) {
	for _, discovery := range s.discoveries {
		if discovery.CLI == cli {
			return cloneDiscovery(discovery), true
		}
	}
	return CLIDiscovery{}, false
}

func cloneDiscoveries(discoveries []CLIDiscovery) []CLIDiscovery {
	if discoveries == nil {
		return nil
	}
	cloned := make([]CLIDiscovery, len(discoveries))
	for i, discovery := range discoveries {
		cloned[i] = cloneDiscovery(discovery)
	}
	return slices.Clip(cloned)
}

func cloneDiscovery(discovery CLIDiscovery) CLIDiscovery {
	discovery.Models = slices.Clone(discovery.Models)
	return discovery
}

// Classification is the recommendation policy's bounded interpretation of a
// CLI/model selection.
type Classification struct {
	Family Family
	Tier   Tier
}

// Selection is one exact model recommendation or a model-less CLI default.
type Selection struct {
	Role           Role
	CLI            string
	Model          string
	Family         Family
	Tier           Tier
	Fallback       Fallback
	DiscoveryError string
}

// Pair identifies a creator/evaluator diversity relationship.
type Pair string

const (
	LeadCrosscheck    Pair = "lead-crosscheck"
	ImplementorTester Pair = "implementor-tester"
)

// PairStatus explains whether a paired recommendation established known
// family diversity or had to fall back to normal precedence.
type PairStatus struct {
	Pair            Pair
	Creator         Role
	Evaluator       Role
	CreatorFamily   Family
	EvaluatorFamily Family
	Diverse         bool
	Limited         bool
}

// Recommendation contains the fixed four selections and pair explanations.
// Accessors return fixed-size value arrays so callers cannot mutate its state.
type Recommendation struct {
	selections [4]Selection
	pairs      [2]PairStatus
}

// Selections returns recommendations in Lead, Crosscheck, Implementor, Tester
// order.
func (r *Recommendation) Selections() [4]Selection {
	return r.selections
}

// Selection returns the recommendation for role.
func (r *Recommendation) Selection(role Role) Selection {
	for _, selection := range r.selections {
		if selection.Role == role {
			return selection
		}
	}
	return Selection{}
}

// PairStatuses returns Lead/Crosscheck followed by Implementor/Tester.
func (r *Recommendation) PairStatuses() [2]PairStatus {
	return r.pairs
}

var leadCrosscheckTesterCLIOrder = [...]string{"claude", "codex", "opencode", "copilot", "cursor"}
var implementorCLIOrder = [...]string{"codex", "cursor", "opencode", "claude", "copilot"}

var claudeFirst = [...]Family{Claude, GPT, Other}
var gptFirst = [...]Family{GPT, Claude, Other}

type policy struct {
	cliOrder      []string
	familyOrder   []Family
	preferredTier Tier
	alternateTier Tier
}

func policyFor(role Role) (policy, bool) {
	switch role {
	case Lead:
		return policy{
			cliOrder: leadCrosscheckTesterCLIOrder[:], familyOrder: claudeFirst[:],
			preferredTier: Flagship, alternateTier: Balanced,
		}, true
	case Crosscheck:
		return policy{
			cliOrder: leadCrosscheckTesterCLIOrder[:], familyOrder: gptFirst[:],
			preferredTier: Flagship, alternateTier: Balanced,
		}, true
	case Implementor:
		return policy{
			cliOrder: implementorCLIOrder[:], familyOrder: gptFirst[:],
			preferredTier: Balanced, alternateTier: Flagship,
		}, true
	case Tester:
		return policy{
			cliOrder: leadCrosscheckTesterCLIOrder[:], familyOrder: claudeFirst[:],
			preferredTier: Balanced, alternateTier: Flagship,
		}, true
	default:
		return policy{}, false
	}
}

// Recommend builds all four role recommendations and their diversity status.
func Recommend(snapshot Snapshot) Recommendation {
	if len(snapshot.discoveries) == 0 {
		return Recommendation{}
	}

	lead := RecommendRole(snapshot, Lead)
	crosscheck, leadPair := RecommendEvaluator(snapshot, Crosscheck, &lead)
	implementor := RecommendRole(snapshot, Implementor)
	tester, implementorPair := RecommendEvaluator(snapshot, Tester, &implementor)

	return Recommendation{
		selections: [4]Selection{lead, crosscheck, implementor, tester},
		pairs:      [2]PairStatus{leadPair, implementorPair},
	}
}

// RecommendRole applies normal precedence for a role. Paired evaluator roles
// should use RecommendEvaluator when creator diversity is relevant.
func RecommendRole(snapshot Snapshot, role Role) Selection {
	rolePolicy, ok := policyFor(role)
	if !ok {
		return Selection{}
	}
	return selectCandidate(snapshot, role, &rolePolicy, nil)
}

// RecommendEvaluator recomputes Crosscheck or Tester against a finalized
// creator selection.
func RecommendEvaluator(
	snapshot Snapshot,
	evaluator Role,
	creator *Selection,
) (Selection, PairStatus) {
	rolePolicy, ok := policyFor(evaluator)
	if !ok || creator == nil || (evaluator != Crosscheck && evaluator != Tester) {
		return Selection{}, PairStatus{}
	}

	status := pairStatusFor(evaluator)
	creatorFamily := Classify(creator.CLI, creator.Model).Family
	status.CreatorFamily = creatorFamily

	filterFamily := Family("")
	if creatorFamily.known() && hasDifferentKnownCandidate(snapshot, &rolePolicy, creatorFamily) {
		filterFamily = creatorFamily
	}

	selection := selectCandidate(snapshot, evaluator, &rolePolicy, &filterFamily)
	status.EvaluatorFamily = selection.Family
	status.Diverse = filterFamily.known() &&
		selection.Family.known() &&
		selection.Family != creatorFamily
	status.Limited = !status.Diverse
	return selection, status
}

func pairStatusFor(evaluator Role) PairStatus {
	switch evaluator {
	case Crosscheck:
		return PairStatus{
			Pair: LeadCrosscheck, Creator: Lead, Evaluator: Crosscheck,
		}
	case Tester:
		return PairStatus{
			Pair: ImplementorTester, Creator: Implementor, Evaluator: Tester,
		}
	default:
		return PairStatus{}
	}
}

type candidate struct {
	selection Selection
	version   []int
}

func hasDifferentKnownCandidate(snapshot Snapshot, rolePolicy *policy, creator Family) bool {
	for _, cli := range rolePolicy.cliOrder {
		discovery, ok := snapshot.Discovery(cli)
		if !ok {
			continue
		}
		candidates := automaticCandidates(discovery, "")
		for i := range candidates {
			candidate := &candidates[i]
			if candidate.selection.Family.known() && candidate.selection.Family != creator {
				return true
			}
		}
	}
	return false
}

func selectCandidate(
	snapshot Snapshot,
	role Role,
	rolePolicy *policy,
	excludedFamily *Family,
) Selection {
	for _, cli := range rolePolicy.cliOrder {
		discovery, ok := snapshot.Discovery(cli)
		if !ok {
			continue
		}
		candidates := automaticCandidates(discovery, role)
		for _, family := range rolePolicy.familyOrder {
			if isExcluded(family, excludedFamily) {
				continue
			}
			if selected, ok := bestTierCandidate(
				candidates,
				family,
				rolePolicy.preferredTier,
			); ok {
				return selected
			}
			if selected, ok := bestTierCandidate(
				candidates,
				family,
				rolePolicy.alternateTier,
			); ok {
				selected.Fallback = AlternateTier
				return selected
			}
		}

		for i := range candidates {
			possible := &candidates[i]
			if possible.selection.Model != "" ||
				isExcluded(possible.selection.Family, excludedFamily) {
				continue
			}
			return possible.selection
		}
	}
	return Selection{}
}

func isExcluded(family Family, excludedFamily *Family) bool {
	if excludedFamily == nil || !excludedFamily.known() {
		return false
	}
	return !family.known() || family == *excludedFamily
}

func automaticCandidates(discovery CLIDiscovery, role Role) []candidate {
	candidates := make([]candidate, 0, len(discovery.Models)+1)
	if discovery.DiscoveryError == "" {
		for _, model := range discovery.Models {
			classification, version := classify(discovery.CLI, model)
			if classification.Tier == UnrecognizedTier {
				continue
			}
			candidates = append(candidates, candidate{
				selection: Selection{
					Role:           role,
					CLI:            discovery.CLI,
					Model:          model,
					Family:         classification.Family,
					Tier:           classification.Tier,
					DiscoveryError: discovery.DiscoveryError,
				},
				version: version,
			})
		}
	}

	defaultClassification := Classify(discovery.CLI, "")
	candidates = append(candidates, candidate{
		selection: Selection{
			Role:           role,
			CLI:            discovery.CLI,
			Family:         defaultClassification.Family,
			Fallback:       defaultFallback(discovery),
			DiscoveryError: discovery.DiscoveryError,
		},
	})
	return candidates
}

func defaultFallback(discovery CLIDiscovery) Fallback {
	switch {
	case discovery.DiscoveryError != "":
		return DefaultDiscoveryError
	case len(discovery.Models) == 0:
		return DefaultNoModels
	default:
		return DefaultUnrecognizedModels
	}
}

func bestTierCandidate(
	candidates []candidate,
	family Family,
	tier Tier,
) (Selection, bool) {
	best := -1
	for i := range candidates {
		possible := &candidates[i]
		if possible.selection.Model == "" ||
			possible.selection.Family != family ||
			possible.selection.Tier != tier {
			continue
		}
		if best < 0 || compareVersions(possible.version, candidates[best].version) > 0 {
			best = i
		}
	}
	if best < 0 {
		return Selection{}, false
	}
	return candidates[best].selection, true
}

func compareVersions(left, right []int) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	left = trimTrailingZeros(left)
	right = trimTrailingZeros(right)
	for i := 0; i < max(len(left), len(right)); i++ {
		var leftPart, rightPart int
		if i < len(left) {
			leftPart = left[i]
		}
		if i < len(right) {
			rightPart = right[i]
		}
		switch {
		case leftPart > rightPart:
			return 1
		case leftPart < rightPart:
			return -1
		}
	}
	return 0
}

func trimTrailingZeros(version []int) []int {
	for len(version) > 1 && version[len(version)-1] == 0 {
		version = version[:len(version)-1]
	}
	return version
}

// Classify recognizes only the bounded Claude Opus/Sonnet and GPT Sol/Terra
// policy. It preserves no aliases and intentionally does not recognize Gemini.
func Classify(cli, model string) Classification {
	classification, _ := classify(cli, model)
	return classification
}

func classify(cli, model string) (classification Classification, version []int) {
	if model == "" {
		switch cli {
		case "claude":
			return Classification{Family: Claude}, nil
		case "codex":
			return Classification{Family: GPT}, nil
		default:
			return Classification{}, nil
		}
	}

	lowerModel := strings.ToLower(model)
	gptEnd, hasGPT := findGPTMarker(lowerModel)

	var family Family
	switch cli {
	case "claude":
		family = Claude
	case "codex":
		family = GPT
	default:
		switch {
		case hasWholeToken(lowerModel, "claude"):
			family = Claude
		case hasGPT:
			family = GPT
		default:
			family = Other
		}
	}

	switch family {
	case Claude:
		switch {
		case hasWholeToken(lowerModel, "opus"):
			return Classification{Family: family, Tier: Flagship}, nil
		case hasWholeToken(lowerModel, "sonnet"):
			return Classification{Family: family, Tier: Balanced}, nil
		default:
			return Classification{Family: family}, nil
		}
	case GPT:
		if !hasGPT {
			return Classification{Family: family}, nil
		}
		version = parseGPTVersion(lowerModel[gptEnd:])
		switch {
		case hasWholeToken(lowerModel, "sol"):
			return Classification{Family: family, Tier: Flagship}, version
		case hasWholeToken(lowerModel, "terra"):
			return Classification{Family: family, Tier: Balanced}, version
		default:
			return Classification{Family: family}, version
		}
	default:
		return Classification{Family: family}, nil
	}
}

func findGPTMarker(value string) (end int, found bool) {
	for start := 0; start+3 <= len(value); start++ {
		if value[start:start+3] != "gpt" {
			continue
		}
		if isAlphaNumericBefore(value, start) {
			continue
		}
		end = start + 3
		if end == len(value) ||
			!isAlphaNumericAt(value, end) ||
			isASCIIDigit(value[end]) {
			return end, true
		}
	}
	return 0, false
}

func hasWholeToken(value, token string) bool {
	for start := 0; start+len(token) <= len(value); start++ {
		if value[start:start+len(token)] != token {
			continue
		}
		if isAlphaNumericBefore(value, start) {
			continue
		}
		end := start + len(token)
		if isAlphaNumericAt(value, end) {
			continue
		}
		return true
	}
	return false
}

func isAlphaNumeric(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char)
}

func isAlphaNumericBefore(value string, offset int) bool {
	if offset <= 0 {
		return false
	}
	char, _ := utf8.DecodeLastRuneInString(value[:offset])
	return isAlphaNumeric(char)
}

func isAlphaNumericAt(value string, offset int) bool {
	if offset >= len(value) {
		return false
	}
	char, _ := utf8.DecodeRuneInString(value[offset:])
	return isAlphaNumeric(char)
}

func isASCIIDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

func parseGPTVersion(suffix string) []int {
	start := 0
	for start < len(suffix) {
		char, size := utf8.DecodeRuneInString(suffix[start:])
		if isAlphaNumeric(char) {
			break
		}
		start += size
	}
	if start == len(suffix) || !isASCIIDigit(suffix[start]) {
		return nil
	}

	var version []int
	for start < len(suffix) && isASCIIDigit(suffix[start]) {
		componentStart := start
		for start < len(suffix) && isASCIIDigit(suffix[start]) {
			start++
		}
		part, err := strconv.Atoi(suffix[componentStart:start])
		if err != nil {
			return nil
		}
		version = append(version, part)

		next := start
		for next < len(suffix) {
			char, size := utf8.DecodeRuneInString(suffix[next:])
			if isAlphaNumeric(char) {
				break
			}
			next += size
		}
		if next == len(suffix) || !isASCIIDigit(suffix[next]) {
			break
		}
		start = next
	}
	return version
}
