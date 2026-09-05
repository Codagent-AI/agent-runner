//go:build dev_audit && devaudit_e2e

package devaudit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

const e2eWorkflow = `name: audit-e2e
description: "Test-only complete development-audit journey."

steps:
  - id: execute
    command: |
      if [ -n "${AUDIT_E2E_FAIL_UNTIL:-}" ] && [ ! -f "$AUDIT_E2E_FAIL_UNTIL" ]; then
        exit 1
      fi
      printf 'completed\n' > audit-e2e-result.txt
`

func init() {
	builtinworkflows.RegisterBuiltinAsset("spec-driven/audit-e2e-v1.0.yaml", []byte(e2eWorkflow))
	allowInsecureDevelopmentAuditTestURI = true
	configureDetachedAuditCommand = func(command *exec.Cmd, auditSessionDir string) {
		logFile, err := os.OpenFile(filepath.Join(auditSessionDir, "detached.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- test-only audit directory.
		if err == nil {
			command.Stdout, command.Stderr = logFile, logFile
		}
	}
	crosscheckCommand = func(args []string, workspace, outputDir string) (*exec.Cmd, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("crosscheck adapter produced no command")
		}
		if err := os.MkdirAll(outputDir, 0o700); err != nil {
			return nil, err
		}
		command := exec.Command(args[0], args[1:]...) // #nosec G204 -- this build tag is a controlled local E2E harness.
		command.Dir = workspace
		return command, nil
	}
	if baseURL := os.Getenv("AGENT_RUNNER_DEVAUDIT_E2E_SHEETS_URL"); baseURL != "" {
		defaultSheetsReporter.SheetsBaseURL = baseURL
		defaultSheetsReporter.Store = ConnectionStore{}
	}
}
