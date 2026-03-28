package textfmt

import (
	"strings"
	"testing"
)

func TestInterpolate(t *testing.T) {
	t.Run("replaces params in template", func(t *testing.T) {
		result, err := Interpolate("hello {{name}}", map[string]string{"name": "world"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "hello world" {
			t.Fatalf("expected 'hello world', got %q", result)
		}
	})

	t.Run("replaces captured variables in template", func(t *testing.T) {
		result, err := Interpolate("value: {{output}}", nil, map[string]string{"output": "captured-value"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "value: captured-value" {
			t.Fatalf("expected 'value: captured-value', got %q", result)
		}
	})

	t.Run("captured variables take precedence over params", func(t *testing.T) {
		result, err := Interpolate("{{key}}",
			map[string]string{"key": "param-val"},
			map[string]string{"key": "captured-val"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "captured-val" {
			t.Fatalf("expected 'captured-val', got %q", result)
		}
	})

	t.Run("throws for undefined variable", func(t *testing.T) {
		_, err := Interpolate("{{missing}}", map[string]string{}, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "Undefined variable: {{missing}}") {
			t.Fatalf("expected 'Undefined variable' error, got: %v", err)
		}
	})

	t.Run("handles multiple placeholders", func(t *testing.T) {
		result, err := Interpolate("{{a}} and {{b}}", map[string]string{"a": "x", "b": "y"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "x and y" {
			t.Fatalf("expected 'x and y', got %q", result)
		}
	})

	t.Run("returns template unchanged when no placeholders", func(t *testing.T) {
		result, err := Interpolate("no placeholders here", map[string]string{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "no placeholders here" {
			t.Fatalf("expected unchanged template, got %q", result)
		}
	})
}
