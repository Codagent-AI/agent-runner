package runview

import (
	"errors"
	"testing"
)

func TestClipboardBackend(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		available map[string]bool
		want      clipboardCommand
		wantErr   string
	}{
		{name: "darwin uses pbcopy", goos: "darwin", want: clipboardCommand{name: "pbcopy"}},
		{name: "windows uses clip", goos: "windows", want: clipboardCommand{name: "clip.exe"}},
		{name: "linux prefers wl-copy", goos: "linux", available: map[string]bool{"wl-copy": true, "xclip": true}, want: clipboardCommand{name: "wl-copy"}},
		{name: "linux falls back to xclip", goos: "linux", available: map[string]bool{"xclip": true}, want: clipboardCommand{name: "xclip", args: []string{"-selection", "clipboard"}}},
		{name: "linux reports missing backend", goos: "linux", wantErr: "clipboard unavailable: install wl-copy or xclip"},
		{name: "unsupported goos reports clear error", goos: "plan9", wantErr: "clipboard unsupported on plan9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := clipboardBackend(tt.goos, func(name string) (string, error) {
				if tt.available[name] {
					return "/usr/bin/" + name, nil
				}
				return "", errors.New("not found")
			})
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("clipboardBackend() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("clipboardBackend() returned error: %v", err)
			}
			if got.name != tt.want.name {
				t.Fatalf("clipboardBackend() command = %q, want %q", got.name, tt.want.name)
			}
			if len(got.args) != len(tt.want.args) {
				t.Fatalf("clipboardBackend() args = %#v, want %#v", got.args, tt.want.args)
			}
			for i := range got.args {
				if got.args[i] != tt.want.args[i] {
					t.Fatalf("clipboardBackend() args = %#v, want %#v", got.args, tt.want.args)
				}
			}
		})
	}
}
