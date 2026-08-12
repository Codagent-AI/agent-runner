package prevalidate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/google/go-cmp/cmp"
)

func TestPipelineResolvesParamBoundSubWorkflow(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "green-v1.0.yaml", `
name: green
steps:
  - id: ok
    command: echo ok
`)
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
params:
  - name: flavor
steps:
  - id: call
    workflow: "{{flavor}}.yaml"
`)

	opts, _, _ := fakeOptions(t, &config.Config{})
	if _, err := Pipeline(root, map[string]string{"flavor": "green-v1.0"}, Strict, opts); err != nil {
		t.Fatalf("Pipeline returned error: %v", err)
	}
}

func TestPipelineHandlesUnboundSubWorkflowParamByMode(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
params:
  - name: flavor
steps:
  - id: call
    workflow: "{{flavor}}-v1.0.yaml"
`)
	opts, _, _ := fakeOptions(t, &config.Config{})

	t.Run("strict rejects", func(t *testing.T) {
		_, err := Pipeline(root, nil, Strict, opts)
		if err == nil {
			t.Fatal("Pipeline error = nil, want unbound parameter failure")
		}
		for _, want := range []string{"call", "flavor", "unresolved workflow parameter"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Pipeline error = %q, want substring %q", err, want)
			}
		}
	})

	t.Run("lenient defers", func(t *testing.T) {
		result, err := Pipeline(root, nil, Lenient, opts)
		if err != nil {
			t.Fatalf("Pipeline returned error: %v", err)
		}
		if len(result.DeferredWarnings) != 1 {
			t.Fatalf("DeferredWarnings = %v, want one", result.DeferredWarnings)
		}
		for _, want := range []string{"call", "flavor", "checked at run time"} {
			if !strings.Contains(result.DeferredWarnings[0].Error(), want) {
				t.Fatalf("warning = %q, want substring %q", result.DeferredWarnings[0], want)
			}
		}
	})
}

func TestPipelineAllowsNilProfileStoreForWorkflowWithoutAgents(t *testing.T) {
	root := writeWorkflow(t, t.TempDir(), "shell-v1.0.yaml", `
name: shell
steps:
  - id: shell
    command: echo ok
`)
	opts := Options{
		LoadConfig: func() (*config.Config, []string, error) { return nil, nil, nil },
		LookPath:   func(string) (string, error) { return "", nil },
		Adapter:    func(string) (cli.Adapter, error) { return nil, fmt.Errorf("unused") },
	}
	if _, err := Pipeline(root, nil, Strict, opts); err != nil {
		t.Fatalf("Pipeline() error = %v", err)
	}
}

func TestPipelineRecognizesAndReservesIntakeHandoff(t *testing.T) {
	opts, _, _ := fakeOptions(t, &config.Config{})
	fixtures := filepath.Join("..", "..", "testdata")
	referencing := filepath.Join(fixtures, "intake-handoff-reference-v1.0.yaml")
	if _, err := Pipeline(referencing, nil, Strict, opts); err != nil {
		t.Fatalf("Pipeline() reference error = %v, want intake_handoff builtin accepted", err)
	}

	for name, fixture := range map[string]string{
		"parameter":       "intake-handoff-param-v1.0.yaml",
		"capture":         "intake-handoff-capture-v1.0.yaml",
		"outcome capture": "intake-handoff-outcome-capture-v1.0.yaml",
	} {
		path := filepath.Join(fixtures, fixture)
		if _, err := Pipeline(path, nil, Strict, opts); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("Pipeline() %s error = %v, want reserved-name error", name, err)
		}
	}
}

func TestPipelineReturnsDeduplicatedLegacyAgentDeprecations(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "legacy-v1.0.yaml", `
name: legacy
steps:
  - id: first
    agent: planner
    session: new
    prompt: first
  - id: second
    agent: planner
    session: new
    prompt: second
`)
	cfg := &config.Config{ActiveAgents: map[string]*config.Agent{
		"lead": {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet", Effort: "high"},
	}}
	opts, _, _ := fakeOptions(t, cfg)

	result, err := Pipeline(root, nil, Strict, opts)
	if err != nil {
		t.Fatalf("Pipeline() error = %v", err)
	}
	if len(result.AgentDeprecations) != 1 {
		t.Fatalf("AgentDeprecations length = %d, want 1", len(result.AgentDeprecations))
	}
	item := result.AgentDeprecations[0]
	if item.Alias != "planner" || item.Canonical != "lead" {
		t.Fatalf("AgentDeprecations[0] = %v, want planner->lead", item)
	}
}

func TestPipelineRewalksSameSubWorkflowWithDifferentParams(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "good-v1.0.yaml", `
name: good
steps:
  - id: ok
    command: echo ok
`)
	writeWorkflow(t, dir, "switch-v1.0.yaml", `
name: switch
params:
  - name: target
steps:
  - id: call-target
    workflow: "{{target}}.yaml"
`)
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: first
    workflow: switch-v1.0.yaml
    params:
      target: good-v1.0
  - id: second
    workflow: switch-v1.0.yaml
    params:
      target: missing-v1.0
`)

	opts, _, _ := fakeOptions(t, &config.Config{})
	_, err := Pipeline(root, nil, Strict, opts)
	if err == nil {
		t.Fatal("expected second parameter set to validate and fail")
	}
	if !strings.Contains(err.Error(), "missing-v1.0.yaml") {
		t.Fatalf("expected missing nested workflow error, got: %v", err)
	}
}

func TestPipelineSubWorkflowLoadFailureNamesReferencingStep(t *testing.T) {
	tests := []struct {
		name       string
		childName  string
		childBody  string
		wantDetail string
	}{
		{name: "missing", childName: "missing-v1.0.yaml", wantDetail: "cannot read workflow file"},
		{name: "malformed", childName: "broken-v1.0.yaml", childBody: "not: yaml: valid: :", wantDetail: "invalid YAML"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.childBody != "" {
				writeWorkflow(t, dir, tt.childName, tt.childBody)
			}
			root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: call-child
    workflow: `+tt.childName+`
`)
			opts, _, _ := fakeOptions(t, &config.Config{})

			_, err := Pipeline(root, nil, Strict, opts)
			if err == nil {
				t.Fatal("Pipeline error = nil, want child load failure")
			}
			for _, want := range []string{"call-child", tt.childName, tt.wantDetail} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Pipeline error = %q, want substring %q", err, want)
				}
			}
		})
	}
}

func TestPipelineRejectsMissingRequiredSubWorkflowParamByMode(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "child-v1.0.yaml", `
name: child
params:
  - name: release
steps:
  - id: use-release
    command: echo "{{release}}"
`)
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: call-child
    workflow: child-v1.0.yaml
`)
	opts, _, _ := fakeOptions(t, &config.Config{})

	for _, mode := range []Mode{Strict, Lenient} {
		_, err := Pipeline(root, nil, mode, opts)
		if err == nil {
			t.Fatalf("mode %v: Pipeline error = nil, want missing required child parameter", mode)
		}
		for _, want := range []string{"child-v1.0.yaml", "call-child", "release", "missing required parameter"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("mode %v: Pipeline error = %q, want substring %q", mode, err, want)
			}
		}
	}
}

func TestPipelineRejectsCapturedWorkflowPath(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: choose
    command: echo child-v1.0.yaml
    capture: detected_target
  - id: call
    workflow: "{{detected_target}}"
`)

	opts, _, _ := fakeOptions(t, &config.Config{})
	for _, mode := range []Mode{Strict, Lenient} {
		_, err := Pipeline(root, nil, mode, opts)
		if err == nil {
			t.Fatalf("mode %v: expected captured workflow path error", mode)
		}
		if !strings.Contains(err.Error(), "call") || !strings.Contains(err.Error(), "captured") {
			t.Fatalf("mode %v: expected error to name step and captured variable, got: %v", mode, err)
		}
	}
}

func TestPipelineRejectsMissingNamedSessionInChild(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "child-v1.0.yaml", `
name: child
steps:
  - id: continue-work
    prompt: continue
    session: implementor
`)
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: call-child
    workflow: child-v1.0.yaml
`)
	opts, _, _ := fakeOptions(t, &config.Config{})

	_, err := Pipeline(root, nil, Strict, opts)
	if err == nil {
		t.Fatal("Pipeline error = nil, want missing session declaration")
	}
	for _, want := range []string{"child-v1.0.yaml", "continue-work", "implementor", "not declared"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Pipeline error = %q, want substring %q", err, want)
		}
	}
}

func TestPipelineMissingParamBoundChildNamesTemplateAndResolvedPath(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
params:
  - name: flavor
steps:
  - id: call
    workflow: "{{flavor}}-v1.0.yaml"
`)
	opts, _, _ := fakeOptions(t, &config.Config{})

	_, err := Pipeline(root, map[string]string{"flavor": "green"}, Strict, opts)
	if err == nil {
		t.Fatal("Pipeline error = nil, want missing resolved child")
	}
	for _, want := range []string{"call", "{{flavor}}-v1.0.yaml", "green-v1.0.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Pipeline error = %q, want substring %q", err, want)
		}
	}
}

func TestPipelineChecksInterpolatedVariableReferences(t *testing.T) {
	t.Run("rejects undefined prompt variable", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: ask
    agent: implementor
    prompt: "use {{missing}}"
`)
		cfg := &config.Config{
			ActiveAgents: map[string]*config.Agent{
				"implementor": {DefaultMode: "autonomous", CLI: "claude", Model: "opus", Effort: "high"},
			},
		}
		opts, _, _ := fakeOptions(t, cfg)

		_, err := Pipeline(root, nil, Strict, opts)
		if err == nil {
			t.Fatal("expected undefined variable error")
		}
		if !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "ask") {
			t.Fatalf("expected error to name variable and step, got: %v", err)
		}
	})

	t.Run("allows variables captured by an earlier step", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: discover
    command: echo value
    capture: found
  - id: use
    command: echo {{found}}
`)
		opts, _, _ := fakeOptions(t, &config.Config{})

		if _, err := Pipeline(root, nil, Strict, opts); err != nil {
			t.Fatalf("Pipeline returned error: %v", err)
		}
	})
}

func TestPipelineDedupesSessionAwareProbeTriples(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "child-v1.0.yaml", `
name: child
steps:
  - id: inherited
    prompt: continue
    session: inherit
`)
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: origin
    agent: implementor
    prompt: start
  - id: changed
    prompt: continue
    session: resume
    model: sonnet
  - id: call
    workflow: child-v1.0.yaml
`)
	cfg := &config.Config{
		ActiveProfile: "default",
		Profiles: map[string]*config.ProfileSet{
			"default": {Agents: map[string]*config.Agent{
				"implementor": {DefaultMode: "autonomous", CLI: "claude", Model: "opus", Effort: "high"},
			}},
		},
		ActiveAgents: map[string]*config.Agent{
			"implementor": {DefaultMode: "autonomous", CLI: "claude", Model: "opus", Effort: "high"},
		},
	}
	opts, lookups, probes := fakeOptions(t, cfg)

	if _, err := Pipeline(root, nil, Strict, opts); err != nil {
		t.Fatalf("Pipeline returned error: %v", err)
	}

	if got := lookups["claude"]; got != 1 {
		t.Fatalf("LookPath(claude) calls = %d, want 1", got)
	}
	want := []string{"claude|opus|high", "claude|sonnet|high"}
	if diff := cmp.Diff(want, probes.calls); diff != "" {
		t.Fatalf("probe calls mismatch (-want +got):\n%s", diff)
	}
}

func TestPipelineRewalksSubWorkflowForEachInheritedOrigin(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "child-v1.0.yaml", `
name: child
steps:
  - id: inherited
    prompt: continue
    session: inherit
    model: opus
`)
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: origin-a
    agent: implementor-a
    session: new
    prompt: start
  - id: call-a
    workflow: child-v1.0.yaml
  - id: origin-b
    agent: implementor-b
    session: new
    prompt: start
  - id: call-b
    workflow: child-v1.0.yaml
`)
	cfg := &config.Config{
		ActiveProfile: "default",
		Profiles: map[string]*config.ProfileSet{
			"default": {Agents: map[string]*config.Agent{
				"implementor-a": {DefaultMode: "autonomous", CLI: "claude", Model: "haiku", Effort: "high"},
				"implementor-b": {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet", Effort: "medium"},
			}},
		},
		ActiveAgents: map[string]*config.Agent{
			"implementor-a": {DefaultMode: "autonomous", CLI: "claude", Model: "haiku", Effort: "high"},
			"implementor-b": {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet", Effort: "medium"},
		},
	}
	opts, _, probes := fakeOptions(t, cfg)

	if _, err := Pipeline(root, nil, Strict, opts); err != nil {
		t.Fatalf("Pipeline returned error: %v", err)
	}

	// probeTriples sorts triples alphabetically by "cli|model|effort" before invoking ProbeModel.
	want := []string{
		"claude|haiku|high",    // origin-a
		"claude|opus|high",     // child inherited from origin-a (model override)
		"claude|opus|medium",   // child inherited from origin-b — must not be memoized away
		"claude|sonnet|medium", // origin-b
	}
	if diff := cmp.Diff(want, probes.calls); diff != "" {
		t.Fatalf("probe calls mismatch (-want +got):\n%s", diff)
	}
}

func TestPipelineProbesAdapterExecutableName(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: ask
    agent: implementor
    prompt: start
`)
	cfg := &config.Config{
		ActiveAgents: map[string]*config.Agent{
			"implementor": {DefaultMode: "autonomous", CLI: "cursor", Model: "auto", Effort: "low"},
		},
	}

	lookups := map[string]int{}
	opts := Options{
		LoadConfig: func() (*config.Config, []string, error) {
			return cfg, nil, nil
		},
		LookPath: func(name string) (string, error) {
			lookups[name]++
			return "/bin/" + name, nil
		},
		Adapter: func(name string) (cli.Adapter, error) {
			if name != "cursor" {
				t.Fatalf("adapter name = %q, want cursor", name)
			}
			return &cli.CursorAdapter{}, nil
		},
	}

	if _, err := Pipeline(root, nil, Strict, opts); err != nil {
		t.Fatalf("Pipeline returned error: %v", err)
	}

	if got := lookups["agent"]; got != 1 {
		t.Fatalf("LookPath(agent) calls = %d, want 1", got)
	}
	if got := lookups["cursor"]; got != 0 {
		t.Fatalf("LookPath(cursor) calls = %d, want 0", got)
	}
}

func TestValidationErrorReportsProbeStrengthForProbeFailures(t *testing.T) {
	t.Run("probe error includes strength tag", func(t *testing.T) {
		err := probeError(probeKey{cli: "claude", model: "haiku", effort: "low"},
			probeSource{file: "wf.yaml", stepID: "ask", agent: "implementor"},
			cli.BinaryOnly, fmt.Errorf("binary not found"))
		msg := err.Error()
		if !strings.Contains(msg, "probe_strength=BinaryOnly") {
			t.Fatalf("expected probe_strength tag in probe error, got %q", msg)
		}
	})

	t.Run("non-probe error omits strength tag", func(t *testing.T) {
		err := ValidationError{File: "wf.yaml", StepID: "loop", Message: "loop requires at least one body step"}
		msg := err.Error()
		if strings.Contains(msg, "probe_strength") {
			t.Fatalf("expected no probe_strength tag in non-probe error, got %q", msg)
		}
	})
}

func TestValidationErrorRejectsInvalidLoopAsIndex(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: bad-loop
    loop:
      max: 2
      as_index: 1bad
    steps:
      - id: body
        command: echo hi
`)
	cfg := &config.Config{}
	opts := Options{
		LoadConfig: func() (*config.Config, []string, error) { return cfg, nil, nil },
		LookPath:   func(string) (string, error) { return "", nil },
		Adapter:    func(string) (cli.Adapter, error) { return nil, fmt.Errorf("unused") },
	}
	_, err := Pipeline(root, nil, Strict, opts)
	if err == nil {
		t.Fatal("expected validation error for invalid as_index, got nil")
	}
	if !strings.Contains(err.Error(), "invalid loop binding name") || !strings.Contains(err.Error(), "as_index") {
		t.Fatalf("expected as_index validation error, got %q", err.Error())
	}
}

func fakeOptions(t *testing.T, cfg *config.Config) (Options, map[string]int, *recordingProbeRegistry) {
	t.Helper()
	probes := &recordingProbeRegistry{}
	lookups := map[string]int{}
	return Options{
		LoadConfig: func() (*config.Config, []string, error) {
			return cfg, nil, nil
		},
		LookPath: func(name string) (string, error) {
			lookups[name]++
			return "/bin/" + name, nil
		},
		Adapter: probes.adapter,
	}, lookups, probes
}

type recordingProbeRegistry struct {
	calls []string
}

func (r *recordingProbeRegistry) adapter(name string) (cli.Adapter, error) {
	return probeAdapter{name: name, calls: &r.calls}, nil
}

type probeAdapter struct {
	name  string
	calls *[]string
}

func (a probeAdapter) BuildArgs(*cli.BuildArgsInput) []string { return nil }
func (a probeAdapter) DiscoverSessionID(*cli.DiscoverOptions) string {
	return ""
}
func (a probeAdapter) SupportsSystemPrompt() bool { return false }
func (a probeAdapter) ProbeModel(model, effort string) (cli.ProbeStrength, error) {
	*a.calls = append(*a.calls, fmt.Sprintf("%s|%s|%s", a.name, model, effort))
	return cli.BinaryOnly, nil
}

func writeWorkflow(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	return path
}
