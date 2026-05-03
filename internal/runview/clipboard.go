package runview

import (
	"os/exec"
	"strings"
)

var writeClipboard = writeClipboardPB

func writeClipboardPB(text string) error {
	cmd := exec.Command("pbcopy") // #nosec G204 -- fixed system clipboard command
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
