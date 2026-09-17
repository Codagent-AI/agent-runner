//go:build dev_audit && devaudit_smoke

package devaudit

import (
	_ "embed"

	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

//go:embed workflows/openspec/audit-smoke-v1.0.yaml
var auditSmokeWorkflow []byte

func init() {
	// This tag adds only fixture data. Both model stages retain their production
	// sandbox and the normal coordinator, profile, and completion paths.
	builtinworkflows.RegisterBuiltinAsset("openspec/audit-smoke-v1.0.yaml", auditSmokeWorkflow)
}
