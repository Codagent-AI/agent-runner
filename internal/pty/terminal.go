package pty

import (
	"fmt"
	"os"
)

// restoreTerminalModes resets terminal overrides that the hosted CLI may have set
// (enhanced keyboard, modifyOtherKeys, focus events, bracketed paste, cursor visibility).
func restoreTerminalModes() {
	_, _ = fmt.Fprint(os.Stdout, "\x1b[<u")     // restore enhanced keyboard
	_, _ = fmt.Fprint(os.Stdout, "\x1b[>4;0m")  // disable modifyOtherKeys
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?1004l") // disable focus events
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004l") // disable bracketed paste
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?25h")   // show cursor
}
