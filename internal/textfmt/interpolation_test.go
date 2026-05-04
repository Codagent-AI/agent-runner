package textfmt

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func TestInterpolate(t *testing.T) {
	t.Run("replaces builtins in template", func(t *testing.T) {
		result, err := Interpolate("dir: {{session_dir}}", nil, nil,
			map[string]string{"session_dir": "/tmp/run-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "dir: /tmp/run-1" {
			t.Fatalf("expected 'dir: /tmp/run-1', got %q", result)
		}
	})

	t.Run("params take precedence over builtins", func(t *testing.T) {
		result, err := Interpolate("{{session_dir}}",
			map[string]string{"session_dir": "from-param"},
			nil,
			map[string]string{"session_dir": "from-builtin"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "from-param" {
			t.Fatalf("expected 'from-param', got %q", result)
		}
	})

	t.Run("captured variables take precedence over builtins", func(t *testing.T) {
		result, err := Interpolate("{{session_dir}}",
			nil,
			map[string]string{"session_dir": "from-captured"},
			map[string]string{"session_dir": "from-builtin"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "from-captured" {
			t.Fatalf("expected 'from-captured', got %q", result)
		}
	})

	t.Run("replaces params in template", func(t *testing.T) {
		result, err := Interpolate("hello {{name}}", map[string]string{"name": "world"}, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "hello world" {
			t.Fatalf("expected 'hello world', got %q", result)
		}
	})

	t.Run("replaces captured variables in template", func(t *testing.T) {
		result, err := Interpolate("value: {{output}}", nil, map[string]string{"output": "captured-value"}, nil)
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
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "captured-val" {
			t.Fatalf("expected 'captured-val', got %q", result)
		}
	})

	t.Run("throws for undefined variable", func(t *testing.T) {
		_, err := Interpolate("{{missing}}", map[string]string{}, nil, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "undefined variable: {{missing}}") {
			t.Fatalf("expected 'Undefined variable' error, got: %v", err)
		}
	})

	t.Run("handles multiple placeholders", func(t *testing.T) {
		result, err := Interpolate("{{a}} and {{b}}", map[string]string{"a": "x", "b": "y"}, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "x and y" {
			t.Fatalf("expected 'x and y', got %q", result)
		}
	})

	t.Run("returns template unchanged when no placeholders", func(t *testing.T) {
		result, err := Interpolate("no placeholders here", map[string]string{}, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "no placeholders here" {
			t.Fatalf("expected unchanged template, got %q", result)
		}
	})
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"foo; rm -rf /", "'foo; rm -rf /'"},
		{"$(whoami)", "'$(whoami)'"},
		{"`id`", "'`id`'"},
		{"a\"b", "'a\"b'"},
		{"", "''"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellQuote(tt.input)
			if got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInterpolateShellSafe(t *testing.T) {
	t.Run("quotes param values for shell safety", func(t *testing.T) {
		result, err := InterpolateShellSafe("test -f {{filename}}",
			map[string]string{"filename": "foo; rm -rf /"},
			nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "'foo; rm -rf /'") {
			t.Fatalf("expected shell-quoted value, got %q", result)
		}
	})

	t.Run("quotes builtin values for shell safety", func(t *testing.T) {
		result, err := InterpolateShellSafe("ls {{session_dir}}/output",
			nil, nil, map[string]string{"session_dir": "/tmp/runs/abc"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "ls '/tmp/runs/abc'/output" {
			t.Fatalf("expected shell-quoted builtin, got %q", result)
		}
	})

	t.Run("returns error for undefined variable", func(t *testing.T) {
		_, err := InterpolateShellSafe("{{missing}}", nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for undefined variable")
		}
	})
}

func TestInterpolateShellSafeTypedPreservesCapturedWhitespace(t *testing.T) {
	captures := map[string]model.CapturedValue{
		"value": model.NewCapturedString("  padded\n"),
	}

	result, err := InterpolateShellSafeTyped("printf %s {{value}}", nil, captures, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "printf %s '  padded\n'" {
		t.Fatalf("InterpolateShellSafeTyped() = %q", result)
	}
}

func TestInterpolateShellSafeTypedEscapesDoubleQuotedPlaceholders(t *testing.T) {
	captures := map[string]model.CapturedValue{
		"value": model.NewCapturedString(`learn more "$(rm -rf /)"`),
	}

	result, err := InterpolateShellSafeTyped(`test "x{{value}}" != "xlearn_more"`, nil, captures, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `test "xlearn more \"\$(rm -rf /)\"" != "xlearn_more"`
	if result != want {
		t.Fatalf("InterpolateShellSafeTyped() = %q, want %q", result, want)
	}
}

func TestInterpolateTyped(t *testing.T) {
	captures := map[string]model.CapturedValue{
		"profile": {Kind: model.CaptureMap, Map: map[string]string{"adapter": "claude", "model": "opus"}},
		"out":     {Kind: model.CaptureString, Str: "hello"},
		"choices": {Kind: model.CaptureList, List: []string{"claude", "codex"}},
	}

	t.Run("resolves map field access", func(t *testing.T) {
		got, err := InterpolateTyped("adapter={{profile.adapter}}", nil, captures, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "adapter=claude" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("rejects whole map in string context", func(t *testing.T) {
		_, err := InterpolateTyped("profile={{profile}}", nil, captures, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "map capture {{profile}} cannot be interpolated in a string context") ||
			!strings.Contains(err.Error(), "{{profile.<field>}}") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects field access on string capture", func(t *testing.T) {
		_, err := InterpolateTyped("{{out.field}}", nil, captures, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "field access requires map-typed capture: {{out.field}}") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResolveTypedValue(t *testing.T) {
	captures := map[string]model.CapturedValue{
		"choices": {Kind: model.CaptureList, List: []string{"claude", "codex"}},
	}

	got, err := ResolveTypedValue("{{choices}}", captures)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := model.CapturedValue{Kind: model.CaptureList, List: []string{"claude", "codex"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("typed value mismatch (-want +got):\n%s", diff)
	}
}
