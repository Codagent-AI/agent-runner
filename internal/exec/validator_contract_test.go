//go:build validator_contract

package exec

import (
	"os"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestContractValidatorCapabilities(t *testing.T) {
	if os.Getenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE") == "" {
		t.Skip("set AGENT_RUNNER_VALIDATOR_EXECUTABLE to a built agent-validator")
	}
	dir := t.TempDir()
	_, environment, err := prepareValidatorLaunch(
		&model.Step{ID: "validate", MetricsSource: "agent-validator"},
		&model.ExecutionContext{SessionDir: dir, WorkingDir: dir},
	)
	if err != nil {
		t.Fatalf("real validator capabilities probe failed: %v", err)
	}
	if len(environment) != 3 {
		t.Fatalf("validator launch environment = %v", environment)
	}
}
