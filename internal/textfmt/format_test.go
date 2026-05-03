package textfmt

import (
	"testing"
)

func intP(n int) *int { return &n }

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text unchanged", "hello world", "hello world"},
		{"standard SGR removed", "\x1b[31mred\x1b[0m", "red"},
		{"private CSI cursor hide removed", "\x1b[?25lhidden\x1b[?25h", "hidden"},
		{"private CSI with params removed", "\x1b[?2004htext\x1b[?2004l", "text"},
		{"OSC title removed", "\x1b]0;title\x07body", "body"},
		{"C0 NUL removed", "a\x00b", "ab"},
		{"C0 CR removed", "line\r\n", "line\n"},
		{"TAB preserved", "col1\tcol2", "col1\tcol2"},
		{"LF preserved", "line1\nline2", "line1\nline2"},
		{"DEL removed", "ab\x7fc", "abc"},
		{"multiple escapes removed", "\x1b[1m\x1b[32mbold green\x1b[0m", "bold green"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildBreadcrumb(t *testing.T) {
	t.Run("returns just the step id for a top-level step", func(t *testing.T) {
		result := BuildBreadcrumb(nil, "validate")
		if result != "validate" {
			t.Fatalf("expected 'validate', got %q", result)
		}
	})

	t.Run("returns breadcrumb for a step inside a loop iteration", func(t *testing.T) {
		path := []NestingInfo{{StepID: "task-loop", Iteration: intP(0)}}
		result := BuildBreadcrumb(path, "implement")
		expected := "task-loop > iteration 1 > implement"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("returns breadcrumb for a step inside a sub-workflow inside a loop", func(t *testing.T) {
		path := []NestingInfo{
			{StepID: "task-loop", Iteration: intP(0)},
			{StepID: "verify", SubWorkflowName: "verify-task"},
		}
		result := BuildBreadcrumb(path, "check")
		expected := "task-loop > iteration 1 > verify > verify-task > check"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("returns breadcrumb for plain nesting segment", func(t *testing.T) {
		path := []NestingInfo{{StepID: "parent"}}
		result := BuildBreadcrumb(path, "child")
		expected := "parent > child"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("converts 0-indexed iteration to 1-indexed display", func(t *testing.T) {
		path := []NestingInfo{{StepID: "loop", Iteration: intP(4)}}
		result := BuildBreadcrumb(path, "step")
		expected := "loop > iteration 5 > step"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})
}
