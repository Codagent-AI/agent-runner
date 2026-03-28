package engine

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

type stubEngine struct {
	config map[string]any
}

func (e *stubEngine) GetStateDir(params map[string]string) string                        { return "" }
func (e *stubEngine) ValidateWorkflow(_ model.Workflow, _ map[string]string, _ string) error { return nil }
func (e *stubEngine) NeedsDeferredValidation() bool                                      { return false }
func (e *stubEngine) EnrichPrompt(_ string, _ map[string]string, _ EnrichOptions) string { return "" }
func (e *stubEngine) ValidateStep(_ string, _ map[string]string) (bool, error)           { return true, nil }

func TestCreateEngine(t *testing.T) {
	t.Run("throws for unrecognized engine type", func(t *testing.T) {
		_, err := Create(map[string]any{"type": "foo"})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "Unknown engine type") {
			t.Fatalf("expected 'Unknown engine type', got: %v", err)
		}
	})

	t.Run("throws for unrecognized engine type with descriptive error", func(t *testing.T) {
		_, err := Create(map[string]any{"type": "nonexistent"})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Fatalf("expected error to contain type name, got: %v", err)
		}
	})
}

func TestRegisterEngine(t *testing.T) {
	t.Run("registers an engine and createEngine returns it", func(t *testing.T) {
		Register("test-engine", func(config map[string]any) Engine {
			return &stubEngine{config: config}
		})
		eng, err := Create(map[string]any{"type": "test-engine"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if eng == nil {
			t.Fatal("expected engine instance")
		}
	})

	t.Run("passes remaining config fields to constructor", func(t *testing.T) {
		Register("config-test", func(config map[string]any) Engine {
			return &stubEngine{config: config}
		})
		eng, err := Create(map[string]any{
			"type":        "config-test",
			"change_param": "change_id",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		se := eng.(*stubEngine)
		if se.config["change_param"] != "change_id" {
			t.Fatal("expected config to contain change_param")
		}
		if _, ok := se.config["type"]; ok {
			t.Fatal("expected type to be excluded from config")
		}
	})
}
