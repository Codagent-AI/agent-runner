package audit

import "testing"

func TestRedact(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "github token",
			in:   "token ghp_AbC123XyZ remains hidden",
			want: "token <REDACTED> remains hidden",
		},
		{
			name: "openai style key",
			in:   "key sk-AbC123XyZ",
			want: "key <REDACTED>",
		},
		{
			name: "bearer credential",
			in:   "Authorization: Bearer abc.def-123",
			want: "Authorization: <REDACTED>",
		},
		{
			name: "env token assignment",
			in:   "MY_TOKEN=abc123 next",
			want: "<REDACTED> next",
		},
		{
			name: "password assignment",
			in:   "db password=swordfish next",
			want: "db <REDACTED> next",
		},
		{
			name: "non match preserved",
			in:   "plain text",
			want: "plain text",
		},
		{
			name: "surrounding context preserved",
			in:   "before ghp_AbC123XyZ after",
			want: "before <REDACTED> after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
