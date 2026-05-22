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
	writeWorkflow(t, dir, "green.yaml", `
name: green
steps:
  - id: ok
    command: echo ok
`)
	root := writeWorkflow(t, dir, "root.yaml", `
name: root
params:
  - name: flavor
steps:
  - id: call
    workflow: "{{flavor}}.yaml"
`)

	opts, _, _ := fakeOptions(t, &config.Config{})
	if _, err := Pipeline(root, map[string]string{"flavor": "green"}, Strict, opts); err != nil {
		t.Fatalf("Pipeline returned error: %v", err)
	}
}

func TestPipelineRewalksSameSubWorkflowWithDifferentParams(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "good.yaml", `
name: good
steps:
  - id: ok
    command: echo ok
`)
	writeWorkflow(t, dir, "switch.yaml", `
name: switch
params:
  - name: target
steps:
  - id: call-target
    workflow: "{{target}}.yaml"
`)
	root := writeWorkflow(t, dir, "root.yaml", `
name: root
steps:
  - id: first
    workflow: switch.yaml
    params:
      target: good
  - id: second
    workflow: switch.yaml
    params:
      target: missing
`)

	opts, _, _ := fakeOptions(t, &config.Config{})
	_, err := Pipeline(root, nil, Strict, opts)
	if err == nil {
		t.Fatal("expected second parameter set to validate and fail")
	}
	if !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("expected missing nested workflow error, got: %v", err)
	}
}

func TestPipelineRejectsCapturedWorkflowPath(t *testing.T) {
	dir := t.TempDir()
	root := writeWorkflow(t, dir, "root.yaml", `
name: root
steps:
  - id: choose
    command: echo child.yaml
    capture: detected_target
  - id: call
    workflow: "{{detected_target}}"
`)

	opts, _, _ := fakeOptions(t, &config.Config{})
	_, err := Pipeline(root, nil, Lenient, opts)
	if err == nil {
		t.Fatal("expected captured workflow path error")
	}
	if !strings.Contains(err.Error(), "call") || !strings.Contains(err.Error(), "captured") {
		t.Fatalf("expected error to name step and captured variable, got: %v", err)
	}
}

func TestPipelineChecksInterpolatedVariableReferences(t *testing.T) {
	t.Run("rejects undefined prompt variable", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root.yaml", `
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
		root := writeWorkflow(t, dir, "root.yaml", `
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
	writeWorkflow(t, dir, "child.yaml", `
name: child
steps:
  - id: inherited
    prompt: continue
    session: inherit
`)
	root := writeWorkflow(t, dir, "root.yaml", `
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
    workflow: child.yaml
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
	writeWorkflow(t, dir, "child.yaml", `
name: child
steps:
  - id: inherited
    prompt: continue
    session: inherit
    model: opus
`)
	root := writeWorkflow(t, dir, "root.yaml", `
name: root
steps:
  - id: origin-a
    agent: implementor-a
    session: new
    prompt: start
  - id: call-a
    workflow: child.yaml
  - id: origin-b
    agent: implementor-b
    session: new
    prompt: start
  - id: call-b
    workflow: child.yaml
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
	root := writeWorkflow(t, dir, "root.yaml", `
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
	root := writeWorkflow(t, dir, "root.yaml", `
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
