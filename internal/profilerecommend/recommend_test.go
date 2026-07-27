package profilerecommend

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRecommendWorkedExamples(t *testing.T) {
	tests := []struct {
		name      string
		discovery []CLIDiscovery
		want      [4]Selection
		wantPairs [2]PairStatus
	}{
		{
			name: "standard Claude and Codex recommendation",
			discovery: []CLIDiscovery{
				{CLI: "codex", Models: []string{"gpt-5.6-terra", "gpt-5.6-sol"}},
				{CLI: "claude", Models: []string{"sonnet", "opus"}},
			},
			want: [4]Selection{
				{Role: Lead, CLI: "claude", Model: "opus", Family: Claude, Tier: Flagship},
				{Role: Crosscheck, CLI: "codex", Model: "gpt-5.6-sol", Family: GPT, Tier: Flagship},
				{Role: Implementor, CLI: "codex", Model: "gpt-5.6-terra", Family: GPT, Tier: Balanced},
				{Role: Tester, CLI: "claude", Model: "sonnet", Family: Claude, Tier: Balanced},
			},
			wantPairs: [2]PairStatus{
				{
					Pair:            LeadCrosscheck,
					Creator:         Lead,
					Evaluator:       Crosscheck,
					CreatorFamily:   Claude,
					EvaluatorFamily: GPT,
					Diverse:         true,
				},
				{
					Pair:            ImplementorTester,
					Creator:         Implementor,
					Evaluator:       Tester,
					CreatorFamily:   GPT,
					EvaluatorFamily: Claude,
					Diverse:         true,
				},
			},
		},
		{
			name: "multi-provider defaults when dedicated CLIs are absent",
			discovery: []CLIDiscovery{
				{CLI: "cursor", Models: []string{"cursor-small"}},
				{CLI: "copilot", Models: []string{"github-default"}},
				{CLI: "opencode", Models: []string{"local-model"}},
			},
			want: [4]Selection{
				defaultSelection(Lead, "opencode", DefaultUnrecognizedModels),
				defaultSelection(Crosscheck, "opencode", DefaultUnrecognizedModels),
				defaultSelection(Implementor, "cursor", DefaultUnrecognizedModels),
				defaultSelection(Tester, "opencode", DefaultUnrecognizedModels),
			},
			wantPairs: [2]PairStatus{
				{
					Pair:      LeadCrosscheck,
					Creator:   Lead,
					Evaluator: Crosscheck,
					Limited:   true,
				},
				{
					Pair:      ImplementorTester,
					Creator:   Implementor,
					Evaluator: Tester,
					Limited:   true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Recommend(NewSnapshot(tt.discovery))

			if diff := cmp.Diff(tt.want, got.Selections()); diff != "" {
				t.Errorf("selections mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantPairs, got.PairStatuses()); diff != "" {
				t.Errorf("pair statuses mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecommendUsesCLIMajorPolicyOrdering(t *testing.T) {
	tests := []struct {
		name      string
		discovery []CLIDiscovery
		role      Role
		creator   Selection
		want      Selection
	}{
		{
			name: "family precedence applies within one CLI",
			discovery: []CLIDiscovery{
				{CLI: "opencode", Models: []string{"gpt-5.7-sol", "anthropic/claude-opus"}},
			},
			role: Lead,
			want: Selection{
				Role: Lead, CLI: "opencode", Model: "anthropic/claude-opus",
				Family: Claude, Tier: Flagship,
			},
		},
		{
			name: "earlier CLI wins before preferred family on later CLI",
			discovery: []CLIDiscovery{
				{CLI: "cursor", Models: []string{"gpt-5.7-sol"}},
				{CLI: "claude", Models: []string{"opus"}},
				{CLI: "opencode"},
			},
			role:    Crosscheck,
			creator: defaultSelection(Lead, "opencode", DefaultNoModels),
			want: Selection{
				Role: Crosscheck, CLI: "claude", Model: "opus",
				Family: Claude, Tier: Flagship,
			},
		},
		{
			name: "implementor owns separate CLI order",
			discovery: []CLIDiscovery{
				{CLI: "claude", Models: []string{"sonnet"}},
				{CLI: "cursor", Models: []string{"gpt-5.7-terra"}},
			},
			role: Implementor,
			want: Selection{
				Role: Implementor, CLI: "cursor", Model: "gpt-5.7-terra",
				Family: GPT, Tier: Balanced,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := NewSnapshot(tt.discovery)
			var got Selection
			if tt.role == Crosscheck || tt.role == Tester {
				got, _ = RecommendEvaluator(snapshot, tt.role, &tt.creator)
			} else {
				got = RecommendRole(snapshot, tt.role)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("selection mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecommendEvaluatorRecomputesForCreator(t *testing.T) {
	snapshot := NewSnapshot([]CLIDiscovery{
		{CLI: "opencode", Models: []string{
			"anthropic/claude-opus",
			"anthropic/claude-sonnet",
			"openai/gpt-5.7-sol",
			"openai/gpt-5.7-terra",
		}},
	})

	tests := []struct {
		name      string
		evaluator Role
		creator   Selection
		want      Selection
		wantPair  PairStatus
	}{
		{
			name:      "crosscheck differs from Claude lead on same multi-provider CLI",
			evaluator: Crosscheck,
			creator: Selection{
				Role: Lead, CLI: "opencode", Model: "anthropic/claude-opus",
				Family: Claude, Tier: Flagship,
			},
			want: Selection{
				Role: Crosscheck, CLI: "opencode", Model: "openai/gpt-5.7-sol",
				Family: GPT, Tier: Flagship,
			},
			wantPair: PairStatus{
				Pair:            LeadCrosscheck,
				Creator:         Lead,
				Evaluator:       Crosscheck,
				CreatorFamily:   Claude,
				EvaluatorFamily: GPT,
				Diverse:         true,
			},
		},
		{
			name:      "crosscheck recomputes away from a customized GPT lead",
			evaluator: Crosscheck,
			creator: Selection{
				Role: Lead, CLI: "opencode", Model: "openai/gpt-5.7-sol",
				Family: GPT, Tier: Flagship,
			},
			want: Selection{
				Role: Crosscheck, CLI: "opencode", Model: "anthropic/claude-opus",
				Family: Claude, Tier: Flagship,
			},
			wantPair: PairStatus{
				Pair:            LeadCrosscheck,
				Creator:         Lead,
				Evaluator:       Crosscheck,
				CreatorFamily:   GPT,
				EvaluatorFamily: Claude,
				Diverse:         true,
			},
		},
		{
			name:      "tester differs from GPT implementor on same multi-provider CLI",
			evaluator: Tester,
			creator: Selection{
				Role: Implementor, CLI: "opencode", Model: "openai/gpt-5.7-terra",
				Family: GPT, Tier: Balanced,
			},
			want: Selection{
				Role: Tester, CLI: "opencode", Model: "anthropic/claude-sonnet",
				Family: Claude, Tier: Balanced,
			},
			wantPair: PairStatus{
				Pair:            ImplementorTester,
				Creator:         Implementor,
				Evaluator:       Tester,
				CreatorFamily:   GPT,
				EvaluatorFamily: Claude,
				Diverse:         true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotPair := RecommendEvaluator(snapshot, tt.evaluator, &tt.creator)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("selection mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantPair, gotPair); diff != "" {
				t.Errorf("pair status mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecommendEvaluatorExplainsDiversityLimitations(t *testing.T) {
	tests := []struct {
		name      string
		discovery []CLIDiscovery
		evaluator Role
		creator   Selection
		want      Selection
		wantPair  PairStatus
	}{
		{
			name: "same and unclassified families do not establish crosscheck diversity",
			discovery: []CLIDiscovery{
				{CLI: "claude", Models: []string{"opus"}},
				{CLI: "opencode", Models: []string{"mystery-model"}},
			},
			evaluator: Crosscheck,
			creator: Selection{
				Role: Lead, CLI: "claude", Model: "opus", Family: Claude, Tier: Flagship,
			},
			want: Selection{
				Role: Crosscheck, CLI: "claude", Model: "opus", Family: Claude, Tier: Flagship,
			},
			wantPair: PairStatus{
				Pair:            LeadCrosscheck,
				Creator:         Lead,
				Evaluator:       Crosscheck,
				CreatorFamily:   Claude,
				EvaluatorFamily: Claude,
				Limited:         true,
			},
		},
		{
			name: "unknown multi-provider creator does not trigger a false diversity claim",
			discovery: []CLIDiscovery{
				{CLI: "opencode"},
				{CLI: "claude", Models: []string{"sonnet"}},
			},
			evaluator: Tester,
			creator:   defaultSelection(Implementor, "opencode", DefaultNoModels),
			want: Selection{
				Role: Tester, CLI: "claude", Model: "sonnet", Family: Claude, Tier: Balanced,
			},
			wantPair: PairStatus{
				Pair:            ImplementorTester,
				Creator:         Implementor,
				Evaluator:       Tester,
				EvaluatorFamily: Claude,
				Limited:         true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotPair := RecommendEvaluator(
				NewSnapshot(tt.discovery),
				tt.evaluator,
				&tt.creator,
			)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("selection mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantPair, gotPair); diff != "" {
				t.Errorf("pair status mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecommendTierFallbacks(t *testing.T) {
	tests := []struct {
		name      string
		discovery []CLIDiscovery
		role      Role
		want      Selection
	}{
		{
			name: "balanced role falls back to flagship tier",
			discovery: []CLIDiscovery{
				{CLI: "codex", Models: []string{"gpt-5.7-sol"}},
			},
			role: Implementor,
			want: Selection{
				Role: Implementor, CLI: "codex", Model: "gpt-5.7-sol",
				Family: GPT, Tier: Flagship, Fallback: AlternateTier,
			},
		},
		{
			name: "flagship role falls back to balanced tier",
			discovery: []CLIDiscovery{
				{CLI: "claude", Models: []string{"sonnet"}},
			},
			role: Lead,
			want: Selection{
				Role: Lead, CLI: "claude", Model: "sonnet",
				Family: Claude, Tier: Balanced, Fallback: AlternateTier,
			},
		},
		{
			name: "unrecognized models fall back to CLI default",
			discovery: []CLIDiscovery{
				{CLI: "opencode", Models: []string{"zeta", "alpha"}},
			},
			role: Lead,
			want: defaultSelection(Lead, "opencode", DefaultUnrecognizedModels),
		},
		{
			name: "empty model result falls back to CLI default",
			discovery: []CLIDiscovery{
				{CLI: "claude"},
			},
			role: Lead,
			want: defaultSelection(Lead, "claude", DefaultNoModels),
		},
		{
			name: "discovery error falls back to CLI default and is retained",
			discovery: []CLIDiscovery{
				{CLI: "codex", DiscoveryError: "model query timed out"},
			},
			role: Implementor,
			want: Selection{
				Role:           Implementor,
				CLI:            "codex",
				Family:         GPT,
				Fallback:       DefaultDiscoveryError,
				DiscoveryError: "model query timed out",
			},
		},
		{
			name: "discovery error ignores partial recognized models",
			discovery: []CLIDiscovery{
				{
					CLI:            "codex",
					Models:         []string{"gpt-5.7-terra"},
					DiscoveryError: "model query returned a partial result",
				},
			},
			role: Implementor,
			want: Selection{
				Role:           Implementor,
				CLI:            "codex",
				Family:         GPT,
				Fallback:       DefaultDiscoveryError,
				DiscoveryError: "model query returned a partial result",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RecommendRole(NewSnapshot(tt.discovery), tt.role)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("selection mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecommendGPTVersions(t *testing.T) {
	tests := []struct {
		name   string
		models []string
		role   Role
		want   string
	}{
		{
			name: "latest flexible balanced GPT version",
			models: []string{
				"gpt-5.6-terra",
				"openai:gpt_5_7_terra",
				"gpt-5.7-sol-latest",
			},
			role: Implementor,
			want: "openai:gpt_5_7_terra",
		},
		{
			name: "latest flexible flagship GPT version",
			models: []string{
				"gpt-5.6-sol",
				"gpt-5.7-sol-latest",
				"openai:gpt_5_7_terra",
			},
			role: Lead,
			want: "gpt-5.7-sol-latest",
		},
		{
			name:   "numeric components compare numerically",
			models: []string{"gpt-5.9-sol", "gpt-5.10-sol"},
			role:   Lead,
			want:   "gpt-5.10-sol",
		},
		{
			name:   "equal versions retain discovery order",
			models: []string{"openai/gpt-5.7-sol", "gpt_5_7_sol"},
			role:   Lead,
			want:   "openai/gpt-5.7-sol",
		},
		{
			name:   "unparseable versions retain discovery order",
			models: []string{"gpt-sol", "gpt-9.9-sol"},
			role:   Lead,
			want:   "gpt-sol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RecommendRole(NewSnapshot([]CLIDiscovery{
				{CLI: "opencode", Models: tt.models},
			}), tt.role)
			if got.Model != tt.want {
				t.Fatalf("model = %q, want %q", got.Model, tt.want)
			}
		})
	}
}

func TestParseGPTVersionRejectsPlatformIntOverflow(t *testing.T) {
	if got := parseGPTVersion("-999999999999999999999999999999-sol"); got != nil {
		t.Fatalf("parseGPTVersion() = %v, want nil", got)
	}
}

func TestClassifyRecognizedIdentifierShapes(t *testing.T) {
	tests := []struct {
		name  string
		cli   string
		model string
		want  Classification
	}{
		{
			name: "Claude Opus token",
			cli:  "opencode", model: "anthropic/claude-opus-4",
			want: Classification{Family: Claude, Tier: Flagship},
		},
		{
			name: "Claude Sonnet token is case insensitive",
			cli:  "cursor", model: "CLAUDE_SONNET_latest",
			want: Classification{Family: Claude, Tier: Balanced},
		},
		{
			name: "provider slash GPT Sol",
			cli:  "opencode", model: "openai/gpt-5.7-sol",
			want: Classification{Family: GPT, Tier: Flagship},
		},
		{
			name: "provider colon and underscore GPT Terra",
			cli:  "opencode", model: "openai:gpt_5_7_terra",
			want: Classification{Family: GPT, Tier: Balanced},
		},
		{
			name: "GPT immediately followed by version",
			cli:  "cursor", model: "gpt5.7-sol-latest",
			want: Classification{Family: GPT, Tier: Flagship},
		},
		{
			name: "vendor prefix and intervening token",
			cli:  "copilot", model: "vendor/openai-gpt-5.7-codex-terra",
			want: Classification{Family: GPT, Tier: Balanced},
		},
		{
			name: "dedicated Claude default",
			cli:  "claude",
			want: Classification{Family: Claude},
		},
		{
			name: "dedicated Codex default",
			cli:  "codex",
			want: Classification{Family: GPT},
		},
		{
			name: "multi-provider default",
			cli:  "opencode",
			want: Classification{},
		},
		{
			name: "concrete unclassified model",
			cli:  "cursor", model: "gemini-2.5-pro",
			want: Classification{Family: Other},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, Classify(tt.cli, tt.model)); diff != "" {
				t.Errorf("classification mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClassifyRejectsFalsePositiveBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		model string
	}{
		{name: "Sol is part of solar", model: "gpt-5.7-solar-model"},
		{name: "Terra is part of terrain", model: "gpt-5.7-terrain-large"},
		{name: "Sol without GPT family", model: "claude-sol"},
		{name: "GPT has no left boundary", model: "megpt-5.7-sol"},
		{name: "GPT has no right boundary or version", model: "gptastic-sol"},
		{name: "Opus is part of larger token", model: "claude-opusmax"},
		{name: "Sonnet is part of larger token", model: "claude-sonnetish"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify("opencode", tt.model)
			if got.Tier != UnrecognizedTier {
				t.Fatalf("tier = %q, want %q (classification %#v)", got.Tier, UnrecognizedTier, got)
			}
		})
	}
}

func TestSnapshotPreservesOrderAndOwnsItsData(t *testing.T) {
	input := []CLIDiscovery{
		{CLI: "cursor", Models: []string{"first", "second"}},
		{CLI: "claude", Models: []string{"opus"}, DiscoveryError: "partial result"},
	}
	snapshot := NewSnapshot(input)

	input[0].CLI = "mutated"
	input[0].Models[0] = "mutated"
	first := snapshot.Discoveries()
	first[0].CLI = "also-mutated"
	first[0].Models[0] = "also-mutated"

	want := []CLIDiscovery{
		{CLI: "cursor", Models: []string{"first", "second"}},
		{CLI: "claude", Models: []string{"opus"}, DiscoveryError: "partial result"},
	}
	if diff := cmp.Diff(want, snapshot.Discoveries()); diff != "" {
		t.Fatalf("discoveries mismatch (-want +got):\n%s", diff)
	}
}

func TestRecommendEmptySnapshot(t *testing.T) {
	got := Recommend(NewSnapshot(nil))
	if diff := cmp.Diff([4]Selection{}, got.Selections()); diff != "" {
		t.Errorf("selections mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([2]PairStatus{}, got.PairStatuses()); diff != "" {
		t.Errorf("pair statuses mismatch (-want +got):\n%s", diff)
	}
}

func defaultSelection(role Role, cli string, fallback Fallback) Selection {
	return Selection{
		Role:     role,
		CLI:      cli,
		Family:   Classify(cli, "").Family,
		Fallback: fallback,
	}
}
