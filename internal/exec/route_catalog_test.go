package exec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

// TestRouteValidationCatalogIncludesUserScope proves intake can route to a
// workflow installed in the user-scope directory. The workflow browser
// enumerates that scope, so omitting it here would make a workflow the user can
// launch by hand unroutable from intake, reporting "workflow not found".
func TestRouteValidationCatalogIncludesUserScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	userWorkflows := filepath.Join(home, ".agent-runner", "workflows")
	if err := os.MkdirAll(userWorkflows, 0o755); err != nil {
		t.Fatalf("create user workflows dir: %v", err)
	}
	workflow := "name: user-routable\nsteps:\n  - id: noop\n    command: \"true\"\n"
	if err := os.WriteFile(filepath.Join(userWorkflows, "user-routable-v1.0.yaml"), []byte(workflow), 0o644); err != nil {
		t.Fatalf("write user workflow: %v", err)
	}

	ctx := &model.ExecutionContext{SessionDir: t.TempDir(), ProjectRoot: t.TempDir()}
	options := routeValidationOptions(ctx)
	if options == nil {
		t.Fatal("routeValidationOptions returned nil for a context with a session directory")
	}

	entry, err := options.Catalog.ResolveWorkflow("user-routable")
	if err != nil {
		t.Fatalf("user-scope workflow is not routable from intake: %v", err)
	}
	if entry.SourcePath == "" {
		t.Fatal("resolved user-scope workflow has no source path")
	}
}
