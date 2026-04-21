package pty

import (
	"fmt"
	"io"
	"os"
)

// restoreTerminalModes resets terminal overrides that the hosted CLI may have set
// (enhanced keyboard, modifyOtherKeys, focus events, bracketed paste, cursor visibility,
// scrolling region, cursor position).
func restoreTerminalModes() {
	_, _ = fmt.Fprint(os.Stdout, "\x1b[<u")     // restore enhanced keyboard
	_, _ = fmt.Fprint(os.Stdout, "\x1b[>4;0m")  // disable modifyOtherKeys
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?1004l") // disable focus events
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004l") // disable bracketed paste
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?25h")   // show cursor
	_, _ = fmt.Fprint(os.Stdout, "\x1b[r")      // reset scrolling region to full screen
	_, _ = fmt.Fprint(os.Stdout, "\x1b[999;1H") // move cursor to bottom-left
}

// writeEnterShellAltScreen switches to the alternate screen buffer, clears it,
// and homes the cursor so the interactive shell step renders on a clean canvas.
func writeEnterShellAltScreen(w io.Writer) {
	_, _ = fmt.Fprint(w, "\x1b[?1049h\x1b[2J\x1b[H")
}

// writeExitShellAltScreen leaves the alternate screen buffer, restoring whatever
// was previously on the normal screen (typically the suspended TUI's saved
// state, which the caller will re-enter via ResumeHook).
func writeExitShellAltScreen(w io.Writer) {
	_, _ = fmt.Fprint(w, "\x1b[?1049l")
}

func enterShellAltScreen() { writeEnterShellAltScreen(os.Stdout) }
func exitShellAltScreen()  { writeExitShellAltScreen(os.Stdout) }
