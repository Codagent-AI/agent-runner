package runview

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

var writeClipboard = writeClipboardSystem

func writeClipboardSystem(text string) error {
	backend, err := clipboardBackend(runtime.GOOS, exec.LookPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(backend.name, backend.args...) // #nosec G204 -- fixed system clipboard command selected by platform
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

type clipboardCommand struct {
	name string
	args []string
}

func clipboardBackend(goos string, lookPath func(string) (string, error)) (clipboardCommand, error) {
	switch goos {
	case "darwin":
		return clipboardCommand{name: "pbcopy"}, nil
	case "windows":
		return clipboardCommand{name: "clip.exe"}, nil
	case "linux":
		if _, err := lookPath("wl-copy"); err == nil {
			return clipboardCommand{name: "wl-copy"}, nil
		}
		if _, err := lookPath("xclip"); err == nil {
			return clipboardCommand{name: "xclip", args: []string{"-selection", "clipboard"}}, nil
		}
		return clipboardCommand{}, fmt.Errorf("clipboard unavailable: install wl-copy or xclip")
	default:
		return clipboardCommand{}, fmt.Errorf("clipboard unsupported on %s", goos)
	}
}
