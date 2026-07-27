package prevalidate

import (
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestAllEmbeddedBuiltinVersionsLoadAndPreValidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectConfig := t.TempDir() + "/.agent-runner/config.yaml"
	refs, err := builtinworkflows.List()
	if err != nil {
		t.Fatalf("list builtins: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("expected at least one builtin workflow ref")
	}

	exactRefs := make(map[string]bool, len(refs))
	for _, ref := range refs {
		exactRefs[ref] = true
	}

	for _, ref := range refs {
		t.Run(ref, func(t *testing.T) {
			workflow, err := loader.LoadWorkflow(ref, loader.Options{})
			if err != nil {
				t.Fatalf("load exact embedded definition: %v", err)
			}
			assertExactEmbeddedChildren(t, ref, workflow.Steps, exactRefs)

			_, err = Pipeline(ref, nil, Lenient, Options{
				LoadConfig: func() (*config.Config, []string, error) {
					cfg, err := config.Load(projectConfig)
					return cfg, nil, err
				},
				LookPath: func(name string) (string, error) {
					return "/bin/" + name, nil
				},
				Adapter: func(name string) (cli.Adapter, error) {
					return probeAdapter{name: name, calls: new([]string)}, nil
				},
			})
			if err != nil {
				t.Fatalf("prevalidate builtin: %v", err)
			}
		})
	}
}

func assertExactEmbeddedChildren(t *testing.T, parentRef string, steps []model.Step, exactRefs map[string]bool) {
	t.Helper()

	for i := range steps {
		step := &steps[i]
		if step.Workflow != "" {
			childRef := loader.ResolveRelativeWorkflowPath(parentRef, step.Workflow)
			if _, err := workflowcatalog.Parse(childRef); err != nil {
				t.Errorf("step %q references unversioned child %q: %v", step.ID, step.Workflow, err)
			} else if !exactRefs[childRef] {
				t.Errorf("step %q references missing embedded child %q", step.ID, childRef)
			}
		}
		assertExactEmbeddedChildren(t, parentRef, step.Steps, exactRefs)
	}
}
