package exec

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestPrepareValidatorMetricsEnvironmentUsesCorrelationProtocol(t *testing.T) {
	ctx := &model.ExecutionContext{SessionDir: t.TempDir(), ExecutionSessionID: "execution-1"}
	step := &model.Step{ID: "validate", MetricsSource: "agent-validator"}

	capture, environment, err := prepareNestedMetricsEnvironment(step, ctx)
	if err != nil {
		t.Fatalf("prepare metrics environment: %v", err)
	}
	if capture.contextID == "" {
		t.Fatal("context ID is empty")
	}
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "AGENT_RUNNER_METRICS_CONSUMER=agent-runner") {
		t.Fatalf("environment = %q, want metrics consumer", environment)
	}
	if !strings.Contains(joined, "AGENT_RUNNER_METRICS_CONTEXT="+capture.contextID) {
		t.Fatalf("environment = %q, want opaque metrics context", environment)
	}
	if strings.Contains(joined, "AGENT_RUNNER_NESTED_METRICS") {
		t.Fatalf("environment retains retired JSONL transport: %q", environment)
	}
}
