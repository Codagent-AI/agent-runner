package engine

import (
	"fmt"

	"github.com/codagent/agent-runner/internal/model"
)

// Engine defines the interface for workflow engine plugins.
type Engine interface {
	GetStateDir(params map[string]string) string
	ValidateWorkflow(workflow model.Workflow, params map[string]string, workflowFile string) error
	NeedsDeferredValidation() bool
	EnrichPrompt(stepID string, params map[string]string, opts EnrichOptions) string
	ValidateStep(stepID string, params map[string]string) (bool, error)
}

// EnrichOptions provides additional context for prompt enrichment.
type EnrichOptions struct {
	SessionStrategy string
}

// Constructor is a function that creates an Engine from config.
type Constructor func(config map[string]any) Engine

var registry = map[string]Constructor{}

// Register adds an engine constructor to the registry.
func Register(engineType string, ctor Constructor) {
	registry[engineType] = ctor
}

// Create creates an engine instance from a config map.
func Create(engineConfig map[string]any) (Engine, error) {
	engineType, ok := engineConfig["type"].(string)
	if !ok {
		return nil, fmt.Errorf("engine config must have a \"type\" field")
	}

	ctor, ok := registry[engineType]
	if !ok {
		return nil, fmt.Errorf("Unknown engine type: %q", engineType)
	}

	// Pass remaining config fields (exclude "type").
	rest := make(map[string]any)
	for k, v := range engineConfig {
		if k != "type" {
			rest[k] = v
		}
	}

	return ctor(rest), nil
}
