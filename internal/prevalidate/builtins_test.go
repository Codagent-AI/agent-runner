package prevalidate

import (
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestAllBuiltinsPreValidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectConfig := t.TempDir() + "/.agent-runner/config.yaml"
	refs, err := builtinworkflows.List()
	if err != nil {
		t.Fatalf("list builtins: %v", err)
	}
	for _, ref := range refs {
		t.Run(ref, func(t *testing.T) {
			_, err := Pipeline(ref, nil, Lenient, Options{
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
