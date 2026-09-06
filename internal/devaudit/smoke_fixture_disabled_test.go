//go:build dev_audit && !devaudit_smoke

package devaudit

import (
	"testing"

	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestOrdinaryDevelopmentBuildExcludesSmokeFixture(t *testing.T) {
	if ref, err := builtinworkflows.Resolve("openspec:audit-smoke"); err == nil {
		t.Fatalf("ordinary development build exposed smoke fixture %q", ref)
	}
}
